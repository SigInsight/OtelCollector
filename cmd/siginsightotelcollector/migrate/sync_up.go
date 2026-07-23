package migrate

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/SigInsight/OtelCollector/cmd/siginsightotelcollector/config"
	schemamigrator "github.com/SigInsight/OtelCollector/cmd/siginsightschemamigrator/schema_migrator"
	"github.com/cenkalti/backoff/v4"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

type syncUp struct {
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
		migrationManager: migrationManager,
		timeout:          timeout,
		logger:           logger,
	}, nil
}

func (cmd *syncUp) Run(ctx context.Context) error {
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
	if err := ensureV1Baseline(ctx, cmd.migrationManager, cmd.logger); err != nil {
		return err
	}
	return runPostBaselineMigrations(ctx, cmd.migrationManager, postBaselineSyncPhase, cmd.logger)
}
