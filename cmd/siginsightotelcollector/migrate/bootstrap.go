package migrate

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/SigInsight/OtelCollector/cmd/siginsightotelcollector/config"
	schemamigrator "github.com/SigInsight/OtelCollector/cmd/siginsightschemamigrator/schema_migrator"
	"github.com/cenkalti/backoff/v4"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

type bootstrap struct {
	conn             clickhouse.Conn
	migrationManager *schemamigrator.MigrationManager
	timeout          time.Duration
	logger           *zap.Logger
}

func registerBootstrap(parentCmd *cobra.Command, logger *zap.Logger) {
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Creates the necessary tables to track status of migrations. A migration table is typically created to track the status of migrations. This command creates the necessary tables to track the status of migrations.",
		RunE: func(cmd *cobra.Command, args []string) error {
			bootstrap, err := newBootstrap(config.Clickhouse.DSN, config.MigrateBootstrap.Timeout, logger)
			if err != nil {
				return err
			}

			if err := bootstrap.Run(cmd.Context()); err != nil {
				return err
			}

			return nil
		},
	}

	config.MigrateBootstrap.RegisterFlags(cmd)
	parentCmd.AddCommand(cmd)
}

func newBootstrap(dsn string, timeout time.Duration, logger *zap.Logger) (*bootstrap, error) {
	opts, err := clickhouse.ParseDSN(dsn)
	if err != nil {
		return nil, err
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, err
	}

	migrationManager, err := schemamigrator.NewMigrationManager(
		schemamigrator.WithConn(conn),
		schemamigrator.WithLogger(logger),
	)
	if err != nil {
		return nil, err
	}

	return &bootstrap{
		conn:             conn,
		migrationManager: migrationManager,
		timeout:          timeout,
		logger:           logger,
	}, nil
}

func (cmd *bootstrap) Run(ctx context.Context) error {
	backoff := backoff.NewExponentialBackOff()
	backoff.MaxElapsedTime = cmd.timeout

	for {
		err := cmd.Bootstrap(ctx)
		if err == nil {
			break
		}

		migrateErr := Unwrapb(err)
		// exit early for non-retryable errors.
		if !migrateErr.IsRetryable() {
			return fmt.Errorf("failed to bootstrap store for migrations: %w", err)
		}

		cmd.logger.Info("retrying store bootstrap for migrations", zap.Error(err))
		nextBackOff := backoff.NextBackOff()
		if nextBackOff == backoff.Stop {
			return fmt.Errorf("failed to bootstrap store for migrations within the configured timeout of %s", cmd.timeout)
		}
		time.Sleep(nextBackOff)
	}

	return nil
}

func (cmd *bootstrap) Bootstrap(ctx context.Context) error {
	err := cmd.UpgradeLegacyStorageNames(ctx)
	if err != nil {
		return err
	}

	err = cmd.CreateDatabases(ctx)
	if err != nil {
		return err
	}

	err = cmd.CreateMigrationTables(ctx)
	if err != nil {
		return err
	}

	return nil
}

// UpgradeLegacyStorageNames adopts a pre-SigInsight telemetry installation
// without copying its data. ClickHouse does not rewrite view definitions when
// a database or source table is renamed, so affected views are recreated from
// the current frozen baseline after the physical names are switched.
func (cmd *bootstrap) UpgradeLegacyStorageNames(ctx context.Context) error {
	databases, err := cmd.databaseNames(ctx)
	if err != nil {
		return NewRetryableError(err)
	}

	changed := false
	legacyRenames := schemamigrator.LegacyDatabaseRenames()
	legacyDatabases := make([]string, 0, len(legacyRenames))
	for legacy := range legacyRenames {
		legacyDatabases = append(legacyDatabases, legacy)
	}
	sort.Strings(legacyDatabases)
	for _, legacy := range legacyDatabases {
		current := legacyRenames[legacy]
		_, legacyExists := databases[legacy]
		_, currentExists := databases[current]
		if legacyExists && currentExists {
			return New(fmt.Errorf("both legacy database %q and current database %q exist; resolve the conflict before retrying", legacy, current))
		}
		if !legacyExists {
			continue
		}
		if err := cmd.conn.Exec(ctx, fmt.Sprintf("RENAME DATABASE %s TO %s", legacy, current)); err != nil {
			return New(err)
		}
		changed = true
	}

	for _, rename := range []struct{ old, new string }{
		{"signoz_index_v3", "span_index_v3"},
		{"signoz_index_v2", "span_index_v2"},
		{"signoz_error_index_v2", "error_index_v2"},
		{"signoz_spans", "legacy_spans"},
	} {
		renamed, err := cmd.renameTableIfNeeded(ctx, schemamigrator.SigInsightTracesDB, rename.old, rename.new)
		if err != nil {
			return New(err)
		}
		changed = changed || renamed
	}
	if !changed {
		return nil
	}

	if err := cmd.restoreCurrentViews(ctx); err != nil {
		return New(err)
	}
	if err := cmd.rewriteLegacyViewDefinitions(ctx); err != nil {
		return New(err)
	}
	return nil
}

