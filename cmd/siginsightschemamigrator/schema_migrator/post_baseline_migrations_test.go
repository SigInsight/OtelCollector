package schemamigrator

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidatePostBaselineMigrations(t *testing.T) {
	syncMigration := func(database string, id uint64) PostBaselineMigration {
		return PostBaselineMigration{Database: database, Migration: SchemaMigrationRecord{
			MigrationID: id,
			UpItems:     []Operation{AlterTableAddColumn{Database: database, Table: "spans", Column: Column{Name: "new_column", Type: ColumnTypeString}}},
		}}
	}
	asyncMigration := func(database string, id uint64) PostBaselineMigration {
		return PostBaselineMigration{Database: database, Migration: SchemaMigrationRecord{
			MigrationID: id,
			UpItems:     []Operation{AlterTableDropColumn{Database: database, Table: "spans", Column: Column{Name: "old_column"}}},
		}}
	}

	tests := []struct {
		name  string
		sync  []PostBaselineMigration
		async []PostBaselineMigration
		want  string
	}{
		{name: "empty canonical registry", sync: nil, async: nil},
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

func TestCanonicalBaselineIsFinalSchema(t *testing.T) {
	expected := map[string][]string{
		SigInsightAnalyticsDB: {"rule_state_history"},
		SigInsightLogsDB:      {"field_values", "logs", "logs_attribute_keys", "logs_resource_keys", "resource_sets", "usage"},
		SigInsightMetadataDB:  nil,
		SigInsightMeterDB:     {"meter_points", "meter_rollup_1d", "meter_rollup_1d_mv"},
		SigInsightMetricsDB:   {"exp_hist", "metadata", "metric_points", "metric_rollup_5m", "metric_rollup_5m_mv", "metric_rollup_30m", "metric_rollup_30m_mv", "metric_series", "metric_series_6h", "metric_series_6h_mv", "metric_series_1d", "metric_series_1d_mv", "metric_series_1w", "metric_series_1w_mv", "usage"},
		SigInsightTracesDB:    {"exceptions", "field_values", "operations", "resource_sets", "root_operations_mv", "service_edges", "service_edges_db_calls_mv", "service_edges_messaging_calls_mv", "service_edges_service_calls_mv", "span_attributes_keys", "spans", "sub_root_operations_mv", "trace_summary", "trace_summary_from_spans_mv", "usage"},
	}
	versionedBusinessTable := regexp.MustCompile(`(?:^|_)(?:v[0-9]+|agg_[0-9]+)(?:_|$)`)

	for _, spec := range CanonicalBaselineSpecs() {
		t.Run(spec.Database, func(t *testing.T) {
			operations := spec.BaselineMigration.UpItems
			got := make([]string, 0, len(operations))
			previousRank := 0
			for _, operation := range operations {
				ddl, ok := operation.(StaticDDLOperation)
				require.True(t, ok)
				got = append(got, ddl.Table)
				require.Equal(t, spec.Database, ddl.Database)
				require.NotContains(t, ddl.Query, "{{")
				require.False(t, versionedBusinessTable.MatchString(ddl.Table), ddl.Table)
				rank := staticDDLDependencyRank(ddl.Query)
				require.GreaterOrEqual(t, rank, previousRank, ddl.Table)
				previousRank = rank
			}
			require.ElementsMatch(t, expected[spec.Database], got)
			require.Len(t, operations, len(expected[spec.Database]))
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
			query := canonicalDDLQuery(t, tc.database, tc.table)
			for _, fragment := range tc.contains {
				require.Contains(t, query, fragment)
			}
		})
	}
}

func TestCanonicalTraceColumnsAndMetricsColumns(t *testing.T) {
	spansDDL := canonicalDDLQuery(t, SigInsightTracesDB, "spans")
	require.NotContains(t, spansDDL, " ALIAS ")
	for _, column := range []string{
		"service_name", "http_route", "messaging_system", "messaging_operation",
		"db_system", "rpc_system", "rpc_service", "rpc_method", "peer_service",
	} {
		require.Contains(t, spansDDL, "`"+column+"`")
	}
	require.Contains(t, spansDDL, "INDEX idx_http_route `http_route`")
	require.Contains(t, spansDDL, "INDEX idx_rpc_method `rpc_method`")

	for _, table := range []string{"metric_points", "metric_series"} {
		query := canonicalDDLQuery(t, SigInsightMetricsDB, table)
		require.NotContains(t, query, "inserted_at_unix_milli")
	}
}

func TestCanonicalBaselineHasNoLegacyReferences(t *testing.T) {
	legacyNames := []string{
		"rule_state_history_v0", "logs_v2", "logs_v2_resource", "tag_attributes_v2",
		"samples_v4", "time_series_v4", "span_index_v3", "traces_v3_resource",
		"error_index_v2", "top_level_operations", "dependency_graph_minutes_v2",
		"samples_agg_1d",
	}
	for _, spec := range CanonicalBaselineSpecs() {
		for _, operation := range spec.BaselineMigration.UpItems {
			ddl := operation.(StaticDDLOperation)
			for _, legacyName := range legacyNames {
				require.NotContains(t, ddl.Query, legacyName)
			}
			require.NotContains(t, ddl.Query, "body_promoted")
			require.NotContains(t, ddl.Query, "body_v2")
			require.NotContains(t, ddl.Query, "attributes_metadata")
			require.NotContains(t, ddl.Query, "column_evolution_metadata")
		}
	}
}

func TestNoPostBaselineCutoverMigrationsRemain(t *testing.T) {
	require.Empty(t, PostBaselineSyncMigrations)
	require.Empty(t, PostBaselineAsyncMigrations)
	for _, database := range Databases {
		require.Empty(t, postBaselineMigrationIDs(database))
	}
}

func canonicalDDLQuery(t *testing.T, database, table string) string {
	t.Helper()
	for _, operation := range canonicalBaselineDDLOperations[database] {
		ddl := operation.(StaticDDLOperation)
		if ddl.Table == table {
			return ddl.Query
		}
	}
	t.Fatalf("canonical DDL %s.%s not found", database, table)
	return ""
}

func staticDDLDependencyRank(query string) int {
	if strings.HasPrefix(query, "CREATE MATERIALIZED VIEW") {
		return 2
	}
	return 0
}
