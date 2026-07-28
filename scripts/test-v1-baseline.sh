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

declare -Ar table_fingerprints=(
	[signoz_analytics]="1:573356622719098860"
	[signoz_logs]="6:10013745453448773421"
	[signoz_metadata]="2:9471719162152962200"
	[signoz_meter]="3:5731232586497943347"
	[signoz_metrics]="18:4727395258706994518"
	[signoz_traces]="21:15141153314076288482"
)

declare -Ar column_fingerprints=(
	[signoz_analytics]="11:4111144204702223109"
	[signoz_logs]="43:15143235747694373747"
	[signoz_metadata]="13:11073036235476819325"
	[signoz_meter]="38:8743824752276981965"
	[signoz_metrics]="196:954080453982615115"
	[signoz_traces]="235:12795843324237009066"
)

assert_schema_fingerprints() {
	local database actual_table actual_columns
	for database in signoz_analytics signoz_logs signoz_metadata signoz_meter signoz_metrics signoz_traces; do
		actual_table="$(clickhouse_query "SELECT concat(toString(count()), ':', toString(cityHash64(arrayStringConcat(arraySort(groupArray(concat(name, '|', engine, '|', partition_key, '|', sorting_key, '|', primary_key, '|', sampling_key, '|', create_table_query))), '\\n')))) FROM system.tables WHERE database = '${database}' AND name NOT IN ('schema_migrations', 'schema_migrations_v2')")"
		assert_equal "${table_fingerprints[${database}]}" "${actual_table}" "${database} table fingerprint"

		actual_columns="$(clickhouse_query "SELECT concat(toString(count()), ':', toString(cityHash64(arrayStringConcat(arraySort(groupArray(concat(table, '|', toString(position), '|', name, '|', type, '|', default_kind, '|', default_expression, '|', compression_codec))), '\\n')))) FROM system.columns WHERE database = '${database}' AND table NOT IN ('schema_migrations', 'schema_migrations_v2')")"
		assert_equal "${column_fingerprints[${database}]}" "${actual_columns}" "${database} column fingerprint"
	done
}

assert_baseline_markers() {
	local database marker history_count expected_history_count
	for database in signoz_traces signoz_metrics signoz_logs signoz_metadata signoz_analytics signoz_meter; do
		marker="$(clickhouse_query "SELECT concat(toString(migration_id), ':', status) FROM ${database}.schema_migrations_v2 WHERE migration_id = 999 SETTINGS final = 1")"
		assert_equal "999:finished" "${marker}" "${database} baseline marker"
		history_count="$(clickhouse_query "SELECT count() FROM ${database}.schema_migrations_v2 SETTINGS final = 1")"
		expected_history_count=2
		if [[ "${database}" == "signoz_logs" ]]; then
			expected_history_count=4
		fi
		assert_equal "${expected_history_count}" "${history_count}" "${database} migration history"
	done
}

seed_legacy_logs_json_schema() {
	clickhouse_query "ALTER TABLE signoz_logs.logs_v2 ADD COLUMN IF NOT EXISTS body_v2 JSON(max_dynamic_paths=0) AFTER body"
	clickhouse_query "ALTER TABLE signoz_logs.logs_v2 ADD COLUMN IF NOT EXISTS body_promoted JSON() AFTER body_v2"
	clickhouse_query "ALTER TABLE signoz_logs.logs_v2 ADD INDEX IF NOT EXISTS body_v2_string_ngram_idx lower(toString(body_v2)) TYPE ngrambf_v1(4, 15000, 3, 0) GRANULARITY 1"
	clickhouse_query "ALTER TABLE signoz_logs.logs_v2 ADD INDEX IF NOT EXISTS body_v2_string_token_idx lower(toString(body_v2)) TYPE tokenbf_v1(10000, 2, 0) GRANULARITY 1"
	clickhouse_query "ALTER TABLE signoz_logs.logs_v2 ADD INDEX IF NOT EXISTS body_v2_paths_ngram_idx JSONAllPaths(body_v2) TYPE ngrambf_v1(4, 15000, 3, 0) GRANULARITY 1"
	clickhouse_query "ALTER TABLE signoz_logs.logs_v2 ADD INDEX IF NOT EXISTS body_v2_paths_token_idx JSONAllPaths(body_v2) TYPE tokenbf_v1(10000, 2, 0) GRANULARITY 1"
	clickhouse_query "CREATE TABLE IF NOT EXISTS signoz_metadata.json_path_types (path String, type String, last_seen UInt64) ENGINE = ReplacingMergeTree ORDER BY (path, type)"
	clickhouse_query "INSERT INTO signoz_logs.logs_v2 (ts_bucket_start, timestamp, observed_timestamp, id, body, body_v2, body_promoted) VALUES (toUInt64(toUnixTimestamp(now())), toUInt64(toUnixTimestamp64Nano(now64(9))), toUInt64(toUnixTimestamp64Nano(now64(9))), 'legacy-json-cleanup-test', '{\"service\":{\"name\":\"api\"}}', '{\"service\":{\"name\":\"api\"}}', '{\"service\":{\"name\":\"api\"}}')"
	clickhouse_query "INSERT INTO signoz_logs.schema_migrations_v2 (migration_id, status, created_at) VALUES (2001, 'finished', now())"
}

