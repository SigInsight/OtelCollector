package migrate

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/SigInsight/OtelCollector/cmd/siginsightotelcollector/config"
	"github.com/cenkalti/backoff/v4"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

type ready struct {
	conn    clickhouse.Conn
	timeout time.Duration
	logger  *zap.Logger
}

func registerReady(parentCmd *cobra.Command, logger *zap.Logger) {
	readyCmd := &cobra.Command{
		Use:   "ready",
		Short: "Checks if the store is ready to run migrations.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ready, err := newReady(
				config.Clickhouse.DSN,
				config.MigrateReady.Timeout,
				logger,
			)
			if err != nil {
				return err
			}

			err = ready.Run(cmd.Context())
			if err != nil {
				return err
			}

			return nil
		},
	}

	config.MigrateReady.RegisterFlags(readyCmd)
	parentCmd.AddCommand(readyCmd)
}

func newReady(dsn string, timeout time.Duration, logger *zap.Logger) (*ready, error) {
	opts, err := clickhouse.ParseDSN(dsn)
	if err != nil {
		return nil, err
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, err
	}

	return &ready{
		conn:    conn,
		timeout: timeout,
		logger:  logger,
	}, nil
}

func (r *ready) Run(ctx context.Context) error {
	backoff := backoff.NewExponentialBackOff()
	backoff.MaxElapsedTime = r.timeout

	for {
		err := r.Ready(ctx)
		if err == nil {
			break
		}

		migrateErr := Unwrapb(err)
		// exit early for non-retryable errors.
		if !migrateErr.IsRetryable() {
			return fmt.Errorf("store not ready due to non-retryable error: %w", err)
		}

		r.logger.Info("Waiting for store to be in ready state", zap.Error(err))
		nextBackOff := backoff.NextBackOff()
		if nextBackOff == backoff.Stop {
			return fmt.Errorf("timed out waiting for store readiness checks to pass within the configured timeout of %s", r.timeout)
		}
		time.Sleep(nextBackOff)
	}

	return nil
}

func (r *ready) Ready(ctx context.Context) error {
	if err := r.CheckClickhouse(ctx); err != nil {
		return err
	}

	return nil
}

func (r *ready) CheckClickhouse(ctx context.Context) error {
	if err := r.conn.Ping(ctx); err != nil {
		return NewRetryableError(err)
	}
	r.logger.Info("clickhouse is ready")
	return nil
}
