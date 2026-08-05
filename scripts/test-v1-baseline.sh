#!/usr/bin/env bash

set -euo pipefail

# This verifies the frozen v1 baseline on the oldest supported ClickHouse
# version. The server intentionally has no Keeper/ZooKeeper or cluster config.
readonly CLICKHOUSE_IMAGE="${CLICKHOUSE_IMAGE:-clickhouse/clickhouse-server:25.5.6}"

repo_root="$(git rev-parse --show-toplevel)"
run_id="$$-${RANDOM}"
network="otel-v1-baseline-${run_id}"
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
	echo "v1 baseline integration test failed: $*" >&2
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
	local database marker history_count expected_history_count
	for database in siginsight_traces siginsight_metrics siginsight_logs siginsight_metadata siginsight_analytics siginsight_meter; do
		marker="$(clickhouse_query "SELECT concat(toString(migration_id), ':', status) FROM ${database}.schema_migrations_v2 WHERE migration_id = 999 SETTINGS final = 1")"
		assert_equal "999:finished" "${marker}" "${database} baseline marker"
		history_count="$(clickhouse_query "SELECT count() FROM ${database}.schema_migrations_v2 SETTINGS final = 1")"
		expected_history_count=4
		if [[ "${database}" == "siginsight_metadata" ]]; then
			expected_history_count=3
		fi
		if [[ "${database}" == "siginsight_traces" ]]; then
			expected_history_count=5
		fi
		if [[ "${database}" == "siginsight_metrics" ]]; then
			expected_history_count=6
		fi
		if [[ "${database}" == "siginsight_logs" ]]; then
			expected_history_count=6
		fi
		assert_equal "${expected_history_count}" "${history_count}" "${database} migration history"
	done
}

seed_legacy_logs_json_schema() {
	clickhouse_query "ALTER TABLE siginsight_logs.logs_v2 ADD COLUMN IF NOT EXISTS body_v2 JSON(max_dynamic_paths=0) AFTER body"
	clickhouse_query "ALTER TABLE siginsight_logs.logs_v2 ADD COLUMN IF NOT EXISTS body_promoted JSON() AFTER body_v2"
	clickhouse_query "ALTER TABLE siginsight_logs.logs_v2 ADD INDEX IF NOT EXISTS body_v2_string_ngram_idx lower(toString(body_v2)) TYPE ngrambf_v1(4, 15000, 3, 0) GRANULARITY 1"
	clickhouse_query "ALTER TABLE siginsight_logs.logs_v2 ADD INDEX IF NOT EXISTS body_v2_string_token_idx lower(toString(body_v2)) TYPE tokenbf_v1(10000, 2, 0) GRANULARITY 1"
	clickhouse_query "ALTER TABLE siginsight_logs.logs_v2 ADD INDEX IF NOT EXISTS body_v2_paths_ngram_idx JSONAllPaths(body_v2) TYPE ngrambf_v1(4, 15000, 3, 0) GRANULARITY 1"
	clickhouse_query "ALTER TABLE siginsight_logs.logs_v2 ADD INDEX IF NOT EXISTS body_v2_paths_token_idx JSONAllPaths(body_v2) TYPE tokenbf_v1(10000, 2, 0) GRANULARITY 1"
	clickhouse_query "CREATE TABLE IF NOT EXISTS siginsight_metadata.json_path_types (path String, type String, last_seen UInt64) ENGINE = ReplacingMergeTree ORDER BY (path, type)"
	clickhouse_query "INSERT INTO siginsight_logs.logs_v2 (ts_bucket_start, timestamp, observed_timestamp, id, body, body_v2, body_promoted) VALUES (toUInt64(toUnixTimestamp(now())), toUInt64(toUnixTimestamp64Nano(now64(9))), toUInt64(toUnixTimestamp64Nano(now64(9))), 'legacy-json-cleanup-test', '{\"service\":{\"name\":\"api\"}}', '{\"service\":{\"name\":\"api\"}}', '{\"service\":{\"name\":\"api\"}}')"
	clickhouse_query "INSERT INTO siginsight_logs.schema_migrations_v2 (migration_id, status, created_at) VALUES (2001, 'finished', now())"
}

