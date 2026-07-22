package migrate

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/SigInsight/OtelCollector/cmd/siginsightotelcollector/config"
	schemamigrator "github.com/SigInsight/OtelCollector/cmd/siginsightschemamigrator/schema_migrator"
	"github.com/SigInsight/OtelCollector/constants"
	"github.com/cenkalti/backoff/v4"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

type syncUp struct {
	conn             clickhouse.Conn
	cluster          string
	migrationManager *schemamigrator.MigrationManager
	timeout          time.Duration
	logger           *zap.Logger
}

func registerSyncUp(parentCmd *cobra.Command, logger *zap.Logger) {
	syncUpCommand := &cobra.Command{
		Use:          "up",
		Short:        "Runs 'up' sync migrations for the store. Up migrations are used to apply new migrations to the store.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			up, err := newSyncUp(config.Clickhouse.DSN, config.Clickhouse.Cluster, config.Clickhouse.Replication, config.MigrateSyncUp.Timeout, logger)
			if err != nil {
				return err
			}

			err = up.Run(cmd.Context())
			if err != nil {
				return err
			}

			return nil
		},
	}

	config.MigrateSyncUp.RegisterFlags(syncUpCommand)

	parentCmd.AddCommand(syncUpCommand)
}

func newSyncUp(dsn string, cluster string, replication bool, timeout time.Duration, logger *zap.Logger) (*syncUp, error) {
	opts, err := clickhouse.ParseDSN(dsn)
	if err != nil {
		return nil, err
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, err
	}

	migrationManager, err := schemamigrator.NewMigrationManager(
		schemamigrator.WithClusterName(cluster),
		schemamigrator.WithReplicationEnabled(replication),
		schemamigrator.WithConn(conn),
		schemamigrator.WithConnOptions(*opts),
		schemamigrator.WithLogger(logger),
	)

	return &syncUp{
		conn:             conn,
		cluster:          cluster,
		migrationManager: migrationManager,
		timeout:          timeout,
		logger:           logger,
	}, nil
}

func (cmd *syncUp) Run(ctx context.Context) error {
	if constants.EnableLogsMigrationsV2 {
		if err := ensureLogsJSONSupport(ctx, cmd.conn); err != nil {
			return err
		}
	}

	backoff := backoff.NewExponentialBackOff()
	backoff.MaxElapsedTime = cmd.timeout

	for {
		err := cmd.SyncUp(ctx)
		if err == nil {
			break
		}

		migrateErr := Unwrapb(err)
		// exit early for non-retryable errors.
		if !migrateErr.IsRetryable() {
			return fmt.Errorf("failed to run migrations up sync: %w", err)
		}

		cmd.logger.Info("Error occurred while running migrations up sync, retrying", zap.Error(err))
		nextBackOff := backoff.NextBackOff()
		if nextBackOff == backoff.Stop {
			return fmt.Errorf("timed out waiting for sync up to complete within the configured timeout of %s", cmd.timeout)
		}

		time.Sleep(nextBackOff)
	}

	return nil
}

func (cmd *syncUp) SyncUp(ctx context.Context) error {
	err := cmd.runSquashedMigrations(ctx)
	if err != nil {
		return err
	}

	cmd.logger.Info("running sync migrations")
	err = cmd.run(ctx, schemamigrator.TracesMigrations, schemamigrator.SigInsightTracesDB)
	if err != nil {
		return err
	}

	logsMigrations := schemamigrator.LogsMigrations
	if constants.EnableLogsMigrationsV2 {
		logsMigrations = schemamigrator.LogsMigrationsV2
	}

	err = cmd.run(ctx, logsMigrations, schemamigrator.SigInsightLogsDB)
	if err != nil {
		return err
	}

	err = cmd.run(ctx, schemamigrator.MetricsMigrations, schemamigrator.SigInsightMetricsDB)
	if err != nil {
		return err
	}

	err = cmd.run(ctx, schemamigrator.MetadataMigrations, schemamigrator.SigInsightMetadataDB)
	if err != nil {
		return err
	}

	err = cmd.run(ctx, schemamigrator.AnalyticsMigrations, schemamigrator.SigInsightAnalyticsDB)
	if err != nil {
		return err
	}

	err = cmd.run(ctx, schemamigrator.MeterMigrations, schemamigrator.SigInsightMeterDB)
	if err != nil {
		return err
	}

	return nil
}

func (cmd *syncUp) runSquashedMigrations(ctx context.Context) error {
	squashedMigrations := []struct {
		database   string
		migrations []schemamigrator.SchemaMigrationRecord
	}{
		{schemamigrator.SigInsightLogsDB, schemamigrator.CustomRetentionLogsMigrations},
		{schemamigrator.SigInsightMetricsDB, schemamigrator.SquashedMetricsMigrations},
		{schemamigrator.SigInsightTracesDB, schemamigrator.SquashedTracesMigrations},
	}

	for _, item := range squashedMigrations {
		database := item.database
		cmd.logger.Info("checking if should run squashed migrations", zap.String("database", database))
		should, err := cmd.migrationManager.ShouldRunSquashedV2(ctx, database)
		if err != nil {
			return NewRetryableError(err)
		}

		if !should {
			cmd.logger.Info("skipping squashed migrations", zap.String("database", database))
			continue
		}

		cmd.logger.Info("running squashed migrations", zap.String("database", database))

		err = cmd.run(ctx, item.migrations, database)
		if err != nil {
			return err
		}
	}

	return nil
}

func (cmd *syncUp) run(ctx context.Context, migrations []schemamigrator.SchemaMigrationRecord, db string) error {
	for _, migration := range migrations {
		if !cmd.migrationManager.IsSync(migration) {
			continue
		}

		// TODO: Figure out how to run migrations on all shards when replication is not enabled.
		ok, err := cmd.migrationManager.CheckMigrationStatus(ctx, db, migration.MigrationID, schemamigrator.FinishedStatus)
		if err != nil {
			return NewRetryableError(err)
		}

		if ok {
			continue
		}

		for _, item := range migration.UpItems {
			if err := cmd.migrationManager.RunOperationWithoutUpdate(ctx, item, migration.MigrationID, db); err != nil {
				cmd.logger.Error("Error occurred while running operation", zap.Error(err))

				// if any one of the operations fails, mark the migration as failed
				if err := cmd.migrationManager.InsertMigrationEntry(ctx, db, migration.MigrationID, schemamigrator.FailedStatus); err != nil {
					return err
				}

				return err
			}
		}

		// if all the operations succeed, mark the migration as finished
		if err := cmd.migrationManager.InsertMigrationEntry(ctx, db, migration.MigrationID, schemamigrator.FinishedStatus); err != nil {
			return NewRetryableError(err)
		}
	}

	return nil
}