assert_legacy_logs_json_removed() {
	local columns indexes metadata_tables cleanup_marker body_json_value
	columns="$(clickhouse_query "SELECT count() FROM system.columns WHERE database = 'signoz_logs' AND table = 'logs_v2' AND name IN ('body_v2', 'body_promoted')")"
	assert_equal "0" "${columns}" "legacy logs JSON columns"
	indexes="$(clickhouse_query "SELECT count() FROM system.data_skipping_indices WHERE database = 'signoz_logs' AND table = 'logs_v2' AND name LIKE 'body_v2_%'")"
	assert_equal "0" "${indexes}" "legacy logs JSON indexes"
	metadata_tables="$(clickhouse_query "SELECT count() FROM system.tables WHERE database = 'signoz_metadata' AND name IN ('json_path_types', 'distributed_json_path_types')")"
	assert_equal "0" "${metadata_tables}" "legacy logs JSON metadata tables"
	cleanup_marker="$(clickhouse_query "SELECT concat(toString(migration_id), ':', status) FROM signoz_logs.schema_migrations_v2 WHERE migration_id = 2002 SETTINGS final = 1")"
	assert_equal "2002:finished" "${cleanup_marker}" "legacy logs JSON cleanup marker"
	body_json_value="$(clickhouse_query "SELECT JSON_VALUE(body, '$.service.name') FROM signoz_logs.logs_v2 WHERE id = 'legacy-json-cleanup-test' LIMIT 1")"
	assert_equal "api" "${body_json_value}" "body String JSON path query"
}

assert_metadata_seed() {
	local seed_count
	seed_count="$(clickhouse_query "SELECT count() FROM signoz_metadata.column_evolution_metadata WHERE (signal, column_name, column_type, version) IN (('logs', 'resources_string', 'Map(LowCardinality(String), Float64)', 0), ('logs', 'resource', 'JSON()', 1), ('traces', 'resources_string', 'Map(LowCardinality(String), Float64)', 0), ('traces', 'resource', 'JSON()', 1))")"
	assert_equal "4" "${seed_count}" "metadata seed records"
}

assert_obsolete_tables_removed() {
	local obsolete_tables
	obsolete_tables="$(clickhouse_query "SELECT count() FROM system.tables WHERE concat(database, '.', name) IN (
		'signoz_analytics.distributed_rule_state_history_v0',
		'signoz_logs.distributed_logs_attribute_keys',
		'signoz_logs.distributed_logs_resource_keys',
		'signoz_logs.distributed_logs_v2',
		'signoz_logs.distributed_logs_v2_resource',
		'signoz_logs.distributed_tag_attributes_v2',
		'signoz_logs.distributed_usage',
		'signoz_metadata.distributed_attributes_metadata',
		'signoz_metadata.distributed_column_evolution_metadata',
		'signoz_metadata.column_evolution_metadata',
		'signoz_meter.distributed_samples',
		'signoz_meter.distributed_samples_agg_1d',
		'signoz_metrics.distributed_exp_hist',
		'signoz_metrics.distributed_metadata',
		'signoz_metrics.distributed_samples_v2',
		'signoz_metrics.distributed_samples_v4',
		'signoz_metrics.distributed_samples_v4_agg_30m',
		'signoz_metrics.distributed_samples_v4_agg_5m',
		'signoz_metrics.distributed_time_series_v2',
		'signoz_metrics.distributed_time_series_v4',
		'signoz_metrics.distributed_time_series_v4_1day',
		'signoz_metrics.distributed_time_series_v4_1week',
		'signoz_metrics.distributed_time_series_v4_6hrs',
		'signoz_metrics.distributed_updated_metadata',
		'signoz_metrics.distributed_usage',
		'signoz_metrics.samples_v2',
		'signoz_metrics.time_series_v2',
		'signoz_traces.distributed_dependency_graph_minutes_v2',
		'signoz_traces.distributed_signoz_error_index_v2',
		'signoz_traces.distributed_signoz_index_v2',
		'signoz_traces.distributed_signoz_index_v3',
		'signoz_traces.distributed_signoz_spans',
		'signoz_traces.distributed_span_attributes',
		'signoz_traces.distributed_span_attributes_keys',
		'signoz_traces.distributed_tag_attributes_v2',
		'signoz_traces.distributed_top_level_operations',
		'signoz_traces.distributed_trace_summary',
		'signoz_traces.distributed_traces_v3_resource',
		'signoz_traces.distributed_usage',
		'signoz_traces.distributed_usage_explorer',
		'signoz_traces.span_attributes'
	)")"
	assert_equal "0" "${obsolete_tables}" "obsolete local and distributed tables"
}

