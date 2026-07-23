package schemamigrator

import (
	"context"
	"fmt"
	"slices"
)

// BaselineState classifies migration history at the v1 baseline boundary.
// A complete history still requires schema fingerprint verification before a baseline marker is written.
type BaselineState string

// Baseline history states.
const (
	BaselineStateEmpty          BaselineState = "empty"
	BaselineStateCompleteFresh  BaselineState = "complete-fresh"
	BaselineStateCompleteLegacy BaselineState = "complete-legacy"
	BaselineStatePartial        BaselineState = "partial"
	BaselineStateAhead          BaselineState = "ahead"
)

// BaselineSpec describes required and fresh-install migration records for one database.
type BaselineSpec struct {
	Database             string
	BaseMigrationIDs     []uint64
	RequiredMigrationIDs []uint64
	AllowedMigrationIDs  []uint64
	BaselineMigration    SchemaMigrationRecord
}

// BaselineSnapshot contains the database facts used by the pure baseline classifier.
type BaselineSnapshot struct {
	DomainTableCount   uint64
	LegacyTableExists  bool
	TrackingTableCount uint64
	MigrationStatuses  map[uint64]string
}

// BaselineInspection explains the baseline state and any incompatible migration records.
type BaselineInspection struct {
	Database                string
	State                   BaselineState
	LegacyTableExists       bool
	DomainTableCount        uint64
	TrackingTableCount      uint64
	MissingMigrationIDs     []uint64
	NonFinishedMigrationIDs []uint64
	UnexpectedMigrationIDs  []uint64
}

const baselineInventoryQuery = `SELECT
	countIf(name NOT IN ('schema_migrations', 'schema_migrations_v2')),
	countIf(name = 'schema_migrations'),
	countIf(name = 'schema_migrations_v2')
FROM system.tables
WHERE database = $1`

func baselineMigrationsQuery(database string) string {
	return fmt.Sprintf(
		"SELECT migration_id, status FROM %s.schema_migrations_v2 SETTINGS final = 1",
		database,
	)
}

// V1BaselineSpecs freezes the histories accepted at the v1 boundary. IDs added
// after this boundary need a separate post-baseline runner and must not be added
// here, otherwise an existing 999 marker would incorrectly imply they ran.
func V1BaselineSpecs() []BaselineSpec {
	return []BaselineSpec{
		newV1BaselineSpec(SigInsightTracesDB, migrationIDRange(1, 27), migrationIDRange(1000, 1008)),
		newV1BaselineSpec(SigInsightMetricsDB, migrationIDRange(1, 28), migrationIDRange(1000, 1007)),
		newV1BaselineSpec(SigInsightLogsDB, migrationIDRange(1, 9), migrationIDRange(1000, 1005)),
		newV1BaselineSpec(SigInsightMetadataDB, nil, migrationIDRange(1000, 1001)),
		newV1BaselineSpec(SigInsightAnalyticsDB, nil, migrationIDRange(1, 1)),
		newV1BaselineSpec(SigInsightMeterDB, nil, migrationIDRange(1, 5)),
	}
}

// InspectBaselineState reads migration history and classifies one database.
// It deliberately does not write a baseline marker.
func (m *MigrationManager) InspectBaselineState(ctx context.Context, spec BaselineSpec) (BaselineInspection, error) {
	if !isKnownDatabase(spec.Database) {
		return BaselineInspection{}, fmt.Errorf("unknown baseline database %q", spec.Database)
	}

	snapshot := BaselineSnapshot{
		MigrationStatuses: make(map[uint64]string),
	}

	var legacyTableCount uint64
	if err := m.conn.QueryRow(ctx, baselineInventoryQuery, spec.Database).Scan(
		&snapshot.DomainTableCount,
		&legacyTableCount,
		&snapshot.TrackingTableCount,
	); err != nil {
		return BaselineInspection{}, fmt.Errorf("inspect tables for database %s: %w", spec.Database, err)
	}
	snapshot.LegacyTableExists = legacyTableCount > 0

	if snapshot.TrackingTableCount == 1 {
		rows, err := m.conn.Query(ctx, baselineMigrationsQuery(spec.Database))
		if err != nil {
			return BaselineInspection{}, fmt.Errorf("inspect migrations for database %s: %w", spec.Database, err)
		}
		defer rows.Close()

		for rows.Next() {
			var migrationID uint64
			var status string
			if err := rows.Scan(&migrationID, &status); err != nil {
				return BaselineInspection{}, fmt.Errorf("scan migrations for database %s: %w", spec.Database, err)
			}
			snapshot.MigrationStatuses[migrationID] = status
		}
		if err := rows.Err(); err != nil {
			return BaselineInspection{}, fmt.Errorf("read migrations for database %s: %w", spec.Database, err)
		}
	}

	return ClassifyBaselineState(spec, snapshot), nil
}

