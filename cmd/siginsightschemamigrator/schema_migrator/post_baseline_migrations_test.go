package schemamigrator

import (
	"regexp"
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

	require.Equal(t, []uint64{2001, 2002, 2003, 2004, 2005}, baselineSpecForDatabase(t, SigInsightLogsDB).AllowedMigrationIDs)
}

func TestCanonicalLogsMigrations(t *testing.T) {
	create := findMigrationIn(t, PostBaselineSyncMigrations, SigInsightLogsDB, 2004)
	require.Len(t, create.UpItems, 3)

	createdTables := make([]string, 0, len(create.UpItems))
	for _, operation := range create.UpItems {
		ddl, ok := operation.(StaticDDLOperation)
		require.True(t, ok)
		createdTables = append(createdTables, ddl.Table)
		require.NotContains(t, ddl.Query, "siginsight_logs.logs_v2")
		require.NotContains(t, ddl.Query, "siginsight_logs.tag_attributes_v2")
	}
	require.ElementsMatch(t, []string{"logs", "resource_sets", "field_values"}, createdTables)

	drop := findPostBaselineMigration(t, SigInsightLogsDB, 2005)
	dropSQL := make([]string, 0, len(drop.UpItems))
	for _, operation := range drop.UpItems {
		dropSQL = append(dropSQL, operation.ToSQL())
	}
	require.Equal(t, []string{
		"DROP TABLE IF EXISTS siginsight_logs.logs_v2",
		"DROP TABLE IF EXISTS siginsight_logs.logs_v2_resource",
		"DROP TABLE IF EXISTS siginsight_logs.tag_attributes_v2",
	}, dropSQL)
}

func TestCanonicalCreateMigrationsRewriteEverySelectedObject(t *testing.T) {
	syncIDs := map[string]uint64{
		SigInsightTracesDB:    2002,
		SigInsightMetricsDB:   2003,
		SigInsightLogsDB:      2004,
		SigInsightAnalyticsDB: 2001,
		SigInsightMeterDB:     2001,
	}

	for database, renames := range canonicalSchemaRenames {
		t.Run(database, func(t *testing.T) {
			migration := findMigrationIn(t, PostBaselineSyncMigrations, database, syncIDs[database])
			require.Len(t, migration.UpItems, len(renames))

			canonicalNames := make([]string, 0, len(migration.UpItems))
			for _, operation := range migration.UpItems {
				ddl, ok := operation.(StaticDDLOperation)
				require.True(t, ok)
				canonicalNames = append(canonicalNames, ddl.Table)
				for _, rename := range renames {
					require.NotRegexp(t,
						regexp.QuoteMeta(database+"."+rename.from)+`(?:\s|\(|$)`,
						ddl.Query,
					)
				}
			}

			want := make([]string, 0, len(renames))
			for _, rename := range renames {
				want = append(want, rename.to)
			}
			require.ElementsMatch(t, want, canonicalNames)
		})
	}
}

func TestCanonicalMaterializedViewsUseCanonicalSourcesAndTargets(t *testing.T) {
	tests := []struct {
		database string
		table    string
		contains []string
	}{
		{SigInsightTracesDB, "root_operations_mv", []string{"TO siginsight_traces.operations", "FROM siginsight_traces.spans"}},
		{SigInsightTracesDB, "trace_summary_from_spans_mv", []string{"TO siginsight_traces.trace_summary", "FROM siginsight_traces.spans"}},
		{SigInsightMetricsDB, "metric_rollup_30m_mv", []string{"TO siginsight_metrics.metric_rollup_30m", "FROM siginsight_metrics.metric_rollup_5m"}},
		{SigInsightMetricsDB, "metric_series_1w_mv", []string{"TO siginsight_metrics.metric_series_1w", "FROM siginsight_metrics.metric_series_1d"}},
		{SigInsightMeterDB, "meter_rollup_1d_mv", []string{"TO siginsight_meter.meter_rollup_1d", "FROM siginsight_meter.meter_points"}},
	}

	for _, tc := range tests {
		t.Run(tc.database+"/"+tc.table, func(t *testing.T) {
			var query string
			for _, operation := range canonicalCreateOperations(tc.database) {
				ddl := operation.(StaticDDLOperation)
				if ddl.Table == tc.table {
					query = ddl.Query
					break
				}
			}
			require.NotEmpty(t, query)
			for _, fragment := range tc.contains {
				require.Contains(t, query, fragment)
			}
		})
	}
}

func TestCanonicalTraceColumnsUseFirstVersionNames(t *testing.T) {
	var spansDDL string
	for _, operation := range canonicalCreateOperations(SigInsightTracesDB) {
		ddl := operation.(StaticDDLOperation)
		if ddl.Table == "spans" {
			spansDDL = ddl.Query
			break
		}
	}
	require.NotEmpty(t, spansDDL)
	require.NotContains(t, spansDDL, " ALIAS ")

	for _, rename := range canonicalTraceColumnRenames {
		require.NotContains(t, spansDDL, rename.from)
		require.Contains(t, spansDDL, "`"+rename.to+"`")
	}

	for _, operation := range canonicalCreateOperations(SigInsightTracesDB) {
		ddl := operation.(StaticDDLOperation)
		for _, rename := range canonicalTraceColumnRenames {
			require.NotContains(t, ddl.Query, rename.from, ddl.Table)
		}
	}
}

