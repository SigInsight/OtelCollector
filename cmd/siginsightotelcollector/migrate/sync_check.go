package migrate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/SigInsight/OtelCollector/cmd/siginsightotelcollector/config"
	schemamigrator "github.com/SigInsight/OtelCollector/cmd/siginsightschemamigrator/schema_migrator"
	"github.com/cenkalti/backoff/v4"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

type syncCheck struct {
	timeout          time.Duration
	migrationManager *schemamigrator.MigrationManager
	logger           *zap.Logger
}

func registerSyncCheck(parentCmd *cobra.Command, logger *zap.Logger) {
	syncCheckCommand := &cobra.Command{
		Use:          "check",
		Short:        "Checks the status of migrations for the store by checking the status of migrations in the migration table.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			check, err := newSyncCheck(config.Clickhouse.DSN, config.MigrateSyncCheck.Timeout, logger)
			if err != nil {
				return err
			}

			err = check.Run(cmd.Context())
			if err != nil {
				return err
			}

			return nil
		},
	}

	config.MigrateSyncCheck.RegisterFlags(syncCheckCommand)

	parentCmd.AddCommand(syncCheckCommand)
}

func newSyncCheck(dsn string, timeout time.Duration, logger *zap.Logger) (*syncCheck, error) {
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

	return &syncCheck{
		timeout:          timeout,
		migrationManager: migrationManager,
		logger:           logger,
	}, nil
}

func (cmd *syncCheck) Run(ctx context.Context) error {
	backoff := backoff.NewExponentialBackOff()
	backoff.MaxElapsedTime = cmd.timeout

	for {
		err := cmd.Check(ctx)
		if err == nil {
			break
		}

		if !Unwrapb(err).IsRetryable() {
			return fmt.Errorf("failed to check sync migrations: %w", err)
		}

		cmd.logger.Info("Error occurred while checking for sync migrations to complete, retrying", zap.Error(err))
		nextBackOff := backoff.NextBackOff()
		if nextBackOff == backoff.Stop {
			return errors.New("timed out waiting for sync migrations to complete within the configured timeout")
		}
		time.Sleep(nextBackOff)
	}

	return nil
}

func (cmd *syncCheck) Check(ctx context.Context) error {
	return checkPostBaselineMigrations(ctx, cmd.migrationManager, postBaselineSyncPhase)
}
