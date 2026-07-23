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

cleanup() {
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
	local database marker history_count
	for database in signoz_traces signoz_metrics signoz_logs signoz_metadata signoz_analytics signoz_meter; do
		marker="$(clickhouse_query "SELECT concat(toString(migration_id), ':', status) FROM ${database}.schema_migrations_v2 WHERE migration_id = 999 SETTINGS final = 1")"
		assert_equal "999:finished" "${marker}" "${database} baseline marker"
		history_count="$(clickhouse_query "SELECT count() FROM ${database}.schema_migrations_v2 SETTINGS final = 1")"
		assert_equal "1" "${history_count}" "${database} fresh migration history"
	done
}

assert_metadata_seed() {
	local seed_count
	seed_count="$(clickhouse_query "SELECT count() FROM signoz_metadata.column_evolution_metadata WHERE (signal, column_name, column_type, version) IN (('logs', 'resources_string', 'Map(LowCardinality(String), Float64)', 0), ('logs', 'resource', 'JSON()', 1), ('traces', 'resources_string', 'Map(LowCardinality(String), Float64)', 0), ('traces', 'resource', 'JSON()', 1))")"
	assert_equal "4" "${seed_count}" "metadata seed records"
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
current_migrate async up
current_migrate async check
assert_schema_fingerprints
assert_baseline_markers
assert_metadata_seed
assert_no_coordination_features

echo "v1 baseline integration test passed"