assert_legacy_logs_json_removed() {
	local columns indexes metadata_tables cleanup_marker
	columns="$(clickhouse_query "SELECT count() FROM system.columns WHERE database = 'siginsight_logs' AND table = 'logs_v2' AND name IN ('body_v2', 'body_promoted')")"
	assert_equal "0" "${columns}" "legacy logs JSON columns"
	indexes="$(clickhouse_query "SELECT count() FROM system.data_skipping_indices WHERE database = 'siginsight_logs' AND table = 'logs_v2' AND name LIKE 'body_v2_%'")"
	assert_equal "0" "${indexes}" "legacy logs JSON indexes"
	metadata_tables="$(clickhouse_query "SELECT count() FROM system.tables WHERE database = 'siginsight_metadata' AND name IN ('json_path_types', 'distributed_json_path_types')")"
	assert_equal "0" "${metadata_tables}" "legacy logs JSON metadata tables"
	cleanup_marker="$(clickhouse_query "SELECT concat(toString(migration_id), ':', status) FROM siginsight_logs.schema_migrations_v2 WHERE migration_id = 2002 SETTINGS final = 1")"
	assert_equal "2002:finished" "${cleanup_marker}" "legacy logs JSON cleanup marker"
}

assert_metadata_seed() {
	local seed_count
	seed_count="$(clickhouse_query "SELECT count() FROM siginsight_metadata.column_evolution_metadata WHERE (signal, column_name, column_type, version) IN (('logs', 'resources_string', 'Map(LowCardinality(String), Float64)', 0), ('logs', 'resource', 'JSON()', 1), ('traces', 'resources_string', 'Map(LowCardinality(String), Float64)', 0), ('traces', 'resource', 'JSON()', 1))")"
	assert_equal "4" "${seed_count}" "metadata seed records"
}

simulate_pre_siginsight_storage_names() {
	local database legacy_database view_database view_name view_ddl legacy_view_ddl

	# First move the physical objects back to the historical identity. ClickHouse
	# deliberately does not rewrite view definitions during either rename, so the
	# views are then rebuilt with the same old definitions an upgraded deployment
	# actually has.
	for database in analytics logs metadata meter metrics traces; do
		clickhouse_query "RENAME DATABASE siginsight_${database} TO signoz_${database}"
	done

	for rename in 'span_index_v3 signoz_index_v3' 'error_index_v2 signoz_error_index_v2'; do
		read -r current legacy <<<"${rename}"
		if [[ "$(clickhouse_query "EXISTS TABLE signoz_traces.${current}")" == "1" ]]; then
			clickhouse_query "RENAME TABLE signoz_traces.${current} TO signoz_traces.${legacy}"
		fi
	done

	while IFS=$'\t' read -r view_database view_name; do
		[[ -n "${view_database}" && -n "${view_name}" ]] || continue
		view_ddl="$(clickhouse_query "SELECT create_table_query FROM system.tables WHERE database = '${view_database}' AND name = '${view_name}'")"
		legacy_view_ddl="${view_ddl//siginsight_logs/signoz_logs}"
		legacy_view_ddl="${legacy_view_ddl//siginsight_metrics/signoz_metrics}"
		legacy_view_ddl="${legacy_view_ddl//siginsight_traces/signoz_traces}"
		legacy_view_ddl="${legacy_view_ddl//siginsight_metadata/signoz_metadata}"
		legacy_view_ddl="${legacy_view_ddl//siginsight_analytics/signoz_analytics}"
		legacy_view_ddl="${legacy_view_ddl//siginsight_meter/signoz_meter}"
		legacy_view_ddl="${legacy_view_ddl//span_index_v3/signoz_index_v3}"
		legacy_view_ddl="${legacy_view_ddl//error_index_v2/signoz_error_index_v2}"
		clickhouse_query "DROP TABLE ${view_database}.${view_name}"
		clickhouse_query "${legacy_view_ddl}"
	done < <(clickhouse_query "SELECT concat(database, '\t', name) FROM system.tables WHERE database IN ('signoz_logs', 'signoz_metrics', 'signoz_traces', 'signoz_metadata', 'signoz_analytics', 'signoz_meter') AND engine IN ('MaterializedView', 'View') ORDER BY database, name")
}

