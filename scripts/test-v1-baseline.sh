#!/usr/bin/env bash

set -euo pipefail

# This verifies the destructive M16 canonical baseline on the oldest
# supported ClickHouse version. A fresh installation must create only the
# final schema; it must never create, rename, or drop legacy business tables.
readonly CLICKHOUSE_IMAGE="${CLICKHOUSE_IMAGE:-clickhouse/clickhouse-server:25.5.6}"

repo_root="$(git rev-parse --show-toplevel)"
run_id="$$-${RANDOM}"
network="otel-canonical-schema-${run_id}"
clickhouse_container="${network}-clickhouse"
work_dir="$(mktemp -d)"
collector_binary="${work_dir}/siginsight-otel-collector"
collector_runtime_log="${work_dir}/collector.log"
collector_pid=""

cleanup() {
	if [[ -n "${collector_pid}" ]]; then
		kill "${collector_pid}" >/dev/null 2>&1 || true
		wait "${collector_pid}" >/dev/null 2>&1 || true
	fi
	docker rm -f -v "${clickhouse_container}" >/dev/null 2>&1 || true
	docker network rm "${network}" >/dev/null 2>&1 || true
	rm -rf "${work_dir}"
}
trap cleanup EXIT

fail() {
	echo "canonical schema integration test failed: $*" >&2
	exit 1
}

assert_equal() {
	local expected="$1"
	local actual="$2"
	local description="$3"
	[[ "${actual}" == "${expected}" ]] || fail "${description}: got ${actual@Q}, want ${expected@Q}"
}

clickhouse_query() {
	docker exec "${clickhouse_container}" clickhouse-client --format=TSVRaw --query "$1"
}

current_migrate() {
	"${collector_binary}" migrate "$@" \
		--clickhouse-dsn="tcp://127.0.0.1:${clickhouse_port}" \
		--timeout=10m
}

assert_baseline_markers() {
	local database marker history_count
	for database in siginsight_traces siginsight_metrics siginsight_logs siginsight_metadata siginsight_analytics siginsight_meter; do
		marker="$(clickhouse_query "SELECT concat(toString(migration_id), ':', status) FROM ${database}.schema_migrations_v2 WHERE migration_id = 999 SETTINGS final = 1")"
		assert_equal "999:finished" "${marker}" "${database} canonical baseline marker"
		history_count="$(clickhouse_query "SELECT count() FROM ${database}.schema_migrations_v2 SETTINGS final = 1")"
		assert_equal "1" "${history_count}" "${database} direct migration history"
	done
}

assert_canonical_inventory() {
	local expected actual
	expected=$'siginsight_analytics.rule_state_history\nsiginsight_logs.field_values\nsiginsight_logs.logs\nsiginsight_logs.logs_attribute_keys\nsiginsight_logs.logs_resource_keys\nsiginsight_logs.resource_sets\nsiginsight_logs.usage\nsiginsight_meter.meter_points\nsiginsight_meter.meter_rollup_1d\nsiginsight_meter.meter_rollup_1d_mv\nsiginsight_metrics.exp_hist\nsiginsight_metrics.metadata\nsiginsight_metrics.metric_points\nsiginsight_metrics.metric_rollup_30m\nsiginsight_metrics.metric_rollup_30m_mv\nsiginsight_metrics.metric_rollup_5m\nsiginsight_metrics.metric_rollup_5m_mv\nsiginsight_metrics.metric_series\nsiginsight_metrics.metric_series_1d\nsiginsight_metrics.metric_series_1d_mv\nsiginsight_metrics.metric_series_1w\nsiginsight_metrics.metric_series_1w_mv\nsiginsight_metrics.metric_series_6h\nsiginsight_metrics.metric_series_6h_mv\nsiginsight_metrics.usage\nsiginsight_traces.exceptions\nsiginsight_traces.field_values\nsiginsight_traces.operations\nsiginsight_traces.resource_sets\nsiginsight_traces.root_operations_mv\nsiginsight_traces.service_edges\nsiginsight_traces.service_edges_db_calls_mv\nsiginsight_traces.service_edges_messaging_calls_mv\nsiginsight_traces.service_edges_service_calls_mv\nsiginsight_traces.span_attributes_keys\nsiginsight_traces.spans\nsiginsight_traces.sub_root_operations_mv\nsiginsight_traces.trace_summary\nsiginsight_traces.trace_summary_from_spans_mv\nsiginsight_traces.usage'
	actual="$(clickhouse_query "SELECT concat(database, '.', name) FROM system.tables WHERE database IN ('siginsight_logs', 'siginsight_traces', 'siginsight_metrics', 'siginsight_metadata', 'siginsight_analytics', 'siginsight_meter') AND name NOT IN ('schema_migrations', 'schema_migrations_v2') ORDER BY database, name")"
	assert_equal "${expected}" "${actual}" "canonical business table inventory"
}