func TestCanonicalCleanupDropsEveryLegacyObject(t *testing.T) {
	asyncIDs := map[string]uint64{
		SigInsightTracesDB:    2003,
		SigInsightMetricsDB:   2004,
		SigInsightLogsDB:      2005,
		SigInsightAnalyticsDB: 2002,
		SigInsightMeterDB:     2002,
	}

	for database, id := range asyncIDs {
		t.Run(database, func(t *testing.T) {
			migration := findPostBaselineMigration(t, database, id)
			operations := migration.UpItems
			if database == SigInsightMetricsDB {
				require.Len(t, operations, len(canonicalLegacyTables(database))+2)
				require.Equal(t, "ALTER TABLE siginsight_metrics.metric_points DROP COLUMN IF EXISTS inserted_at_unix_milli", operations[0].ToSQL())
				require.Equal(t, "ALTER TABLE siginsight_metrics.metric_series DROP COLUMN IF EXISTS inserted_at_unix_milli", operations[1].ToSQL())
				operations = operations[2:]
			}

			legacyTables := canonicalLegacyTables(database)
			require.Len(t, operations, len(legacyTables))
			for index, table := range legacyTables {
				require.Equal(t, "DROP TABLE IF EXISTS "+database+"."+table, operations[index].ToSQL())
			}
		})
	}
}

func TestCanonicalSchemaCutovers(t *testing.T) {
	tests := []struct {
		database      string
		createID      uint64
		dropID        uint64
		canonical     []string
		dropPrefixOps int
	}{
		{SigInsightTracesDB, 2002, 2003, []string{
			"spans", "resource_sets", "field_values", "exceptions", "operations",
			"root_operations_mv", "sub_root_operations_mv", "trace_summary_from_spans_mv",
			"service_edges", "service_edges_service_calls_mv", "service_edges_db_calls_mv",
			"service_edges_messaging_calls_mv",
		}, 0},
		{SigInsightMetricsDB, 2003, 2004, []string{
			"metric_points", "metric_rollup_5m", "metric_rollup_5m_mv",
			"metric_rollup_30m", "metric_rollup_30m_mv", "metric_series",
			"metric_series_6h", "metric_series_6h_mv", "metric_series_1d",
			"metric_series_1d_mv", "metric_series_1w", "metric_series_1w_mv",
		}, 2},
		{SigInsightLogsDB, 2004, 2005, []string{"logs", "resource_sets", "field_values"}, 0},
		{SigInsightAnalyticsDB, 2001, 2002, []string{"rule_state_history"}, 0},
		{SigInsightMeterDB, 2001, 2002, []string{"meter_points", "meter_rollup_1d", "meter_rollup_1d_mv"}, 0},
	}
	versionSuffix := regexp.MustCompile(`_v[0-9]+(?:_|$)`)

	for _, tc := range tests {
		t.Run(tc.database, func(t *testing.T) {
			create := findMigrationIn(t, PostBaselineSyncMigrations, tc.database, tc.createID)
			require.Len(t, create.UpItems, len(tc.canonical))

			created := make([]string, 0, len(create.UpItems))
			for _, operation := range create.UpItems {
				ddl, ok := operation.(StaticDDLOperation)
				require.True(t, ok)
				created = append(created, ddl.Table)
				require.False(t, versionSuffix.MatchString(ddl.Table), ddl.Table)
				for _, rename := range canonicalSchemaRenames[tc.database] {
					legacyObject := regexp.MustCompile(regexp.QuoteMeta(tc.database+"."+rename.from) + `(?:\s|$|\(|,)`)
					require.False(t, legacyObject.MatchString(ddl.Query), ddl.Query)
				}
			}
			require.ElementsMatch(t, tc.canonical, created)

			drop := findPostBaselineMigration(t, tc.database, tc.dropID)
			require.Len(t, drop.UpItems, len(tc.canonical)+tc.dropPrefixOps)
			legacyDrops := drop.UpItems[tc.dropPrefixOps:]
			dropped := make([]string, 0, len(legacyDrops))
			for _, operation := range legacyDrops {
				dropOperation, ok := operation.(DropTableOperation)
				require.True(t, ok)
				dropped = append(dropped, dropOperation.Table)
			}
			require.ElementsMatch(t, canonicalLegacyTables(tc.database), dropped)
		})
	}
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
		{SigInsightLogsDB, 2005, []string{"logs_v2", "logs_v2_resource", "tag_attributes_v2"}},
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
	return findMigrationIn(t, PostBaselineAsyncMigrations, database, id)
}

func findMigrationIn(t *testing.T, migrations []PostBaselineMigration, database string, id uint64) SchemaMigrationRecord {
	t.Helper()
	for _, item := range migrations {
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
