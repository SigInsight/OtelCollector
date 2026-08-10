package schemamigrator

import (
	"context"
	"fmt"
	"slices"
)

// BaselineState is the canonical fresh-install boundary state.
type BaselineState string

const (
	BaselineStateEmpty        BaselineState = "empty"
	BaselineStateComplete     BaselineState = "complete"
	BaselineStateIncompatible BaselineState = "incompatible"
)

// BaselineSnapshot contains the facts needed to enforce the destructive M16
// boundary. Legacy histories are observed only so they can be rejected.
type BaselineSnapshot struct {
	DomainTableCount         uint64
	LegacyTrackingTableCount uint64
	TrackingTableCount       uint64
	MigrationStatuses        map[uint64]string
}

// BaselineInspection explains why a database is empty, canonical, or rejected.
type BaselineInspection struct {
	Database                 string
	State                    BaselineState
	DomainTableCount         uint64
	LegacyTrackingTableCount uint64
	TrackingTableCount       uint64
	MissingBaselineMarker    bool
	NonFinishedMigrationIDs  []uint64
	UnexpectedMigrationIDs   []uint64
}

const baselineInventoryQuery = `SELECT
	countIf(name NOT IN ('schema_migrations', 'schema_migrations_v2')),
	countIf(name = 'schema_migrations'),
	countIf(name = 'schema_migrations_v2')
FROM system.tables
WHERE database = $1`

func baselineMigrationsQuery(database string) string {
	return fmt.Sprintf("SELECT migration_id, status FROM %s.schema_migrations_v2 SETTINGS final = 1", database)
}

// InspectBaselineState reads only the canonical marker history. It never
// adopts or translates a pre-M16 schema.
func (m *MigrationManager) InspectBaselineState(ctx context.Context, spec BaselineSpec) (BaselineInspection, error) {
	if !isKnownDatabase(spec.Database) {
		return BaselineInspection{}, fmt.Errorf("unknown baseline database %q", spec.Database)
	}

	snapshot := BaselineSnapshot{MigrationStatuses: make(map[uint64]string)}
	if err := m.conn.QueryRow(ctx, baselineInventoryQuery, spec.Database).Scan(
		&snapshot.DomainTableCount,
		&snapshot.LegacyTrackingTableCount,
		&snapshot.TrackingTableCount,
	); err != nil {
		return BaselineInspection{}, fmt.Errorf("inspect tables for database %s: %w", spec.Database, err)
	}

	if snapshot.TrackingTableCount == 1 {
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

// ClassifyBaselineState accepts only a pristine database or an explicitly
// marked canonical baseline plus registered post-baseline migrations.
func ClassifyBaselineState(spec BaselineSpec, snapshot BaselineSnapshot) BaselineInspection {
	result := BaselineInspection{
		Database:                 spec.Database,
		DomainTableCount:         snapshot.DomainTableCount,
		LegacyTrackingTableCount: snapshot.LegacyTrackingTableCount,
		TrackingTableCount:       snapshot.TrackingTableCount,
	}

	if snapshot.DomainTableCount == 0 && snapshot.LegacyTrackingTableCount == 0 && len(snapshot.MigrationStatuses) == 0 {
		result.State = BaselineStateEmpty
		return result
	}

	allowed := migrationIDSet(spec.AllowedMigrationIDs)
	allowed[spec.BaselineMigration.MigrationID] = struct{}{}
	for migrationID, status := range snapshot.MigrationStatuses {
		if _, ok := allowed[migrationID]; !ok {
			result.UnexpectedMigrationIDs = append(result.UnexpectedMigrationIDs, migrationID)
			continue
		}
		if status != FinishedStatus {
			result.NonFinishedMigrationIDs = append(result.NonFinishedMigrationIDs, migrationID)
		}
	}
	result.MissingBaselineMarker = snapshot.MigrationStatuses[spec.BaselineMigration.MigrationID] != FinishedStatus
	slices.Sort(result.NonFinishedMigrationIDs)
	slices.Sort(result.UnexpectedMigrationIDs)

	if snapshot.LegacyTrackingTableCount == 0 &&
		snapshot.TrackingTableCount == 1 &&
		!result.MissingBaselineMarker &&
		len(result.NonFinishedMigrationIDs) == 0 &&
		len(result.UnexpectedMigrationIDs) == 0 {
		result.State = BaselineStateComplete
		return result
	}

	result.State = BaselineStateIncompatible
	return result
}

func migrationIDSet(migrations []uint64) map[uint64]struct{} {
	ids := make(map[uint64]struct{}, len(migrations))
	for _, migrationID := range migrations {
		ids[migrationID] = struct{}{}
	}
	return ids
}

func isKnownDatabase(database string) bool {
	return slices.Contains(Databases, database)
}