assert_schema_fingerprint() {
	local database expected actual
	for database in siginsight_analytics siginsight_logs siginsight_metadata siginsight_meter siginsight_metrics siginsight_traces; do
		case "${database}" in
			siginsight_analytics) expected="1:12242196818159068904:11:9151060719887308879" ;;
			siginsight_logs) expected="6:4907456606029717637:43:6450537337900337272" ;;
			siginsight_metadata) expected="0:11160318154034397263:0:11160318154034397263" ;;
			siginsight_meter) expected="3:4794694533261956589:38:16828873058290010379" ;;
			siginsight_metrics) expected="15:13305681492122153582:174:8325196865590917439" ;;
			siginsight_traces) expected="15:9151180053864431952:131:14213257424511927958" ;;
		esac
		actual="$(clickhouse_query "SELECT concat(
			toString((SELECT count() FROM system.tables WHERE database = '${database}' AND name NOT IN ('schema_migrations', 'schema_migrations_v2'))), ':',
			toString((SELECT cityHash64(arrayStringConcat(arraySort(groupArray(concat(name, '|', engine, '|', partition_key, '|', sorting_key, '|', primary_key, '|', sampling_key, '|', create_table_query))), '\\n')) FROM system.tables WHERE database = '${database}' AND name NOT IN ('schema_migrations', 'schema_migrations_v2'))), ':',
			toString((SELECT count() FROM system.columns WHERE database = '${database}' AND table NOT IN ('schema_migrations', 'schema_migrations_v2'))), ':',
			toString((SELECT cityHash64(arrayStringConcat(arraySort(groupArray(concat(table, '|', toString(position), '|', name, '|', type, '|', default_kind, '|', default_expression, '|', compression_codec))), '\\n')) FROM system.columns WHERE database = '${database}' AND table NOT IN ('schema_migrations', 'schema_migrations_v2')))
		)")"
		assert_equal "${expected}" "${actual}" "${database} schema fingerprint"
	done
}

assert_no_legacy_ddl() {
	local legacy_ddl
	clickhouse_query "SYSTEM FLUSH LOGS"
	legacy_ddl="$(clickhouse_query "SELECT count() FROM system.query_log WHERE query ILIKE '%siginsight_logs.logs_v2%' OR query ILIKE '%siginsight_traces.span_index_v3%' OR query ILIKE '%siginsight_metrics.samples_v4%' OR query ILIKE '%siginsight_meter.samples_agg_1d%' OR query ILIKE '%siginsight_analytics.rule_state_history_v0%'")"
	assert_equal "0" "${legacy_ddl}" "fresh migration legacy DDL"
}

