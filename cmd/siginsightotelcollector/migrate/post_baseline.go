package migrate

import (
	"context"
	"errors"
	"fmt"

	schemamigrator "github.com/SigInsight/OtelCollector/cmd/siginsightschemamigrator/schema_migrator"
	"go.uber.org/zap"
)

type postBaselinePhase string

const (
	postBaselineSyncPhase  postBaselinePhase = "sync"
	postBaselineAsyncPhase postBaselinePhase = "async"
)

func postBaselineMigrations(phase postBaselinePhase) []schemamigrator.PostBaselineMigration {
	if phase == postBaselineSyncPhase {
		return schemamigrator.PostBaselineSyncMigrations
	}
	return schemamigrator.PostBaselineAsyncMigrations
}

// runPostBaselineMigrations executes only registered migrations. An in-progress
// or failed record is never retried automatically; an operator must resolve it
// before the migration history can be accepted again.
func runPostBaselineMigrations(ctx context.Context, manager *schemamigrator.MigrationManager, phase postBaselinePhase, logger *zap.Logger) error {
	if err := schemamigrator.ValidatePostBaselineMigrations(); err != nil {
		return New(err)
	}
	if err := requireV1Baseline(ctx, manager); err != nil {
		return err
	}

	for _, item := range postBaselineMigrations(phase) {
		if err := requireEarlierPostBaselineMigrations(ctx, manager, item); err != nil {
			return err
		}
		status, exists, err := manager.MigrationStatus(ctx, item.Database, item.Migration.MigrationID)
		if err != nil {
			return NewRetryableError(err)
		}
		if exists {
			if status != schemamigrator.FinishedStatus {
				return New(fmt.Errorf("post-baseline migration %s/%d has status %q; automatic recovery is disabled", item.Database, item.Migration.MigrationID, status))
			}
			continue
		}

		if err := manager.InsertMigrationEntry(ctx, item.Database, item.Migration.MigrationID, schemamigrator.InProgressStatus); err != nil {
			return NewRetryableError(err)
		}
		for _, operation := range item.Migration.UpItems {
			if err := manager.RunOperationWithoutUpdate(ctx, operation, item.Migration.MigrationID, item.Database); err != nil {
				markerErr := manager.InsertMigrationEntry(ctx, item.Database, item.Migration.MigrationID, schemamigrator.FailedStatus)
				return New(errors.Join(err, markerErr))
			}
		}
		if err := manager.InsertMigrationEntry(ctx, item.Database, item.Migration.MigrationID, schemamigrator.FinishedStatus); err != nil {
			return New(err)
		}
		logger.Info("post-baseline migration completed", zap.String("database", item.Database), zap.Uint64("migration_id", item.Migration.MigrationID))
	}
	return nil
}

func checkPostBaselineMigrations(ctx context.Context, manager *schemamigrator.MigrationManager, phase postBaselinePhase) error {
	if err := schemamigrator.ValidatePostBaselineMigrations(); err != nil {
		return New(err)
	}
	if err := requireV1Baseline(ctx, manager); err != nil {
		return err
	}
	for _, item := range postBaselineMigrations(phase) {
		if err := requireEarlierPostBaselineMigrations(ctx, manager, item); err != nil {
			return err
		}
		status, exists, err := manager.MigrationStatus(ctx, item.Database, item.Migration.MigrationID)
		if err != nil {
			return NewRetryableError(err)
		}
		if !exists {
			return NewRetryableError(fmt.Errorf("post-baseline migration %s/%d has not been completed", item.Database, item.Migration.MigrationID))
		}
		if status != schemamigrator.FinishedStatus {
			return New(fmt.Errorf("post-baseline migration %s/%d has status %q; automatic recovery is disabled", item.Database, item.Migration.MigrationID, status))
		}
	}
	return nil
}

func requireEarlierPostBaselineMigrations(ctx context.Context, manager *schemamigrator.MigrationManager, current schemamigrator.PostBaselineMigration) error {
	all := make([]schemamigrator.PostBaselineMigration, 0, len(schemamigrator.PostBaselineSyncMigrations)+len(schemamigrator.PostBaselineAsyncMigrations))
	all = append(all, schemamigrator.PostBaselineSyncMigrations...)
	all = append(all, schemamigrator.PostBaselineAsyncMigrations...)
	for _, earlier := range all {
		if earlier.Database != current.Database || earlier.Migration.MigrationID >= current.Migration.MigrationID {
			continue
		}
		status, exists, err := manager.MigrationStatus(ctx, earlier.Database, earlier.Migration.MigrationID)
		if err != nil {
			return NewRetryableError(err)
		}
		if !exists {
			return New(fmt.Errorf("post-baseline migration %s/%d must finish before %s/%d", earlier.Database, earlier.Migration.MigrationID, current.Database, current.Migration.MigrationID))
		}
		if status != schemamigrator.FinishedStatus {
			return New(fmt.Errorf("post-baseline migration %s/%d has status %q; automatic recovery is disabled", earlier.Database, earlier.Migration.MigrationID, status))
		}
	}
	return nil
}
