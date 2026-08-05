package schemamigrator

import (
	"sort"
	"strings"
)

type canonicalTableRename struct {
	from string
	to   string
}

type canonicalColumnRename struct {
	from string
	to   string
}

var canonicalTraceColumnRenames = []canonicalColumnRename{
	{from: "resource_string_service$$name_exists", to: "service_name_present"},
	{from: "attribute_string_http$$route_exists", to: "http_route_present"},
	{from: "attribute_string_messaging$$system_exists", to: "messaging_system_present"},
	{from: "attribute_string_messaging$$operation_exists", to: "messaging_operation_present"},
	{from: "attribute_string_db$$system_exists", to: "db_system_present"},
	{from: "attribute_string_rpc$$system_exists", to: "rpc_system_present"},
	{from: "attribute_string_rpc$$service_exists", to: "rpc_service_present"},
	{from: "attribute_string_rpc$$method_exists", to: "rpc_method_present"},
	{from: "attribute_string_peer$$service_exists", to: "peer_service_present"},
	{from: "resource_string_service$$name", to: "service_name"},
	{from: "attribute_string_http$$route", to: "http_route"},
	{from: "attribute_string_messaging$$system", to: "messaging_system"},
	{from: "attribute_string_messaging$$operation", to: "messaging_operation"},
	{from: "attribute_string_db$$system", to: "db_system"},
	{from: "attribute_string_rpc$$system", to: "rpc_system"},
	{from: "attribute_string_rpc$$service", to: "rpc_service"},
	{from: "attribute_string_rpc$$method", to: "rpc_method"},
	{from: "attribute_string_peer$$service", to: "peer_service"},
}

// legacyTraceAliasDefinitions only occurred in the frozen span_index_v3 DDL.
// They made old camelCase SQL work but are not part of the canonical contract.
var legacyTraceAliasDefinitions = []string{
	"`traceID` FixedString(32) ALIAS trace_id",
	"`spanID` String ALIAS span_id",
	"`parentSpanID` String ALIAS parent_span_id",
	"`spanKind` String ALIAS kind_string",
	"`durationNano` UInt64 ALIAS duration_nano",
	"`statusCode` Int16 ALIAS status_code",
	"`statusMessage` String ALIAS status_message",
	"`statusCodeString` String ALIAS status_code_string",
	"`references` String ALIAS links",
	"`responseStatusCode` LowCardinality(String) ALIAS response_status_code",
	"`externalHttpUrl` LowCardinality(String) ALIAS external_http_url",
	"`httpUrl` LowCardinality(String) ALIAS http_url",
	"`externalHttpMethod` LowCardinality(String) ALIAS external_http_method",
	"`httpMethod` LowCardinality(String) ALIAS http_method",
	"`httpHost` LowCardinality(String) ALIAS http_host",
	"`dbName` LowCardinality(String) ALIAS db_name",
	"`dbOperation` LowCardinality(String) ALIAS db_operation",
	"`hasError` Bool ALIAS has_error",
	"`isRemote` LowCardinality(String) ALIAS is_remote",
	"`serviceName` LowCardinality(String) ALIAS `resource_string_service$$name`",
	"`httpRoute` LowCardinality(String) ALIAS `attribute_string_http$$route`",
	"`msgSystem` LowCardinality(String) ALIAS `attribute_string_messaging$$system`",
	"`msgOperation` LowCardinality(String) ALIAS `attribute_string_messaging$$operation`",
	"`dbSystem` LowCardinality(String) ALIAS `attribute_string_db$$system`",
	"`rpcSystem` LowCardinality(String) ALIAS `attribute_string_rpc$$system`",
	"`rpcService` LowCardinality(String) ALIAS `attribute_string_rpc$$service`",
	"`rpcMethod` LowCardinality(String) ALIAS `attribute_string_rpc$$method`",
	"`peerService` LowCardinality(String) ALIAS `attribute_string_peer$$service`",
}