// ClassifyBaselineState classifies a snapshot without querying ClickHouse.
func ClassifyBaselineState(spec BaselineSpec, snapshot BaselineSnapshot) BaselineInspection {
	result := BaselineInspection{
		Database:           spec.Database,
		LegacyTableExists:  snapshot.LegacyTableExists,
		DomainTableCount:   snapshot.DomainTableCount,
		TrackingTableCount: snapshot.TrackingTableCount,
	}

	if snapshot.DomainTableCount == 0 &&
		!snapshot.LegacyTableExists &&
		len(snapshot.MigrationStatuses) == 0 {
		result.State = BaselineStateEmpty
		return result
	}

	baseIDs := migrationIDSet(spec.BaseMigrationIDs)
	requiredIDs := migrationIDSet(spec.RequiredMigrationIDs)

	allowedIDs := make(map[uint64]struct{}, len(baseIDs)+len(requiredIDs)+len(spec.AllowedMigrationIDs)+1)
	addMigrationIDs(allowedIDs, baseIDs)
	addMigrationIDs(allowedIDs, requiredIDs)
	addMigrationIDs(allowedIDs, migrationIDSet(spec.AllowedMigrationIDs))
	if spec.BaselineMigration.MigrationID != 0 {
		allowedIDs[spec.BaselineMigration.MigrationID] = struct{}{}
	}

	if !snapshot.LegacyTableExists {
		addMigrationIDs(requiredIDs, baseIDs)
	}

	for migrationID, status := range snapshot.MigrationStatuses {
		if _, ok := allowedIDs[migrationID]; !ok {
			result.UnexpectedMigrationIDs = append(result.UnexpectedMigrationIDs, migrationID)
			continue
		}
		if status != FinishedStatus {
			result.NonFinishedMigrationIDs = append(result.NonFinishedMigrationIDs, migrationID)
		}
	}

	baselineFinished := spec.BaselineMigration.MigrationID != 0 &&
		snapshot.MigrationStatuses[spec.BaselineMigration.MigrationID] == FinishedStatus
	if !baselineFinished {
		for migrationID := range requiredIDs {
			if _, ok := snapshot.MigrationStatuses[migrationID]; !ok {
				result.MissingMigrationIDs = append(result.MissingMigrationIDs, migrationID)
			}
		}
	}

	slices.Sort(result.MissingMigrationIDs)
	slices.Sort(result.NonFinishedMigrationIDs)
	slices.Sort(result.UnexpectedMigrationIDs)

	if len(result.UnexpectedMigrationIDs) > 0 {
		result.State = BaselineStateAhead
		return result
	}

	if snapshot.DomainTableCount == 0 ||
		snapshot.TrackingTableCount != 1 ||
		len(result.MissingMigrationIDs) > 0 ||
		len(result.NonFinishedMigrationIDs) > 0 {
		result.State = BaselineStatePartial
		return result
	}

	if snapshot.LegacyTableExists {
		result.State = BaselineStateCompleteLegacy
	} else {
		result.State = BaselineStateCompleteFresh
	}
	return result
}

func migrationIDSet(migrations []uint64) map[uint64]struct{} {
	ids := make(map[uint64]struct{}, len(migrations))
	for _, migrationID := range migrations {
		ids[migrationID] = struct{}{}
	}
	return ids
}

func addMigrationIDs(destination, source map[uint64]struct{}) {
	for migrationID := range source {
		destination[migrationID] = struct{}{}
	}
}

func isKnownDatabase(database string) bool {
	for _, knownDatabase := range Databases {
		if database == knownDatabase {
			return true
		}
	}
	return false
}

func migrationIDRange(first, last uint64) []uint64 {
	ids := make([]uint64, 0, last-first+1)
	for id := first; id <= last; id++ {
		ids = append(ids, id)
	}
	return ids
}