assert_core_tables_readable() {
	local core_table_count
	core_table_count="$(clickhouse_query "SELECT count() FROM system.tables WHERE concat(database, '.', name) IN (
		'signoz_analytics.rule_state_history_v0',
		'signoz_logs.logs_v2',
		'signoz_logs.tag_attributes_v2',
		'signoz_metadata.attributes_metadata',
		'signoz_meter.samples',
		'signoz_meter.samples_agg_1d',
		'signoz_metrics.exp_hist',
		'signoz_metrics.metadata',
		'signoz_metrics.samples_v4',
		'signoz_metrics.time_series_v4',
		'signoz_traces.signoz_index_v3',
		'signoz_traces.span_attributes_keys',
		'signoz_traces.tag_attributes_v2'
	)")"
	assert_equal "13" "${core_table_count}" "Collector and SigInsight core local tables"

	clickhouse_query "SELECT timestamp, body, resource FROM signoz_logs.logs_v2 LIMIT 0" >/dev/null
	clickhouse_query "SELECT timestamp, trace_id, span_id, resource FROM signoz_traces.signoz_index_v3 LIMIT 0" >/dev/null
	clickhouse_query "SELECT tagKey, tagType, dataType FROM signoz_traces.span_attributes_keys LIMIT 0" >/dev/null
	clickhouse_query "SELECT metric_name, fingerprint, unix_milli, value FROM signoz_metrics.samples_v4 LIMIT 0" >/dev/null
	clickhouse_query "SELECT metric_name, fingerprint, labels, attrs, resource_attrs FROM signoz_metrics.time_series_v4 LIMIT 0" >/dev/null
}

assert_collector_writes_reduced_schema() {
	local now_ns end_ns smoke_counts body_json_value
	"${collector_binary}" --config <(sed \
		-e "s#tcp://clickhouse:9000#tcp://127.0.0.1:${clickhouse_port}#g" \
		-e 's#0.0.0.0:4317#127.0.0.1:14317#g' \
		-e 's#0.0.0.0:4318#127.0.0.1:14318#g' \
		-e 's#0.0.0.0:13133#127.0.0.1:14333#g' \
		-e 's#0.0.0.0:1777#127.0.0.1:14777#g' \
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
{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"schema-smoke"}}]},"scopeSpans":[{"scope":{"name":"schema-smoke"},"spans":[{"traceId":"0123456789abcdef0123456789abcdef","spanId":"0123456789abcdef","name":"schema-smoke-span","kind":2,"startTimeUnixNano":"${now_ns}","endTimeUnixNano":"${end_ns}","status":{"code":1}}]}]}]}
JSON

	curl --fail --silent -H 'Content-Type: application/json' \
		http://127.0.0.1:14318/v1/metrics --data-binary @- >/dev/null <<JSON
{"resourceMetrics":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"schema-smoke"}}]},"scopeMetrics":[{"scope":{"name":"schema-smoke"},"metrics":[{"name":"schema.smoke.gauge","unit":"1","gauge":{"dataPoints":[{"timeUnixNano":"${now_ns}","asDouble":42.5}]}}]}]}]}
JSON

	smoke_counts=""
	for _ in $(seq 1 40); do
		smoke_counts="$(clickhouse_query "SELECT concat(
			toString((SELECT count() > 0 FROM signoz_logs.logs_v2 WHERE resources_string['service.name'] = 'schema-smoke')),
			':',
			toString((SELECT count() > 0 FROM signoz_traces.signoz_index_v3 WHERE resources_string['service.name'] = 'schema-smoke')),
			':',
			toString((SELECT count() > 0 FROM signoz_metrics.samples_v4 WHERE metric_name = 'schema.smoke.gauge')),
			':',
			toString((SELECT count() > 0 FROM signoz_metrics.time_series_v4 WHERE metric_name = 'schema.smoke.gauge'))
		)")"
		if [[ "${smoke_counts}" == "1:1:1:1" ]]; then
			break
		fi
		sleep 1
	done
	assert_equal "1:1:1:1" "${smoke_counts}" "Collector writes to logs, traces, samples_v4, and time_series_v4"

	body_json_value="$(clickhouse_query "SELECT JSON_VALUE(body, '$.service.name') FROM signoz_logs.logs_v2 WHERE resources_string['service.name'] = 'schema-smoke' LIMIT 1")"
	assert_equal "api" "${body_json_value}" "Collector-written body String JSON path query"

	kill "${collector_pid}" >/dev/null 2>&1 || true
	wait "${collector_pid}" >/dev/null 2>&1 || true
	collector_pid=""
}

assert_no_coordination_features() {
	local invalid_tables on_cluster_count
	invalid_tables="$(clickhouse_query "SELECT count() FROM system.tables WHERE database LIKE 'signoz_%' AND (engine LIKE 'Replicated%' OR engine = 'Distributed')")"
	assert_equal "0" "${invalid_tables}" "replicated or Distributed telemetry tables"
	on_cluster_count="$(clickhouse_query "SELECT count() FROM system.tables WHERE database LIKE 'signoz_%' AND create_table_query LIKE '%ON CLUSTER%'")"
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
assert_schema_fingerprints
assert_metadata_seed
seed_legacy_logs_json_schema
current_migrate async up
current_migrate async check
assert_baseline_markers
assert_no_coordination_features
assert_legacy_logs_json_removed
assert_obsolete_tables_removed
assert_core_tables_readable
assert_collector_writes_reduced_schema

echo "v1 baseline integration test passed"
