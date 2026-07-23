package schemamigrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"go.uber.org/zap"
)

var (
	SigInsightLogsDB      = "signoz_logs"
	SigInsightMetricsDB   = "signoz_metrics"
	SigInsightTracesDB    = "signoz_traces"
	SigInsightMetadataDB  = "signoz_metadata"
	SigInsightAnalyticsDB = "signoz_analytics"
	SigInsightMeterDB     = "signoz_meter"
	Databases             = []string{SigInsightTracesDB, SigInsightMetricsDB, SigInsightLogsDB, SigInsightMetadataDB, SigInsightAnalyticsDB, SigInsightMeterDB}

	InProgressStatus = "in-progress"
	FinishedStatus   = "finished"
	FailedStatus     = "failed"
)

type SchemaMigrationRecord struct {
	MigrationID uint64
	UpItems     []Operation
	DownItems   []Operation
}

// MigrationManager is the manager for the schema migrations.
type MigrationManager struct {
	conn   clickhouse.Conn
	logger *zap.Logger
}

type Option func(*MigrationManager)

// NewMigrationManager creates a new migration manager.
func NewMigrationManager(opts ...Option) (*MigrationManager, error) {
	mgr := &MigrationManager{
		logger: zap.NewNop(),
	}
	for _, opt := range opts {
		opt(mgr)
	}
	if mgr.conn == nil {
		return nil, errors.New("conn is required")
	}
	return mgr, nil
}

func WithConn(conn clickhouse.Conn) Option {
	return func(mgr *MigrationManager) {
		mgr.conn = conn
	}
}

func WithLogger(logger *zap.Logger) Option {
	return func(mgr *MigrationManager) {
		mgr.logger = logger
	}
}

func (m *MigrationManager) shouldRunMigration(db string, migrationID uint64, versions []uint64) bool {
	m.logger.Info("Checking if migration should run", zap.String("db", db), zap.Uint64("migration_id", migrationID), zap.Any("versions", versions))
	// if versions are provided, we only run the migrations that are in the versions slice
	if len(versions) != 0 {
		var doesExist bool
		for _, version := range versions {
			if migrationID == version {
				doesExist = true
				break
			}
		}
		if !doesExist {
			m.logger.Info("Migration should not run as it is not in the provided versions", zap.Uint64("migration_id", migrationID), zap.Any("versions", versions))
			return false
		}
	}

	query := fmt.Sprintf("SELECT * FROM %s.schema_migrations_v2 WHERE migration_id = %d SETTINGS final = 1;", db, migrationID)
	m.logger.Info("Fetching migration status", zap.String("query", query))
	var migrationSchemaMigrationRecord MigrationSchemaMigrationRecord
	if err := m.conn.QueryRow(context.Background(), query).ScanStruct(&migrationSchemaMigrationRecord); err != nil {
		if err == sql.ErrNoRows {
			m.logger.Info("Migration not run", zap.Uint64("migration_id", migrationID))
			return true
		}
		// this should not happen
		m.logger.Error("Failed to fetch migration status", zap.Error(err))
		panic(err)
	}
	m.logger.Info("Migration status", zap.Uint64("migration_id", migrationID), zap.String("status", migrationSchemaMigrationRecord.Status))
	if migrationSchemaMigrationRecord.Status != InProgressStatus && migrationSchemaMigrationRecord.Status != FinishedStatus {
		m.logger.Info("Migration not run", zap.Uint64("migration_id", migrationID), zap.String("status", migrationSchemaMigrationRecord.Status))
		return true
	}
	return false
}

func (m *MigrationManager) IsSync(migration SchemaMigrationRecord) bool {
	for _, item := range migration.UpItems {
		// if any of the operations is a sync operation, return true
		if ok := m.IsSyncOperation(item); ok {
			return true
		}
	}

	return false
}

func (m *MigrationManager) IsSyncOperation(item Operation) bool {
	return isSyncOperation(item)
}

func (m *MigrationManager) IsAsync(migration SchemaMigrationRecord) bool {
	for _, item := range migration.UpItems {
		// if any of the operations is an async operation, return true
		if ok := m.IsAsyncOperation(item); ok {
			return true
		}
	}

	return false
}

func (m *MigrationManager) IsAsyncOperation(item Operation) bool {
	// If it is a force migrate operation, return false
	if item.ForceMigrate() {
		return false
	}

	// If it is a sync operation, return false
	if !item.IsMutation() && item.IsIdempotent() && item.IsLightweight() {
		return false
	}

	return true
}