assert_legacy_storage_names_upgraded() {
	local legacy_databases legacy_tables legacy_view_references
	legacy_databases="$(clickhouse_query "SELECT count() FROM system.databases WHERE name IN ('signoz_logs', 'signoz_metrics', 'signoz_traces', 'signoz_metadata', 'signoz_analytics', 'signoz_meter')")"
	assert_equal "0" "${legacy_databases}" "legacy telemetry databases"
	legacy_tables="$(clickhouse_query "SELECT count() FROM system.tables WHERE database = 'siginsight_traces' AND name IN ('signoz_index_v3', 'signoz_index_v2', 'signoz_error_index_v2', 'signoz_spans')")"
	assert_equal "0" "${legacy_tables}" "legacy trace table names"
	legacy_view_references="$(clickhouse_query "SELECT count() FROM system.tables WHERE database IN ('siginsight_logs', 'siginsight_metrics', 'siginsight_traces', 'siginsight_metadata', 'siginsight_analytics', 'siginsight_meter') AND engine IN ('MaterializedView', 'View') AND create_table_query LIKE '%signoz_%'")"
	assert_equal "0" "${legacy_view_references}" "legacy view references"
}

assert_obsolete_tables_removed() {
	local obsolete_tables
	obsolete_tables="$(clickhouse_query "SELECT count() FROM system.tables WHERE concat(database, '.', name) IN (
		'siginsight_analytics.distributed_rule_state_history_v0',
		'siginsight_logs.distributed_logs_attribute_keys',
		'siginsight_logs.distributed_logs_resource_keys',
		'siginsight_logs.distributed_logs_v2',
		'siginsight_logs.distributed_logs_v2_resource',
		'siginsight_logs.distributed_tag_attributes_v2',
		'siginsight_logs.distributed_usage',
		'siginsight_logs.logs_v2',
		'siginsight_logs.logs_v2_resource',
		'siginsight_logs.tag_attributes_v2',
		'siginsight_metadata.distributed_attributes_metadata',
		'siginsight_metadata.distributed_column_evolution_metadata',
		'siginsight_metadata.attributes_metadata',
		'siginsight_metadata.column_evolution_metadata',
		'siginsight_meter.distributed_samples',
		'siginsight_meter.distributed_samples_agg_1d',
		'siginsight_meter.samples',
		'siginsight_meter.samples_agg_1d',
		'siginsight_meter.samples_agg_1d_mv',
		'siginsight_metrics.distributed_exp_hist',
		'siginsight_metrics.distributed_metadata',
		'siginsight_metrics.distributed_samples_v2',
		'siginsight_metrics.distributed_samples_v4',
		'siginsight_metrics.distributed_samples_v4_agg_30m',
		'siginsight_metrics.distributed_samples_v4_agg_5m',
		'siginsight_metrics.distributed_time_series_v2',
		'siginsight_metrics.distributed_time_series_v4',
		'siginsight_metrics.distributed_time_series_v4_1day',
		'siginsight_metrics.distributed_time_series_v4_1week',
		'siginsight_metrics.distributed_time_series_v4_6hrs',
		'siginsight_metrics.distributed_updated_metadata',
		'siginsight_metrics.distributed_usage',
		'siginsight_metrics.samples_v2',
		'siginsight_metrics.samples_v4',
		'siginsight_metrics.samples_v4_agg_5m',
		'siginsight_metrics.samples_v4_agg_5m_mv',
		'siginsight_metrics.samples_v4_agg_30m',
		'siginsight_metrics.samples_v4_agg_30m_mv',
		'siginsight_metrics.time_series_v4',
		'siginsight_metrics.time_series_v4_6hrs',
		'siginsight_metrics.time_series_v4_6hrs_mv',
		'siginsight_metrics.time_series_v4_1day',
		'siginsight_metrics.time_series_v4_1day_mv',
		'siginsight_metrics.time_series_v4_1week',
		'siginsight_metrics.time_series_v4_1week_mv',
		'siginsight_metrics.updated_metadata',
		'siginsight_metrics.time_series_v2',
		'siginsight_traces.distributed_dependency_graph_minutes_v2',
		'siginsight_traces.distributed_signoz_error_index_v2',
		'siginsight_traces.distributed_signoz_index_v2',
		'siginsight_traces.distributed_signoz_index_v3',
		'siginsight_traces.distributed_signoz_spans',
		'siginsight_traces.distributed_span_attributes',
		'siginsight_traces.distributed_span_attributes_keys',
		'siginsight_traces.distributed_tag_attributes_v2',
		'siginsight_traces.distributed_top_level_operations',
		'siginsight_traces.distributed_trace_summary',
		'siginsight_traces.distributed_traces_v3_resource',
		'siginsight_traces.distributed_usage',
		'siginsight_traces.distributed_usage_explorer',
		'siginsight_traces.durationSort',
		'siginsight_traces.dependency_graph_minutes_v2',
		'siginsight_traces.dependency_graph_minutes_service_calls_mv_v2',
		'siginsight_traces.dependency_graph_minutes_db_calls_mv_v2',
		'siginsight_traces.dependency_graph_minutes_messaging_calls_mv_v2',
		'siginsight_traces.error_index_v2',
		'siginsight_traces.legacy_spans',
		'siginsight_traces.root_operations',
		'siginsight_traces.span_attributes',
		'siginsight_traces.span_index_v2',
		'siginsight_traces.span_index_v3',
		'siginsight_traces.sub_root_operations',
		'siginsight_traces.tag_attributes_v2',
		'siginsight_traces.top_level_operations',
		'siginsight_traces.trace_summary_mv',
		'siginsight_traces.traces_v3_resource',
		'siginsight_traces.usage_explorer',
		'siginsight_traces.usage_explorer_mv',
		'siginsight_analytics.rule_state_history_v0'
	)")"
	assert_equal "0" "${obsolete_tables}" "obsolete local and distributed tables"
}

