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

type asyncUp struct {
	migrationManager *schemamigrator.MigrationManager
	timeout          time.Duration
	logger           *zap.Logger
}

func registerAsyncUp(parentCmd *cobra.Command, logger *zap.Logger) {
	syncUpCommand := &cobra.Command{
		Use:          "up",
		Short:        "Runs 'up' async migrations for the store. Up async migrations are used to apply new async migrations to the store.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			up, err := newAsyncUp(config.Clickhouse.DSN, config.Clickhouse.Cluster, config.Clickhouse.Replication, config.MigrateSyncUp.Timeout, logger)
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

	config.MigrateAsyncUp.RegisterFlags(syncUpCommand)

	parentCmd.AddCommand(syncUpCommand)
}

func newAsyncUp(dsn string, cluster string, replication bool, timeout time.Duration, logger *zap.Logger) (*asyncUp, error) {
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

	return &asyncUp{
		migrationManager: migrationManager,
		timeout:          timeout,
		logger:           logger.With(zap.String("type", "async"), zap.String("subcommand", "up")),
	}, nil
}

func (cmd *asyncUp) Run(ctx context.Context) error {
	backoff := backoff.NewExponentialBackOff()
	backoff.MaxElapsedTime = cmd.timeout

	for {
		err := cmd.Up(ctx)
		if err == nil {
			break
		}

		migrateErr := Unwrapb(err)
		// exit early for non-retryable errors.
		if !migrateErr.IsRetryable() {
			return fmt.Errorf("failed to run migrations: %w", err)
		}

		nextBackOff := backoff.NextBackOff()
		cmd.logger.Info("Retryable error occurred while running migrations", zap.Error(err), zap.Duration("retry_after", nextBackOff))

		if nextBackOff == backoff.Stop {
			return fmt.Errorf("timed out waiting for migration to complete within the configured timeout of %s", cmd.timeout)
		}

		time.Sleep(nextBackOff)
	}

	return nil
}

func (cmd *asyncUp) Up(ctx context.Context) error {
	return runPostBaselineMigrations(ctx, cmd.migrationManager, postBaselineAsyncPhase, cmd.logger)
}