assert_canonical_trace_columns() {
	local alias_columns fast_path_columns obsolete_columns
	alias_columns="$(clickhouse_query "SELECT count() FROM system.columns WHERE database = 'siginsight_traces' AND table = 'spans' AND default_kind = 'ALIAS'")"
	assert_equal "0" "${alias_columns}" "legacy trace alias columns"
	fast_path_columns="$(clickhouse_query "SELECT count() FROM system.columns WHERE database = 'siginsight_traces' AND table = 'spans' AND name IN ('service_name', 'http_route', 'messaging_system', 'messaging_operation', 'db_system', 'rpc_system', 'rpc_service', 'rpc_method', 'peer_service', 'service_name_present', 'http_route_present', 'messaging_system_present', 'messaging_operation_present', 'db_system_present', 'rpc_system_present', 'rpc_service_present', 'rpc_method_present', 'peer_service_present')")"
	assert_equal "18" "${fast_path_columns}" "canonical trace fast-path columns"
	obsolete_columns="$(clickhouse_query "SELECT count() FROM system.columns WHERE database = 'siginsight_metrics' AND table IN ('metric_points', 'metric_series') AND name = 'inserted_at_unix_milli'")"
	assert_equal "0" "${obsolete_columns}" "unused metric ingestion timestamp columns"
}

assert_materialized_view_contract() {
	local invalid_references
	invalid_references="$(clickhouse_query "SELECT count() FROM system.tables WHERE database IN ('siginsight_traces', 'siginsight_metrics', 'siginsight_meter') AND engine = 'MaterializedView' AND (create_table_query LIKE '%span_index_v3%' OR create_table_query LIKE '%samples_v4%' OR create_table_query LIKE '%time_series_v4%' OR create_table_query LIKE '%samples_agg_1d%')")"
	assert_equal "0" "${invalid_references}" "canonical materialized view sources and targets"
	clickhouse_query "SELECT name, serviceName FROM siginsight_traces.operations LIMIT 0" >/dev/null
	clickhouse_query "SELECT src, dest, timestamp FROM siginsight_traces.service_edges LIMIT 0" >/dev/null
	clickhouse_query "SELECT metric_name, fingerprint, unix_milli FROM siginsight_metrics.metric_rollup_5m LIMIT 0" >/dev/null
	clickhouse_query "SELECT metric_name, fingerprint, unix_milli FROM siginsight_metrics.metric_series_1w LIMIT 0" >/dev/null
	clickhouse_query "SELECT metric_name, fingerprint, unix_milli FROM siginsight_meter.meter_rollup_1d LIMIT 0" >/dev/null
}

