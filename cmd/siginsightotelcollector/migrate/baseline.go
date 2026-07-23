package migrate

import (
	"context"
	"errors"
	"fmt"
	"strings"

	schemamigrator "github.com/SigInsight/OtelCollector/cmd/siginsightschemamigrator/schema_migrator"
	"go.uber.org/zap"
)

// ensureV1Baseline is the only path that creates or adopts the frozen v1
// schema. Once every database has a finished 999 marker, later schema changes
// are handled by the post-baseline migration registry.
func ensureV1Baseline(ctx context.Context, manager *schemamigrator.MigrationManager, logger *zap.Logger) error {
	if err := schemamigrator.ValidatePostBaselineMigrations(); err != nil {
		return New(err)
	}

	report, err := manager.InspectV1BaselineState(ctx)
	if err != nil {
		return NewRetryableError(err)
	}

	switch report.State {
	case schemamigrator.BaselineStateEmpty:
		// No recovery is attempted after the first DDL succeeds. A later failure
		// leaves observable tables or a failed marker, so the next run is partial.
		logger.Info("initializing consolidated v1 schema baseline")
		for _, spec := range schemamigrator.V1BaselineSpecs() {
			for _, operation := range spec.BaselineMigration.UpItems {
				if err := manager.RunOperationWithoutUpdate(
					ctx,
					operation,
					spec.BaselineMigration.MigrationID,
					spec.Database,
				); err != nil {
					markerErr := manager.InsertMigrationEntry(
						ctx,
						spec.Database,
						spec.BaselineMigration.MigrationID,
						schemamigrator.FailedStatus,
					)
					return New(errors.Join(err, markerErr))
				}
			}
		}
	case schemamigrator.BaselineStateCompleteFresh, schemamigrator.BaselineStateCompleteLegacy:
		logger.Info("adopting completed v1 schema as consolidated baseline",
			zap.String("state", string(report.State)))
		finishedMarkers, err := countFinishedV1BaselineMarkers(ctx, manager)
		if err != nil {
			return NewRetryableError(err)
		}
		if finishedMarkers == len(schemamigrator.V1BaselineSpecs()) {
			// The exact v1 fingerprint was verified before these markers were
			// written. Post-baseline migrations may change that fingerprint.
			return nil
		}
		if finishedMarkers != 0 {
			return New(fmt.Errorf("v1 baseline has markers for %d/%d databases; automatic recovery is disabled", finishedMarkers, len(schemamigrator.V1BaselineSpecs())))
		}
	default:
		return New(formatBaselineStateError(report))
	}

	specs := schemamigrator.V1BaselineSpecs()
	if err := verifyV1BaselineSchemas(ctx, manager, specs); err != nil {
		return err
	}

	for _, spec := range specs {
		finished, err := manager.CheckMigrationStatus(
			ctx,
			spec.Database,
			spec.BaselineMigration.MigrationID,
			schemamigrator.FinishedStatus,
		)
		if err != nil {
			return NewRetryableError(err)
		}
		if finished {
			continue
		}
		if err := manager.InsertMigrationEntry(
			ctx,
			spec.Database,
			spec.BaselineMigration.MigrationID,
			schemamigrator.FinishedStatus,
		); err != nil {
			return NewRetryableError(err)
		}
	}

	return nil
}

func requireV1Baseline(ctx context.Context, manager *schemamigrator.MigrationManager) error {
	if err := schemamigrator.ValidatePostBaselineMigrations(); err != nil {
		return New(err)
	}

	report, err := manager.InspectV1BaselineState(ctx)
	if err != nil {
		return NewRetryableError(err)
	}
	if report.State != schemamigrator.BaselineStateCompleteFresh &&
		report.State != schemamigrator.BaselineStateCompleteLegacy {
		return New(formatBaselineStateError(report))
	}

	finishedMarkers, err := countFinishedV1BaselineMarkers(ctx, manager)
	if err != nil {
		return NewRetryableError(err)
	}
	if finishedMarkers != len(schemamigrator.V1BaselineSpecs()) {
		return New(fmt.Errorf("v1 baseline is complete but not all 999 markers are finished; run sync up first"))
	}
	return nil
}

func countFinishedV1BaselineMarkers(ctx context.Context, manager *schemamigrator.MigrationManager) (int, error) {
	finished := 0
	for _, spec := range schemamigrator.V1BaselineSpecs() {
		ok, err := manager.CheckMigrationStatus(ctx, spec.Database, spec.BaselineMigration.MigrationID, schemamigrator.FinishedStatus)
		if err != nil {
			return 0, err
		}
		if ok {
			finished++
		}
	}
	return finished, nil
}

func verifyV1BaselineSchemas(
	ctx context.Context,
	manager *schemamigrator.MigrationManager,
	specs []schemamigrator.BaselineSpec,
) error {
	for _, spec := range specs {
		if err := manager.VerifyV1BaselineSchema(ctx, spec); err != nil {
			if errors.Is(err, schemamigrator.ErrV1SchemaMismatch) {
				return New(err)
			}
			return NewRetryableError(err)
		}
	}
	return nil
}

func formatBaselineStateError(report schemamigrator.BaselineReport) error {
	var details []string
	for _, database := range report.Databases {
		if database.State == schemamigrator.BaselineStateCompleteFresh ||
			database.State == schemamigrator.BaselineStateCompleteLegacy {
			continue
		}
		details = append(details, fmt.Sprintf(
			"%s=%s missing=%v non_finished=%v unexpected=%v",
			database.Database,
			database.State,
			database.MissingMigrationIDs,
			database.NonFinishedMigrationIDs,
			database.UnexpectedMigrationIDs,
		))
	}
	return fmt.Errorf(
		"v1 baseline is %s; automatic migration recovery is disabled: %s",
		report.State,
		strings.Join(details, "; "),
	)
}