var canonicalSchemaRenames = map[string][]canonicalTableRename{
	SigInsightAnalyticsDB: {
		{from: "rule_state_history_v0", to: "rule_state_history"},
	},
	SigInsightLogsDB: {
		{from: "logs_v2", to: "logs"},
		{from: "logs_v2_resource", to: "resource_sets"},
		{from: "tag_attributes_v2", to: "field_values"},
	},
	SigInsightMetricsDB: {
		{from: "samples_v4", to: "metric_points"},
		{from: "samples_v4_agg_5m", to: "metric_rollup_5m"},
		{from: "samples_v4_agg_5m_mv", to: "metric_rollup_5m_mv"},
		{from: "samples_v4_agg_30m", to: "metric_rollup_30m"},
		{from: "samples_v4_agg_30m_mv", to: "metric_rollup_30m_mv"},
		{from: "time_series_v4", to: "metric_series"},
		{from: "time_series_v4_6hrs", to: "metric_series_6h"},
		{from: "time_series_v4_6hrs_mv", to: "metric_series_6h_mv"},
		{from: "time_series_v4_1day", to: "metric_series_1d"},
		{from: "time_series_v4_1day_mv", to: "metric_series_1d_mv"},
		{from: "time_series_v4_1week", to: "metric_series_1w"},
		{from: "time_series_v4_1week_mv", to: "metric_series_1w_mv"},
	},
	SigInsightTracesDB: {
		{from: "span_index_v3", to: "spans"},
		{from: "traces_v3_resource", to: "resource_sets"},
		{from: "tag_attributes_v2", to: "field_values"},
		{from: "error_index_v2", to: "exceptions"},
		{from: "top_level_operations", to: "operations"},
		{from: "root_operations", to: "root_operations_mv"},
		{from: "sub_root_operations", to: "sub_root_operations_mv"},
		{from: "trace_summary_mv", to: "trace_summary_from_spans_mv"},
		{from: "dependency_graph_minutes_v2", to: "service_edges"},
		{from: "dependency_graph_minutes_service_calls_mv_v2", to: "service_edges_service_calls_mv"},
		{from: "dependency_graph_minutes_db_calls_mv_v2", to: "service_edges_db_calls_mv"},
		{from: "dependency_graph_minutes_messaging_calls_mv_v2", to: "service_edges_messaging_calls_mv"},
	},
	SigInsightMeterDB: {
		{from: "samples", to: "meter_points"},
		{from: "samples_agg_1d", to: "meter_rollup_1d"},
		{from: "samples_agg_1d_mv", to: "meter_rollup_1d_mv"},
	},
}

// canonicalBaselineTables are retained from the frozen DDL without a rename.
// Every other v1 object is obsolete and is deliberately absent from a fresh
// canonical installation.
var canonicalBaselineTables = map[string]map[string]struct{}{
	SigInsightTracesDB: {
		"span_attributes_keys": {},
		"trace_summary":        {},
		"usage":                {},
	},
	SigInsightMetricsDB: {
		"exp_hist": {},
		"metadata": {},
		"usage":    {},
	},
	SigInsightLogsDB: {
		"logs_attribute_keys": {},
		"logs_resource_keys":  {},
		"usage":               {},
	},
}

// canonicalBaselineOperations creates the final, destructive M16 schema in
// the original DDL dependency order. It intentionally has no legacy creation,
// copy, rename, or cleanup phase.
func canonicalBaselineOperations(database string) []Operation {
	renamed := make(map[string]string, len(canonicalSchemaRenames[database]))
	for _, rename := range canonicalSchemaRenames[database] {
		renamed[rename.from] = rename.to
	}

	ordered := append([]canonicalTableRename(nil), canonicalSchemaRenames[database]...)
	sort.Slice(ordered, func(i, j int) bool {
		if len(ordered[i].from) != len(ordered[j].from) {
			return len(ordered[i].from) > len(ordered[j].from)
		}
		return ordered[i].from < ordered[j].from
	})
	replacements := make([]string, 0, len(ordered)*2)
	for _, rename := range ordered {
		replacements = append(replacements, database+"."+rename.from, database+"."+rename.to)
	}
	replacer := strings.NewReplacer(replacements...)

	active := canonicalBaselineTables[database]
	operations := make([]Operation, 0, len(renamed)+len(active))
	for _, rawOperation := range v1BaselineDDLOperations[database] {
		operation, ok := rawOperation.(StaticDDLOperation)
		if !ok {
			panic("frozen v1 DDL contains a non-static operation")
		}

		canonicalName, renamedObject := renamed[operation.Table]
		if !renamedObject {
			if _, retained := active[operation.Table]; !retained {
				continue
			}
			canonicalName = operation.Table
		}

		query := replacer.Replace(operation.Query)
		if database == SigInsightTracesDB {
			query = canonicalTraceDDL(query, canonicalName)
		}
		query = canonicalMetricDDL(query, database, canonicalName)
		operations = append(operations, StaticDDLOperation{
			Database: database,
			Table:    canonicalName,
			Query:    query,
		})
	}
	return operations
}

func canonicalTraceDDL(query, table string) string {
	if table == "spans" {
		for _, definition := range legacyTraceAliasDefinitions {
			needle := ", " + definition
			if !strings.Contains(query, needle) {
				panic("frozen span DDL is missing expected legacy alias: " + definition)
			}
			query = strings.Replace(query, needle, "", 1)
		}
	}

	for _, rename := range canonicalTraceColumnRenames {
		query = strings.ReplaceAll(query, rename.from, rename.to)
	}
	return query
}

func canonicalMetricDDL(query, database, table string) string {
	if database != SigInsightMetricsDB || (table != "metric_points" && table != "metric_series") {
		return query
	}
	const insertedAt = ", `inserted_at_unix_milli` Int64 CODEC(ZSTD(1))"
	if !strings.Contains(query, insertedAt) {
		panic("frozen metric DDL is missing inserted_at_unix_milli")
	}
	return strings.Replace(query, insertedAt, "", 1)
}