assert_collector_writes_canonical_schema() {
	local now_ns end_ns smoke_counts metadata_counts derived_counts body_json_value
	"${collector_binary}" --config <(sed \
		-e "s#tcp://clickhouse:9000#tcp://127.0.0.1:${clickhouse_port}#g" \
		-e 's#0.0.0.0:4317#127.0.0.1:14317#g' \
		-e 's#0.0.0.0:4318#127.0.0.1:14318#g' \
		-e 's#0.0.0.0:13133#127.0.0.1:14333#g' \
		-e 's#0.0.0.0:1777#127.0.0.1:14777#g' \
		-e 's#metrics_flush_interval: 1h#metrics_flush_interval: 1s#g' \
		-e 's#metrics_flush_interval: 60s#metrics_flush_interval: 1s#g' \
		"${repo_root}/config/collector.yaml") >"${collector_runtime_log}" 2>&1 &
	collector_pid=$!

	for _ in $(seq 1 60); do
		if curl --fail --silent http://127.0.0.1:14333/ >/dev/null 2>&1; then
			break
		fi
		if ! kill -0 "${collector_pid}" >/dev/null 2>&1; then
			tail -100 "${collector_runtime_log}" >&2
			fail "collector stopped before accepting OTLP data"
		fi
		sleep 1
	done

	now_ns="$(date +%s%N)"
	end_ns="$((now_ns + 1000000))"
	curl --fail --silent -H 'Content-Type: application/json' http://127.0.0.1:14318/v1/logs --data-binary @- >/dev/null <<JSON
{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"schema-smoke"}}]},"scopeLogs":[{"scope":{"name":"schema-smoke"},"logRecords":[{"timeUnixNano":"${now_ns}","observedTimeUnixNano":"${now_ns}","severityNumber":9,"severityText":"INFO","body":{"stringValue":"{\"service\":{\"name\":\"api\"}}"},"attributes":[{"key":"smoke.kind","value":{"stringValue":"migration"}}]}]}]}]}
JSON
	curl --fail --silent -H 'Content-Type: application/json' http://127.0.0.1:14318/v1/traces --data-binary @- >/dev/null <<JSON
{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"schema-smoke"}}]},"scopeSpans":[{"scope":{"name":"schema-smoke"},"spans":[{"traceId":"0123456789abcdef0123456789abcdef","spanId":"0123456789abcdef","name":"schema-smoke-span","kind":2,"startTimeUnixNano":"${now_ns}","endTimeUnixNano":"${end_ns}","attributes":[{"key":"smoke.kind","value":{"stringValue":"migration"}}],"status":{"code":1}},{"traceId":"0123456789abcdef0123456789abcdef","spanId":"fedcba9876543210","parentSpanId":"0123456789abcdef","name":"schema-smoke-db","kind":3,"startTimeUnixNano":"${now_ns}","endTimeUnixNano":"${end_ns}","attributes":[{"key":"db.system","value":{"stringValue":"postgresql"}},{"key":"smoke.kind","value":{"stringValue":"migration"}}],"status":{"code":1}}]}]}]}
JSON
	curl --fail --silent -H 'Content-Type: application/json' http://127.0.0.1:14318/v1/metrics --data-binary @- >/dev/null <<JSON
{"resourceMetrics":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"schema-smoke"}}]},"scopeMetrics":[{"scope":{"name":"schema-smoke"},"metrics":[{"name":"schema.smoke.gauge","unit":"1","gauge":{"dataPoints":[{"timeUnixNano":"${now_ns}","asDouble":42.5,"attributes":[{"key":"smoke.kind","value":{"stringValue":"migration"}}]}]}}]}]}]}
JSON

	smoke_counts=""
	for _ in $(seq 1 40); do
		smoke_counts="$(clickhouse_query "SELECT concat(toString((SELECT count() > 0 FROM siginsight_logs.logs WHERE resources_string['service.name'] = 'schema-smoke')), ':', toString((SELECT count() > 0 FROM siginsight_traces.spans WHERE resources_string['service.name'] = 'schema-smoke')), ':', toString((SELECT count() > 0 FROM siginsight_metrics.metric_points WHERE metric_name = 'schema.smoke.gauge')), ':', toString((SELECT count() > 0 FROM siginsight_metrics.metric_series WHERE metric_name = 'schema.smoke.gauge')))")"
		[[ "${smoke_counts}" == "1:1:1:1" ]] && break
		sleep 1
	done
	assert_equal "1:1:1:1" "${smoke_counts}" "Collector writes to canonical primary tables"

	metadata_counts=""
	for _ in $(seq 1 40); do
		metadata_counts="$(clickhouse_query "SELECT concat(toString((SELECT count() > 0 FROM siginsight_logs.logs_attribute_keys WHERE name = 'smoke.kind')), ':', toString((SELECT count() > 0 FROM siginsight_logs.field_values WHERE tag_key = 'smoke.kind' AND string_value = 'migration')), ':', toString((SELECT count() > 0 FROM siginsight_traces.span_attributes_keys WHERE tagKey = 'smoke.kind')), ':', toString((SELECT count() > 0 FROM siginsight_traces.field_values WHERE tag_key = 'smoke.kind' AND string_value = 'migration')), ':', toString((SELECT count() > 0 FROM siginsight_metrics.metadata WHERE metric_name = 'schema.smoke.gauge' AND attr_name = 'smoke.kind')))")"
		[[ "${metadata_counts}" == "1:1:1:1:1" ]] && break
		sleep 1
	done
	assert_equal "1:1:1:1:1" "${metadata_counts}" "signal metadata tables"

	derived_counts=""
	for _ in $(seq 1 40); do
		derived_counts="$(clickhouse_query "SELECT concat(toString((SELECT count() > 0 FROM siginsight_traces.operations WHERE name = 'schema-smoke-span' AND serviceName = 'schema-smoke')), ':', toString((SELECT count() > 0 FROM siginsight_traces.trace_summary WHERE trace_id = '0123456789abcdef0123456789abcdef')), ':', toString((SELECT count() > 0 FROM siginsight_traces.service_edges WHERE src = 'schema-smoke' AND dest = 'postgresql')), ':', toString((SELECT count() > 0 FROM siginsight_metrics.metric_rollup_5m WHERE metric_name = 'schema.smoke.gauge')), ':', toString((SELECT count() > 0 FROM siginsight_metrics.metric_rollup_30m WHERE metric_name = 'schema.smoke.gauge')), ':', toString((SELECT count() > 0 FROM siginsight_metrics.metric_series_6h WHERE metric_name = 'schema.smoke.gauge')), ':', toString((SELECT count() > 0 FROM siginsight_metrics.metric_series_1d WHERE metric_name = 'schema.smoke.gauge')), ':', toString((SELECT count() > 0 FROM siginsight_metrics.metric_series_1w WHERE metric_name = 'schema.smoke.gauge')), ':', toString((SELECT count() > 0 FROM siginsight_meter.meter_points WHERE metric_name = 'signoz.meter.log.count')), ':', toString((SELECT count() > 0 FROM siginsight_meter.meter_rollup_1d WHERE metric_name = 'signoz.meter.log.count')))")"
		[[ "${derived_counts}" == "1:1:1:1:1:1:1:1:1:1" ]] && break
		sleep 1
	done
	assert_equal "1:1:1:1:1:1:1:1:1:1" "${derived_counts}" "canonical materialized-view chains"

	body_json_value="$(clickhouse_query "SELECT JSON_VALUE(body, '$.service.name') FROM siginsight_logs.logs WHERE resources_string['service.name'] = 'schema-smoke' LIMIT 1")"
	assert_equal "api" "${body_json_value}" "Collector-written body String JSON path query"

	kill "${collector_pid}" >/dev/null 2>&1 || true
	wait "${collector_pid}" >/dev/null 2>&1 || true
	collector_pid=""
}

