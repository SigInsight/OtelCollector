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

// BaselineSpec describes required, optional, and fresh-install migration records for one database.
type BaselineSpec struct {
	Database           string
	BaseMigrations     []SchemaMigrationRecord
	RequiredMigrations []SchemaMigrationRecord
	OptionalMigrations []SchemaMigrationRecord
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
	Database                    string
	State                       BaselineState
	LegacyTableExists           bool
	DomainTableCount            uint64
	TrackingTableCount          uint64
	MissingMigrationIDs         []uint64
	NonFinishedMigrationIDs     []uint64
	UnexpectedMigrationIDs      []uint64
	AppliedOptionalMigrationIDs []uint64
}

const baselineInventoryQuery = `SELECT
	countIf(name NOT IN ('schema_migrations', 'schema_migrations_v2', 'distributed_schema_migrations_v2')),
	countIf(name = 'schema_migrations'),
	countIf(name IN ('schema_migrations_v2', 'distributed_schema_migrations_v2'))
FROM system.tables
WHERE database = $1`

func baselineMigrationsQuery(database string) string {
	return fmt.Sprintf(
		"SELECT migration_id, status FROM %s.distributed_schema_migrations_v2 SETTINGS final = 1",
		database,
	)
}

// V1BaselineSpecs returns the supported v1 migration history for every telemetry database.
func V1BaselineSpecs() []BaselineSpec {
	optionalLogsMigrations := make([]SchemaMigrationRecord, 0)
	for _, migration := range LogsMigrationsV2 {
		if migration.MigrationID >= 2000 {
			optionalLogsMigrations = append(optionalLogsMigrations, migration)
		}
	}

	return []BaselineSpec{
		{
			Database:           SigInsightTracesDB,
			BaseMigrations:     SquashedTracesMigrations,
			RequiredMigrations: TracesMigrations,
		},
		{
			Database:           SigInsightMetricsDB,
			BaseMigrations:     SquashedMetricsMigrations,
			RequiredMigrations: MetricsMigrations,
		},
		{
			Database:           SigInsightLogsDB,
			BaseMigrations:     CustomRetentionLogsMigrations,
			RequiredMigrations: LogsMigrations,
			OptionalMigrations: optionalLogsMigrations,
		},
		{
			Database:           SigInsightMetadataDB,
			RequiredMigrations: MetadataMigrations,
		},
		{
			Database:           SigInsightAnalyticsDB,
			RequiredMigrations: AnalyticsMigrations,
		},
		{
			Database:           SigInsightMeterDB,
			RequiredMigrations: MeterMigrations,
		},
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

	if snapshot.TrackingTableCount == 2 {
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

	baseIDs := migrationIDSet(spec.BaseMigrations)
	requiredIDs := migrationIDSet(spec.RequiredMigrations)
	optionalIDs := migrationIDSet(spec.OptionalMigrations)

	allowedIDs := make(map[uint64]struct{}, len(baseIDs)+len(requiredIDs)+len(optionalIDs))
	addMigrationIDs(allowedIDs, baseIDs)
	addMigrationIDs(allowedIDs, requiredIDs)
	addMigrationIDs(allowedIDs, optionalIDs)

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
		if _, ok := optionalIDs[migrationID]; ok && status == FinishedStatus {
			result.AppliedOptionalMigrationIDs = append(result.AppliedOptionalMigrationIDs, migrationID)
		}
	}

	for migrationID := range requiredIDs {
		if _, ok := snapshot.MigrationStatuses[migrationID]; !ok {
			result.MissingMigrationIDs = append(result.MissingMigrationIDs, migrationID)
		}
	}

	slices.Sort(result.MissingMigrationIDs)
	slices.Sort(result.NonFinishedMigrationIDs)
	slices.Sort(result.UnexpectedMigrationIDs)
	slices.Sort(result.AppliedOptionalMigrationIDs)

	if len(result.UnexpectedMigrationIDs) > 0 {
		result.State = BaselineStateAhead
		return result
	}

	if snapshot.DomainTableCount == 0 ||
		snapshot.TrackingTableCount != 2 ||
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

func migrationIDSet(migrations []SchemaMigrationRecord) map[uint64]struct{} {
	ids := make(map[uint64]struct{}, len(migrations))
	for _, migration := range migrations {
		ids[migration.MigrationID] = struct{}{}
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