func (m *MigrationManager) insertMigrationEntry(ctx context.Context, db string, migrationID uint64, status string) error {
	query := fmt.Sprintf("INSERT INTO %s.schema_migrations_v2 (migration_id, status, created_at) VALUES (%d, '%s', '%s')", db, migrationID, status, time.Now().UTC().Format("2006-01-02 15:04:05"))
	m.logger.Info("Inserting migration entry", zap.String("query", query))
	return m.conn.Exec(ctx, query)
}

func (m *MigrationManager) updateMigrationEntry(ctx context.Context, db string, migrationID uint64, status string, err string) error {
	query := fmt.Sprintf("ALTER TABLE %s.schema_migrations_v2 UPDATE status = $1, error = $2, updated_at = $3 WHERE migration_id = $4 SETTINGS mutations_sync = 1", db)
	m.logger.Info("Updating migration entry", zap.String("query", query), zap.String("status", status), zap.String("error", err), zap.Uint64("migration_id", migrationID))
	return m.conn.Exec(ctx, query, status, err, time.Now().UTC().Format("2006-01-02 15:04:05"), migrationID)
}

func (m *MigrationManager) RunOperation(ctx context.Context, operation Operation, migrationID uint64, database string, skipStatusUpdate bool) error {
	m.logger.Info("Running operation", zap.Uint64("migration_id", migrationID), zap.String("database", database), zap.Bool("skip_status_update", skipStatusUpdate))
	start := time.Now()
	if !skipStatusUpdate {
		insertErr := m.insertMigrationEntry(ctx, database, migrationID, InProgressStatus)
		if insertErr != nil {
			return insertErr
		}
	}

	sql := operation.ToSQL()
	m.logger.Info("Running operation", zap.String("sql", sql))
	err := m.conn.Exec(ctx, sql)
	if err != nil {
		if skipStatusUpdate {
			return err
		}
		updateErr := m.updateMigrationEntry(ctx, database, migrationID, FailedStatus, err.Error())
		if updateErr != nil {
			return errors.Join(err, updateErr)
		}
		return err
	}

	if !skipStatusUpdate {
		updateErr := m.updateMigrationEntry(ctx, database, migrationID, FinishedStatus, "")
		if updateErr != nil {
			return updateErr
		}
	}
	duration := time.Since(start)
	m.logger.Info("Operation completed", zap.Uint64("migration_id", migrationID), zap.String("database", database), zap.Duration("duration", duration))

	return nil
}

func (m *MigrationManager) RunOperationWithoutUpdate(ctx context.Context, operation Operation, migrationID uint64, database string) error {
	m.logger.Info("Running operation", zap.Uint64("migration_id", migrationID), zap.String("database", database))
	start := time.Now()

	sql := operation.ToSQL()
	m.logger.Info("Running operation", zap.String("sql", sql))
	err := m.conn.Exec(ctx, sql)
	if err != nil {
		return err
	}

	duration := time.Since(start)
	m.logger.Info("Operation completed", zap.Uint64("migration_id", migrationID), zap.String("database", database), zap.Duration("duration", duration))

	return nil
}

func (m *MigrationManager) InsertMigrationEntry(ctx context.Context, db string, migrationID uint64, status string) error {
	query := fmt.Sprintf("INSERT INTO %s.schema_migrations_v2 (migration_id, status, created_at) VALUES (%d, '%s', '%s')", db, migrationID, status, time.Now().UTC().Format("2006-01-02 15:04:05"))
	m.logger.Info("Inserting migration entry", zap.String("query", query))
	return m.conn.Exec(ctx, query)
}

func (m *MigrationManager) CheckMigrationStatus(ctx context.Context, db string, migrationID uint64, status string) (bool, error) {
	actual, exists, err := m.MigrationStatus(ctx, db, migrationID)
	return exists && actual == status, err
}

// MigrationStatus returns the final status record for one migration.
func (m *MigrationManager) MigrationStatus(ctx context.Context, db string, migrationID uint64) (string, bool, error) {
	query := fmt.Sprintf("SELECT * FROM %s.schema_migrations_v2 WHERE migration_id = %d SETTINGS final = 1;", db, migrationID)
	m.logger.Info("Checking migration status", zap.String("query", query))

	var migrationSchemaMigrationRecord MigrationSchemaMigrationRecord
	if err := m.conn.QueryRow(ctx, query).ScanStruct(&migrationSchemaMigrationRecord); err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}

		return "", false, err
	}

	return migrationSchemaMigrationRecord.Status, true, nil
}

func (m *MigrationManager) Close() error {
	return m.conn.Close()
}
