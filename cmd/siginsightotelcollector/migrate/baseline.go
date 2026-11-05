package migrate

import (
	"context"
	"errors"
	"fmt"
	"strings"

	schemamigrator "github.com/SigInsight/OtelCollector/cmd/siginsightschemamigrator/schema_migrator"
	"go.uber.org/zap"
)

// ensureCanonicalBaseline creates the destructive canonical schema only in a
// pristine database. Existing unmarked schemas are never adopted.
func ensureCanonicalBaseline(ctx context.Context, manager *schemamigrator.MigrationManager, logger *zap.Logger) error {
	if err := schemamigrator.ValidatePostBaselineMigrations(); err != nil {
		return New(err)
	}

	report, err := manager.InspectCanonicalBaselineState(ctx)
	if err != nil {
		return NewRetryableError(err)
	}

	switch report.State {
	case schemamigrator.BaselineStateEmpty:
		// No recovery is attempted after the first DDL succeeds. A later failure
		// leaves observable tables or a failed marker, so the next run is partial.
		logger.Info("initializing canonical schema baseline")
		for _, spec := range schemamigrator.CanonicalBaselineSpecs() {
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
	case schemamigrator.BaselineStateComplete:
		return nil
	default:
		return New(formatBaselineStateError(report))
	}

	specs := schemamigrator.CanonicalBaselineSpecs()
	if err := verifyCanonicalSchemas(ctx, manager, specs); err != nil {
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

func requireCanonicalBaseline(ctx context.Context, manager *schemamigrator.MigrationManager) error {
	if err := schemamigrator.ValidatePostBaselineMigrations(); err != nil {
		return New(err)
	}

	report, err := manager.InspectCanonicalBaselineState(ctx)
	if err != nil {
		return NewRetryableError(err)
	}
	if report.State != schemamigrator.BaselineStateComplete {
		return New(formatBaselineStateError(report))
	}
	return nil
}

func verifyCanonicalSchemas(
	ctx context.Context,
	manager *schemamigrator.MigrationManager,
	specs []schemamigrator.BaselineSpec,
) error {
	for _, spec := range specs {
		if err := manager.VerifyCanonicalSchema(ctx, spec.Database); err != nil {
			if errors.Is(err, schemamigrator.ErrCanonicalSchemaMismatch) {
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
		if database.State == schemamigrator.BaselineStateComplete {
			continue
		}
		details = append(details, fmt.Sprintf(
			"%s=%s domain_tables=%d legacy_tracking=%d tracking=%d missing_marker=%t non_finished=%v unexpected=%v",
			database.Database,
			database.State,
			database.DomainTableCount,
			database.LegacyTrackingTableCount,
			database.TrackingTableCount,
			database.MissingBaselineMarker,
			database.NonFinishedMigrationIDs,
			database.UnexpectedMigrationIDs,
		))
	}
	return fmt.Errorf(
		"canonical baseline is %s; automatic migration recovery is disabled. "+
			"This collector installs a destructive canonical schema and cannot adopt an existing telemetry schema. "+
			"Use a new ClickHouse data directory or explicitly discard the siginsight_* telemetry databases before retrying; "+
			"a ClickHouse-only reset does not remove SQL-store data: %s",
		report.State,
		strings.Join(details, "; "),
	)
}