assert_obsolete_columns_removed() {
	local obsolete_columns
	obsolete_columns="$(clickhouse_query "SELECT count() FROM system.columns WHERE database = 'siginsight_metrics' AND table IN ('metric_points', 'metric_series') AND name = 'inserted_at_unix_milli'")"
	assert_equal "0" "${obsolete_columns}" "unused metric ingestion timestamp columns"
}

assert_canonical_trace_columns() {
	local alias_columns fast_path_columns
	alias_columns="$(clickhouse_query "SELECT count() FROM system.columns WHERE database = 'siginsight_traces' AND table = 'spans' AND default_kind = 'ALIAS'")"
	assert_equal "0" "${alias_columns}" "legacy trace alias columns"
	fast_path_columns="$(clickhouse_query "SELECT count() FROM system.columns WHERE database = 'siginsight_traces' AND table = 'spans' AND name IN ('service_name', 'http_route', 'messaging_system', 'messaging_operation', 'db_system', 'rpc_system', 'rpc_service', 'rpc_method', 'peer_service', 'service_name_present', 'http_route_present', 'messaging_system_present', 'messaging_operation_present', 'db_system_present', 'rpc_system_present', 'rpc_service_present', 'rpc_method_present', 'peer_service_present')")"
	assert_equal "18" "${fast_path_columns}" "canonical trace fast-path columns"
}

assert_core_tables_readable() {
	local core_table_count
	core_table_count="$(clickhouse_query "SELECT count() FROM system.tables WHERE concat(database, '.', name) IN (
		'siginsight_analytics.rule_state_history',
		'siginsight_logs.logs',
		'siginsight_logs.field_values',
		'siginsight_meter.meter_points',
		'siginsight_meter.meter_rollup_1d',
		'siginsight_metrics.exp_hist',
		'siginsight_metrics.metadata',
		'siginsight_metrics.metric_points',
		'siginsight_metrics.metric_series',
		'siginsight_traces.exceptions',
		'siginsight_traces.spans',
		'siginsight_traces.span_attributes_keys',
		'siginsight_traces.field_values'
	)")"
	assert_equal "13" "${core_table_count}" "Collector and SigInsight core local tables"

	clickhouse_query "SELECT timestamp, body, resource FROM siginsight_logs.logs LIMIT 0" >/dev/null
	clickhouse_query "SELECT timestamp, trace_id, span_id, resource FROM siginsight_traces.spans LIMIT 0" >/dev/null
	clickhouse_query "SELECT timestamp, errorID, traceID, resourceTagsMap FROM siginsight_traces.exceptions LIMIT 0" >/dev/null
	clickhouse_query "SELECT tagKey, tagType, dataType FROM siginsight_traces.span_attributes_keys LIMIT 0" >/dev/null
	clickhouse_query "SELECT metric_name, fingerprint, unix_milli, value FROM siginsight_metrics.metric_points LIMIT 0" >/dev/null
	clickhouse_query "SELECT metric_name, fingerprint, labels, attrs, resource_attrs FROM siginsight_metrics.metric_series LIMIT 0" >/dev/null
}

