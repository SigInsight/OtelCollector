package schemamigrator

import (
	"strings"
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
	type migrationRange struct {
		baseFirst, baseLast, baseCount             uint64
		requiredFirst, requiredLast, requiredCount uint64
	}
	expected := map[string]migrationRange{
		SigInsightTracesDB:    {1, 27, 27, 1000, 1008, 9},
		SigInsightMetricsDB:   {1, 28, 28, 1000, 1007, 8},
		SigInsightLogsDB:      {1, 9, 9, 1000, 1005, 6},
		SigInsightMetadataDB:  {0, 0, 0, 1000, 1001, 2},
		SigInsightAnalyticsDB: {0, 0, 0, 1, 1, 1},
		SigInsightMeterDB:     {0, 0, 0, 1, 5, 5},
	}

	totalDDL := 0
	for _, spec := range V1BaselineSpecs() {
		require.Equal(t, V1BaselineMigrationID, spec.BaselineMigration.MigrationID)

		want := expected[spec.Database]
		assertMigrationRange(t, want.baseFirst, want.baseLast, want.baseCount, spec.BaseMigrationIDs)
		assertMigrationRange(t, want.requiredFirst, want.requiredLast, want.requiredCount, spec.RequiredMigrationIDs)

		ddlCount := int(v1SchemaFingerprints[spec.Database].TableCount)
		totalDDL += ddlCount
		extraOperations := 0
		if spec.Database == SigInsightMetadataDB {
			extraOperations = 4
		}
		require.Len(t, spec.BaselineMigration.UpItems, ddlCount+extraOperations, spec.Database)

		previousRank := 0
		for _, operation := range spec.BaselineMigration.UpItems[:ddlCount] {
			ddl, ok := operation.(StaticDDLOperation)
			require.True(t, ok, "%s contains a non-static DDL operation", spec.Database)
			assert.NotContains(t, ddl.Query, "{{")
			assert.Equal(t, spec.Database, ddl.Database)

			rank := staticDDLDependencyRank(ddl.Query)
			assert.GreaterOrEqual(t, rank, previousRank, ddl.Table)
			previousRank = rank
		}
	}
	assert.Equal(t, 51, totalDDL)
}

func TestStaticDDLOperationRendersLocalSchema(t *testing.T) {
	operation := v1BaselineDDLOperations[SigInsightAnalyticsDB][0]
	sql := operation.ToSQL()
	assert.NotContains(t, sql, " ON CLUSTER ")
	assert.NotContains(t, sql, "Replicated")
	assert.NotContains(t, sql, "Distributed")
	assert.NotContains(t, sql, "{{")
}

func assertMigrationRange(t *testing.T, first, last, count uint64, ids []uint64) {
	t.Helper()
	require.Len(t, ids, int(count))
	if count == 0 {
		return
	}
	assert.Equal(t, first, ids[0])
	assert.Equal(t, last, ids[len(ids)-1])
}

func staticDDLDependencyRank(query string) int {
	switch {
	case strings.HasPrefix(query, "CREATE MATERIALIZED VIEW"):
		return 2
	default:
		return 0
	}
}
