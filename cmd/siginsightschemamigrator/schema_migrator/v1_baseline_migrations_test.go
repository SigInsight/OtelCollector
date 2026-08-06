package schemamigrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyConsolidatedBaselineMarker(t *testing.T) {
	spec := BaselineSpec{
		Database:             SigInsightLogsDB,
		BaseMigrationIDs:     []uint64{1},
		RequiredMigrationIDs: []uint64{1000},
		BaselineMigration:    SchemaMigrationRecord{MigrationID: V1BaselineMigrationID},
	}

	tests := []struct {
		name     string
		snapshot BaselineSnapshot
		state    BaselineState
		missing  []uint64
		nonFinal []uint64
	}{
		{
			name: "finished marker is complete",
			snapshot: BaselineSnapshot{
				DomainTableCount:   1,
				TrackingTableCount: 1,
				MigrationStatuses: map[uint64]string{
					V1BaselineMigrationID: FinishedStatus,
				},
			},
			state: BaselineStateCompleteFresh,
		},
		{
			name: "finished marker remains complete after post-baseline tables are removed",
			snapshot: BaselineSnapshot{
				TrackingTableCount: 1,
				MigrationStatuses: map[uint64]string{
					V1BaselineMigrationID: FinishedStatus,
				},
			},
			state: BaselineStateCompleteFresh,
		},
		{
			name: "failed marker is partial and is not recovered",
			snapshot: BaselineSnapshot{
				DomainTableCount:   1,
				TrackingTableCount: 1,
				MigrationStatuses: map[uint64]string{
					V1BaselineMigrationID: FailedStatus,
				},
			},
			state:    BaselineStatePartial,
			missing:  []uint64{1, 1000},
			nonFinal: []uint64{V1BaselineMigrationID},
		},
		{
			name: "legacy database with marker remains legacy",
			snapshot: BaselineSnapshot{
				DomainTableCount:   1,
				LegacyTableExists:  true,
				TrackingTableCount: 1,
				MigrationStatuses: map[uint64]string{
					V1BaselineMigrationID: FinishedStatus,
				},
			},
			state: BaselineStateCompleteLegacy,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyBaselineState(spec, tc.snapshot)
			assert.Equal(t, tc.state, got.State)
			assert.Equal(t, tc.missing, got.MissingMigrationIDs)
			assert.Equal(t, tc.nonFinal, got.NonFinishedMigrationIDs)
		})
	}
}

func TestV1BaselineSpecsUseStaticFinalSchema(t *testing.T) {
	for _, spec := range V1BaselineSpecs() {
		require.Equal(t, V1BaselineMigrationID, spec.BaselineMigration.MigrationID)
		require.Empty(t, spec.AllowedMigrationIDs, spec.Database)
		require.Len(t, spec.BaselineMigration.UpItems, int(v1SchemaFingerprints[spec.Database].TableCount), spec.Database)

		previousRank := 0
		for _, operation := range spec.BaselineMigration.UpItems {
			ddl, ok := operation.(StaticDDLOperation)
			require.True(t, ok, "%s contains a non-static DDL operation", spec.Database)
			assert.NotContains(t, ddl.Query, "{{")
			assert.Equal(t, spec.Database, ddl.Database)

			rank := staticDDLDependencyRank(ddl.Query)
			assert.GreaterOrEqual(t, rank, previousRank, ddl.Table)
			previousRank = rank
		}
	}
}

func TestStaticDDLOperationRendersLocalSchema(t *testing.T) {
	operation := v1BaselineDDLOperations[SigInsightAnalyticsDB][0]
	sql := operation.ToSQL()
	assert.NotContains(t, sql, " ON CLUSTER ")
	assert.NotContains(t, sql, "Replicated")
	assert.NotContains(t, sql, "Distributed")
	assert.NotContains(t, sql, "{{")
}