assert_collector_writes_reduced_schema() {
	local now_ns end_ns smoke_counts body_json_value
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

	curl --fail --silent -H 'Content-Type: application/json' \
		http://127.0.0.1:14318/v1/logs --data-binary @- >/dev/null <<JSON
{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"schema-smoke"}}]},"scopeLogs":[{"scope":{"name":"schema-smoke"},"logRecords":[{"timeUnixNano":"${now_ns}","observedTimeUnixNano":"${now_ns}","severityNumber":9,"severityText":"INFO","body":{"stringValue":"{\"service\":{\"name\":\"api\"}}"},"attributes":[{"key":"smoke.kind","value":{"stringValue":"migration"}}]}]}]}]}
JSON

	curl --fail --silent -H 'Content-Type: application/json' \
		http://127.0.0.1:14318/v1/traces --data-binary @- >/dev/null <<JSON
{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"schema-smoke"}}]},"scopeSpans":[{"scope":{"name":"schema-smoke"},"spans":[{"traceId":"0123456789abcdef0123456789abcdef","spanId":"0123456789abcdef","name":"schema-smoke-span","kind":2,"startTimeUnixNano":"${now_ns}","endTimeUnixNano":"${end_ns}","attributes":[{"key":"smoke.kind","value":{"stringValue":"migration"}}],"status":{"code":1}},{"traceId":"0123456789abcdef0123456789abcdef","spanId":"fedcba9876543210","parentSpanId":"0123456789abcdef","name":"schema-smoke-db","kind":3,"startTimeUnixNano":"${now_ns}","endTimeUnixNano":"${end_ns}","attributes":[{"key":"db.system","value":{"stringValue":"postgresql"}},{"key":"smoke.kind","value":{"stringValue":"migration"}}],"status":{"code":1}}]}]}]}
JSON

	curl --fail --silent -H 'Content-Type: application/json' \
		http://127.0.0.1:14318/v1/metrics --data-binary @- >/dev/null <<JSON
{"resourceMetrics":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"schema-smoke"}}]},"scopeMetrics":[{"scope":{"name":"schema-smoke"},"metrics":[{"name":"schema.smoke.gauge","unit":"1","gauge":{"dataPoints":[{"timeUnixNano":"${now_ns}","asDouble":42.5,"attributes":[{"key":"smoke.kind","value":{"stringValue":"migration"}}]}]}}]}]}]}
JSON

	smoke_counts=""
	for _ in $(seq 1 40); do
		smoke_counts="$(clickhouse_query "SELECT concat(
			toString((SELECT count() > 0 FROM siginsight_logs.logs WHERE resources_string['service.name'] = 'schema-smoke')),
			':',
			toString((SELECT count() > 0 FROM siginsight_traces.spans WHERE resources_string['service.name'] = 'schema-smoke')),
			':',
			toString((SELECT count() > 0 FROM siginsight_metrics.metric_points WHERE metric_name = 'schema.smoke.gauge')),
			':',
			toString((SELECT count() > 0 FROM siginsight_metrics.metric_series WHERE metric_name = 'schema.smoke.gauge'))
		)")"
		if [[ "${smoke_counts}" == "1:1:1:1" ]]; then
			break
		fi
		sleep 1
	done
	assert_equal "1:1:1:1" "${smoke_counts}" "Collector writes to canonical logs, traces, metric points, and metric series"

	local metadata_counts
	metadata_counts=""
	for _ in $(seq 1 40); do
		metadata_counts="$(clickhouse_query "SELECT concat(
			toString((SELECT count() > 0 FROM siginsight_logs.logs_attribute_keys WHERE name = 'smoke.kind')),
			':',
			toString((SELECT count() > 0 FROM siginsight_logs.field_values WHERE tag_key = 'smoke.kind' AND string_value = 'migration')),
			':',
			toString((SELECT count() > 0 FROM siginsight_traces.span_attributes_keys WHERE tagKey = 'smoke.kind')),
			':',
			toString((SELECT count() > 0 FROM siginsight_traces.field_values WHERE tag_key = 'smoke.kind' AND string_value = 'migration')),
			':',
			toString((SELECT count() > 0 FROM siginsight_metrics.metadata WHERE metric_name = 'schema.smoke.gauge' AND attr_name = 'smoke.kind'))
		)")"
		if [[ "${metadata_counts}" == "1:1:1:1:1" ]]; then
			break
		fi
		sleep 1
	done
	assert_equal "1:1:1:1:1" "${metadata_counts}" "signal exporters retain field key/value metadata"

	local derived_counts
	derived_counts=""
	for _ in $(seq 1 40); do
		derived_counts="$(clickhouse_query "SELECT concat(
			toString((SELECT count() > 0 FROM siginsight_traces.operations WHERE name = 'schema-smoke-span' AND serviceName = 'schema-smoke')),
			':',
			toString((SELECT count() > 0 FROM siginsight_traces.trace_summary WHERE trace_id = '0123456789abcdef0123456789abcdef')),
			':',
			toString((SELECT count() > 0 FROM siginsight_traces.service_edges WHERE src = 'schema-smoke' AND dest = 'postgresql')),
			':',
			toString((SELECT count() > 0 FROM siginsight_metrics.metric_rollup_5m WHERE metric_name = 'schema.smoke.gauge')),
			':',
			toString((SELECT count() > 0 FROM siginsight_metrics.metric_rollup_30m WHERE metric_name = 'schema.smoke.gauge')),
			':',
			toString((SELECT count() > 0 FROM siginsight_metrics.metric_series_6h WHERE metric_name = 'schema.smoke.gauge')),
			':',
			toString((SELECT count() > 0 FROM siginsight_metrics.metric_series_1d WHERE metric_name = 'schema.smoke.gauge')),
			':',
			toString((SELECT count() > 0 FROM siginsight_metrics.metric_series_1w WHERE metric_name = 'schema.smoke.gauge')),
			':',
			toString((SELECT count() > 0 FROM siginsight_meter.meter_points WHERE metric_name = 'signoz.meter.log.count')),
			':',
			toString((SELECT count() > 0 FROM siginsight_meter.meter_rollup_1d WHERE metric_name = 'signoz.meter.log.count'))
		)")"
		if [[ "${derived_counts}" == "1:1:1:1:1:1:1:1:1:1" ]]; then
			break
		fi
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
docker run -d --name "${clickhouse_container}" \
	--network "${network}" \
	-p 127.0.0.1::9000 \
	-e CLICKHOUSE_SKIP_USER_SETUP=1 \
	"${CLICKHOUSE_IMAGE}" >/dev/null

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

echo "Creating and checking the fresh local baseline"
current_migrate ready
current_migrate bootstrap
current_migrate sync up
current_migrate sync check
assert_metadata_seed
echo "Upgrading a simulated pre-SigInsight ClickHouse installation"
simulate_pre_siginsight_storage_names
current_migrate bootstrap
current_migrate sync up
current_migrate sync check
current_migrate bootstrap
assert_legacy_storage_names_upgraded
assert_metadata_seed
seed_legacy_logs_json_schema
current_migrate async up
current_migrate async check
current_migrate bootstrap
current_migrate sync up
current_migrate sync check
current_migrate async up
current_migrate async check
assert_baseline_markers
assert_no_coordination_features
assert_legacy_logs_json_removed
assert_obsolete_tables_removed
assert_obsolete_columns_removed
assert_canonical_trace_columns
assert_core_tables_readable
assert_collector_writes_reduced_schema

echo "v1 baseline integration test passed"