assert_no_coordination_features() {
	local invalid_tables on_cluster_count
	invalid_tables="$(clickhouse_query "SELECT count() FROM system.tables WHERE database LIKE 'siginsight_%' AND (engine LIKE 'Replicated%' OR engine = 'Distributed')")"
	assert_equal "0" "${invalid_tables}" "replicated or Distributed telemetry tables"
	on_cluster_count="$(clickhouse_query "SELECT count() FROM system.tables WHERE database LIKE 'siginsight_%' AND create_table_query LIKE '%ON CLUSTER%'")"
	assert_equal "0" "${on_cluster_count}" "ON CLUSTER telemetry DDL"
}

echo "Building current collector"
go build -tags=remove_all_sd -o "${collector_binary}" "${repo_root}/cmd/siginsightotelcollector"

docker network create "${network}" >/dev/null
docker run -d --name "${clickhouse_container}" --network "${network}" -p 127.0.0.1::9000 -e CLICKHOUSE_SKIP_USER_SETUP=1 "${CLICKHOUSE_IMAGE}" >/dev/null

clickhouse_port="$(docker port "${clickhouse_container}" 9000/tcp | awk -F: 'NR == 1 { print $NF }')"
[[ -n "${clickhouse_port}" ]] || fail "could not resolve published ClickHouse port"
for _ in $(seq 1 60); do
	if clickhouse_query "SELECT 1" >/dev/null 2>&1; then
		break
	fi
	sleep 1
done
version="$(clickhouse_query "SELECT version()")"
[[ "${version}" == 25.5.6.* ]] || fail "ClickHouse version is ${version}, want 25.5.6.x"

echo "Creating and checking the direct canonical baseline"
current_migrate ready
current_migrate bootstrap
current_migrate sync up
current_migrate async up
current_migrate sync check
current_migrate async check
assert_baseline_markers
assert_canonical_inventory
assert_schema_fingerprint
assert_no_legacy_ddl
assert_canonical_trace_columns
assert_materialized_view_contract
assert_no_coordination_features
assert_collector_writes_canonical_schema

echo "canonical schema integration test passed"
