package schemamigrator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidatePostBaselineMigrations(t *testing.T) {
	syncMigration := func(database string, id uint64) PostBaselineMigration {
		return PostBaselineMigration{
			Database: database,
			Migration: SchemaMigrationRecord{
				MigrationID: id,
				UpItems: []Operation{AlterTableAddColumn{
					Database: database,
					Table:    "spans",
					Column:   Column{Name: "new_column", Type: ColumnTypeString},
				}},
			},
		}
	}
	asyncMigration := func(database string, id uint64) PostBaselineMigration {
		return PostBaselineMigration{
			Database: database,
			Migration: SchemaMigrationRecord{
				MigrationID: id,
				UpItems: []Operation{AlterTableDropColumn{
					Database: database,
					Table:    "spans",
					Column:   Column{Name: "old_column"},
				}},
			},
		}
	}

	tests := []struct {
		name  string
		sync  []PostBaselineMigration
		async []PostBaselineMigration
		want  string
	}{
		{name: "valid", sync: []PostBaselineMigration{syncMigration(SigInsightLogsDB, 2000)}, async: []PostBaselineMigration{asyncMigration(SigInsightLogsDB, 2001)}},
		{name: "old id", sync: []PostBaselineMigration{syncMigration(SigInsightLogsDB, 1000)}, want: "IDs must be"},
		{name: "unknown database", sync: []PostBaselineMigration{syncMigration("unknown", 2000)}, want: "unknown database"},
		{name: "duplicate across phases", sync: []PostBaselineMigration{syncMigration(SigInsightLogsDB, 2000)}, async: []PostBaselineMigration{asyncMigration(SigInsightLogsDB, 2000)}, want: "duplicate"},
		{name: "out of order", sync: []PostBaselineMigration{syncMigration(SigInsightLogsDB, 2001), syncMigration(SigInsightLogsDB, 2000)}, want: "increasing ID"},
		{name: "empty operations", sync: []PostBaselineMigration{{Database: SigInsightLogsDB, Migration: SchemaMigrationRecord{MigrationID: 2000}}}, want: "no operations"},
		{name: "wrong phase", sync: []PostBaselineMigration{asyncMigration(SigInsightLogsDB, 2000)}, want: "other phase"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePostBaselineMigrations(tc.sync, tc.async)
			if tc.want == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestV1BaselineAllowsRegisteredPostBaselineIDs(t *testing.T) {
	spec := BaselineSpec{
		Database:            SigInsightLogsDB,
		AllowedMigrationIDs: []uint64{2000},
		BaselineMigration:   SchemaMigrationRecord{MigrationID: V1BaselineMigrationID},
	}
	inspection := ClassifyBaselineState(spec, BaselineSnapshot{
		DomainTableCount:   1,
		TrackingTableCount: 1,
		MigrationStatuses: map[uint64]string{
			V1BaselineMigrationID: FinishedStatus,
			2000:                  FinishedStatus,
		},
	})
	require.Equal(t, BaselineStateCompleteFresh, inspection.State)
}

func TestLegacyLogsJSONCleanupMigrations(t *testing.T) {
	cleanup := findPostBaselineMigration(t, SigInsightLogsDB, 2002)
	cleanupSQL := make([]string, 0, len(cleanup.UpItems))
	for _, operation := range cleanup.UpItems {
		cleanupSQL = append(cleanupSQL, operation.ToSQL())
	}
	require.Equal(t, []string{
		"ALTER TABLE siginsight_logs.logs_v2 DROP INDEX IF EXISTS body_v2_string_ngram_idx",
		"ALTER TABLE siginsight_logs.logs_v2 DROP INDEX IF EXISTS body_v2_string_token_idx",
		"ALTER TABLE siginsight_logs.logs_v2 DROP INDEX IF EXISTS body_v2_paths_ngram_idx",
		"ALTER TABLE siginsight_logs.logs_v2 DROP INDEX IF EXISTS body_v2_paths_token_idx",
		"ALTER TABLE siginsight_logs.logs_v2 DROP COLUMN IF EXISTS body_promoted",
		"ALTER TABLE siginsight_logs.logs_v2 DROP COLUMN IF EXISTS body_v2",
		"DROP TABLE IF EXISTS siginsight_metadata.distributed_json_path_types",
		"DROP TABLE IF EXISTS siginsight_metadata.json_path_types",
	}, cleanupSQL)

	require.Equal(t, []uint64{2001, 2002, 2003}, baselineSpecForDatabase(t, SigInsightLogsDB).AllowedMigrationIDs)
}

func TestObsoleteTableCleanupMigrations(t *testing.T) {
	tests := []struct {
		database string
		id       uint64
		tables   []string
	}{
		{SigInsightTracesDB, 2000, []string{
			"distributed_dependency_graph_minutes_v2", "distributed_signoz_error_index_v2", "distributed_signoz_index_v2",
			"distributed_signoz_index_v3", "distributed_signoz_spans", "distributed_span_attributes",
			"distributed_span_attributes_keys", "distributed_tag_attributes_v2", "distributed_top_level_operations",
			"distributed_trace_summary", "distributed_traces_v3_resource", "distributed_usage",
			"distributed_usage_explorer", "span_attributes",
		}},
		{SigInsightTracesDB, 2001, []string{
			"usage_explorer_mv", "usage_explorer", "durationSort", "span_index_v2", "legacy_spans",
		}},
		{SigInsightMetricsDB, 2000, []string{
			"distributed_exp_hist", "distributed_metadata", "distributed_samples_v2", "distributed_samples_v4",
			"distributed_samples_v4_agg_30m", "distributed_samples_v4_agg_5m", "distributed_time_series_v2",
			"distributed_time_series_v4", "distributed_time_series_v4_1day", "distributed_time_series_v4_1week",
			"distributed_time_series_v4_6hrs", "distributed_updated_metadata", "distributed_usage",
			"samples_v2", "time_series_v2",
		}},
		{SigInsightMetricsDB, 2001, []string{"updated_metadata"}},
		{SigInsightLogsDB, 2003, []string{
			"distributed_logs_attribute_keys", "distributed_logs_resource_keys", "distributed_logs_v2",
			"distributed_logs_v2_resource", "distributed_tag_attributes_v2", "distributed_usage",
		}},
		{SigInsightMetadataDB, 2000, []string{
			"distributed_attributes_metadata", "distributed_column_evolution_metadata", "column_evolution_metadata",
		}},
		{SigInsightMetadataDB, 2001, []string{"attributes_metadata"}},
		{SigInsightAnalyticsDB, 2000, []string{"distributed_rule_state_history_v0"}},
		{SigInsightMeterDB, 2000, []string{"distributed_samples", "distributed_samples_agg_1d"}},
	}

	for _, tc := range tests {
		t.Run(tc.database, func(t *testing.T) {
			migration := findPostBaselineMigration(t, tc.database, tc.id)
			require.Len(t, migration.UpItems, len(tc.tables))
			for index, table := range tc.tables {
				require.Equal(t, "DROP TABLE IF EXISTS "+tc.database+"."+table, migration.UpItems[index].ToSQL())
			}
			require.Contains(t, baselineSpecForDatabase(t, tc.database).AllowedMigrationIDs, tc.id)
		})
	}
}

func TestUnusedMetricIngestionTimestampCleanup(t *testing.T) {
	migration := findPostBaselineMigration(t, SigInsightMetricsDB, 2002)
	require.Len(t, migration.UpItems, 2)
	require.Equal(t,
		"ALTER TABLE siginsight_metrics.samples_v4 DROP COLUMN IF EXISTS inserted_at_unix_milli",
		migration.UpItems[0].ToSQL(),
	)
	require.Equal(t,
		"ALTER TABLE siginsight_metrics.time_series_v4 DROP COLUMN IF EXISTS inserted_at_unix_milli",
		migration.UpItems[1].ToSQL(),
	)
	require.Contains(t, baselineSpecForDatabase(t, SigInsightMetricsDB).AllowedMigrationIDs, uint64(2002))
}

func findPostBaselineMigration(t *testing.T, database string, id uint64) SchemaMigrationRecord {
	t.Helper()
	for _, item := range PostBaselineAsyncMigrations {
		if item.Database == database && item.Migration.MigrationID == id {
			return item.Migration
		}
	}
	t.Fatalf("post-baseline migration %s/%d not found", database, id)
	return SchemaMigrationRecord{}
}

func baselineSpecForDatabase(t *testing.T, database string) BaselineSpec {
	t.Helper()
	for _, spec := range V1BaselineSpecs() {
		if spec.Database == database {
			return spec
		}
	}
	t.Fatalf("baseline spec for %s not found", database)
	return BaselineSpec{}
}