func (cmd *bootstrap) databaseNames(ctx context.Context) (map[string]struct{}, error) {
	rows, err := cmd.conn.Query(ctx, "SELECT name FROM system.databases")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	databases := make(map[string]struct{})
	for rows.Next() {
		var database string
		if err := rows.Scan(&database); err != nil {
			return nil, err
		}
		databases[database] = struct{}{}
	}
	return databases, rows.Err()
}

func (cmd *bootstrap) renameTableIfNeeded(ctx context.Context, database, oldName, newName string) (bool, error) {
	rows, err := cmd.conn.Query(ctx, "SELECT name FROM system.tables WHERE database = ? AND name IN (?, ?)", database, oldName, newName)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	exists := make(map[string]struct{}, 2)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		exists[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	_, oldExists := exists[oldName]
	_, newExists := exists[newName]
	if oldExists && newExists {
		return false, fmt.Errorf("both legacy table %s.%s and current table %s.%s exist", database, oldName, database, newName)
	}
	if !oldExists {
		return false, nil
	}
	return true, cmd.conn.Exec(ctx, fmt.Sprintf("RENAME TABLE %s.%s TO %s.%s", database, oldName, database, newName))
}

func (cmd *bootstrap) restoreCurrentViews(ctx context.Context) error {
	for _, operation := range schemamigrator.V1MaterializedViewOperations() {
		if err := cmd.conn.Exec(ctx, operation.ToSQL()); err != nil {
			return err
		}
	}
	return nil
}

func (cmd *bootstrap) rewriteLegacyViewDefinitions(ctx context.Context) error {
	rows, err := cmd.conn.Query(ctx, "SELECT database, name, create_table_query FROM system.tables WHERE database IN (?, ?, ?, ?, ?, ?) AND engine IN ('MaterializedView', 'View')", schemamigrator.SigInsightLogsDB, schemamigrator.SigInsightMetricsDB, schemamigrator.SigInsightTracesDB, schemamigrator.SigInsightMetadataDB, schemamigrator.SigInsightAnalyticsDB, schemamigrator.SigInsightMeterDB)
	if err != nil {
		return err
	}
	defer rows.Close()
	type viewDefinition struct{ database, name, ddl string }
	definitions := make([]viewDefinition, 0)
	for rows.Next() {
		var definition viewDefinition
		if err := rows.Scan(&definition.database, &definition.name, &definition.ddl); err != nil {
			return err
		}
		if rewriteLegacyStorageName(definition.ddl) != definition.ddl {
			definitions = append(definitions, definition)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, definition := range definitions {
		if err := cmd.conn.Exec(ctx, fmt.Sprintf("DROP TABLE %s.%s", definition.database, definition.name)); err != nil {
			return err
		}
		if err := cmd.conn.Exec(ctx, rewriteLegacyStorageName(definition.ddl)); err != nil {
			return err
		}
	}
	return nil
}

func rewriteLegacyStorageName(value string) string {
	// Keep this ordered. A map iteration here would make schema-rewrite output
	// nondeterministic and is unnecessary for this fixed one-time migration.
	return strings.NewReplacer(
		"signoz_logs", schemamigrator.SigInsightLogsDB,
		"signoz_metrics", schemamigrator.SigInsightMetricsDB,
		"signoz_traces", schemamigrator.SigInsightTracesDB,
		"signoz_metadata", schemamigrator.SigInsightMetadataDB,
		"signoz_analytics", schemamigrator.SigInsightAnalyticsDB,
		"signoz_meter", schemamigrator.SigInsightMeterDB,
		"signoz_index_v3", "span_index_v3",
		"signoz_index_v2", "span_index_v2",
		"signoz_error_index_v2", "error_index_v2",
		"signoz_spans", "legacy_spans",
	).Replace(value)
}

func (cmd *bootstrap) CreateDatabases(ctx context.Context) error {
	cmd.logger.Info("Creating databases")

	query := `SELECT name FROM system.databases`
	rows, err := cmd.conn.Query(ctx, query)
	if err != nil {
		return NewRetryableError(err)
	}

	databases := map[string]struct{}{}
	for rows.Next() {
		var database string
		if err := rows.Scan(&database); err != nil {
			return err
		}

		databases[database] = struct{}{}
	}

	for _, database := range schemamigrator.Databases {
		if _, ok := databases[database]; !ok {
			command := fmt.Sprintf("CREATE DATABASE %s", database)
			if err := cmd.conn.Exec(ctx, command); err != nil {
				return err
			}
		}
	}

	return nil
}

func (cmd *bootstrap) CreateMigrationTables(ctx context.Context) error {
	cmd.logger.Info("Creating migration tables")

	for _, migration := range schemamigrator.V2MigrationTablesLogs {
		for _, operation := range migration.UpItems {
			shouldRun, err := cmd.ShouldRunOperation(ctx, operation)
			if err != nil {
				return err
			}

			if !shouldRun {
				continue
			}

			err = cmd.migrationManager.RunOperation(ctx, operation, migration.MigrationID, schemamigrator.SigInsightLogsDB, true)
			if err != nil {
				return err
			}
		}
	}

	for _, migration := range schemamigrator.V2MigrationTablesMetrics {
		for _, operation := range migration.UpItems {
			shouldRun, err := cmd.ShouldRunOperation(ctx, operation)
			if err != nil {
				return err
			}

			if !shouldRun {
				continue
			}

			err = cmd.migrationManager.RunOperation(ctx, operation, migration.MigrationID, schemamigrator.SigInsightMetricsDB, true)
			if err != nil {
				return err
			}
		}
	}

	for _, migration := range schemamigrator.V2MigrationTablesTraces {
		for _, operation := range migration.UpItems {
			shouldRun, err := cmd.ShouldRunOperation(ctx, operation)
			if err != nil {
				return err
			}

			if !shouldRun {
				continue
			}

			err = cmd.migrationManager.RunOperation(ctx, operation, migration.MigrationID, schemamigrator.SigInsightTracesDB, true)
			if err != nil {
				return err
			}
		}
	}

	for _, migration := range schemamigrator.V2MigrationTablesMetadata {
		for _, operation := range migration.UpItems {
			shouldRun, err := cmd.ShouldRunOperation(ctx, operation)
			if err != nil {
				return err
			}

			if !shouldRun {
				continue
			}

			err = cmd.migrationManager.RunOperation(ctx, operation, migration.MigrationID, schemamigrator.SigInsightMetadataDB, true)
			if err != nil {
				return err
			}
		}
	}

	for _, migration := range schemamigrator.V2MigrationTablesAnalytics {
		for _, operation := range migration.UpItems {
			shouldRun, err := cmd.ShouldRunOperation(ctx, operation)
			if err != nil {
				return err
			}

			if !shouldRun {
				continue
			}

			err = cmd.migrationManager.RunOperation(ctx, operation, migration.MigrationID, schemamigrator.SigInsightAnalyticsDB, true)
			if err != nil {
				return err
			}
		}
	}

	for _, migration := range schemamigrator.V2MigrationTablesMeter {
		for _, operation := range migration.UpItems {
			shouldRun, err := cmd.ShouldRunOperation(ctx, operation)
			if err != nil {
				return err
			}

			if !shouldRun {
				continue
			}

			err = cmd.migrationManager.RunOperation(ctx, operation, migration.MigrationID, schemamigrator.SigInsightMeterDB, true)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (cmd *bootstrap) ShouldRunOperation(ctx context.Context, operation schemamigrator.Operation) (bool, error) {
	tableOp, ok := operation.(schemamigrator.CreateTableOperation)
	if ok {
		query := fmt.Sprintf(`SELECT count() FROM system.tables WHERE database = '%s' AND name = '%s'`, tableOp.Database, tableOp.Table)

		cmd.logger.Info("Checking if the operation should run", zap.String("sql", query))
		var count uint64
		err := cmd.conn.QueryRow(ctx, query).Scan(&count)
		if err != nil {
			return false, NewRetryableError(err)
		}

		if count > 0 {
			return false, nil
		}
	}

	return true, nil
}
