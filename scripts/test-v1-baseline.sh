#!/usr/bin/env bash

set -euo pipefail

readonly CLICKHOUSE_IMAGE="${CLICKHOUSE_IMAGE:-clickhouse/clickhouse-server:25.5.6}"
readonly ZOOKEEPER_IMAGE="${ZOOKEEPER_IMAGE:-signoz/zookeeper:3.7.1}"
readonly LEGACY_COLLECTOR_IMAGE="${LEGACY_COLLECTOR_IMAGE:-ghcr.io/siginsight/siginsight-otel-collector:v1.0.4}"

repo_root="$(git rev-parse --show-toplevel)"
run_id="$$-${RANDOM}"
network="otel-v1-baseline-${run_id}"
clickhouse_container="${network}-clickhouse"
zookeeper_container="${network}-zookeeper"
work_dir="$(mktemp -d)"
collector_binary="${work_dir}/siginsight-otel-collector"

cleanup() {
	docker rm -f -v "${clickhouse_container}" "${zookeeper_container}" >/dev/null 2>&1 || true
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
	local replication="$1"
	shift
	"${collector_binary}" migrate "$@" \
		--clickhouse-dsn="tcp://127.0.0.1:${clickhouse_port}" \
		--clickhouse-cluster=cluster \
		--clickhouse-replication="${replication}" \
		--timeout=10m
}

legacy_migrate() {
	local replication="$1"
	shift
	docker run --rm --network "${network}" "${LEGACY_COLLECTOR_IMAGE}" migrate "$@" \
		--clickhouse-dsn=tcp://clickhouse:9000 \
		--clickhouse-cluster=cluster \
		--clickhouse-replication="${replication}" \
		--timeout=10m
}

drop_telemetry_databases() {
	local database
	for database in signoz_traces signoz_metrics signoz_logs signoz_metadata signoz_analytics signoz_meter; do
		clickhouse_query "DROP DATABASE IF EXISTS ${database} ON CLUSTER cluster SYNC" >/dev/null
	done
}

declare -Ar non_replicated_table_fingerprints=(
	[signoz_analytics]="2:9115775075856150932"
	[signoz_logs]="12:14053648442494272017"
	[signoz_metadata]="4:14174565384524331006"
	[signoz_meter]="5:5717364897257198659"
	[signoz_metrics]="31:8955509188233892107"
	[signoz_traces]="34:16604672559652066814"
)

declare -Ar replicated_table_fingerprints=(
	[signoz_analytics]="2:14206733227540495132"
	[signoz_logs]="12:15653638739826216655"
	[signoz_metadata]="4:14103122399752458768"
	[signoz_meter]="5:16900608539726510724"
	[signoz_metrics]="31:3479616427438521389"
	[signoz_traces]="34:3709484046233630162"
)

declare -Ar column_fingerprints=(
	[signoz_analytics]="21:4487537369260509279"
	[signoz_logs]="84:10876619119462323544"
	[signoz_metadata]="26:3058743734507644696"
	[signoz_meter]="62:12106642581548859426"
	[signoz_metrics]="330:15891760589189666270"
	[signoz_traces]="406:4301296587597110300"
)

assert_schema_fingerprints() {
	local replication="$1"
	local database expected_table actual_table actual_columns
	for database in signoz_analytics signoz_logs signoz_metadata signoz_meter signoz_metrics signoz_traces; do
		if [[ "${replication}" == "true" ]]; then
			expected_table="${replicated_table_fingerprints[${database}]}"
		else
			expected_table="${non_replicated_table_fingerprints[${database}]}"
		fi

		actual_table="$(clickhouse_query "SELECT concat(toString(count()), ':', toString(cityHash64(arrayStringConcat(arraySort(groupArray(concat(name, '|', engine, '|', partition_key, '|', sorting_key, '|', primary_key, '|', sampling_key, '|', create_table_query))), '\\n')))) FROM system.tables WHERE database = '${database}' AND name NOT IN ('schema_migrations', 'schema_migrations_v2', 'distributed_schema_migrations_v2')")"
		assert_equal "${expected_table}" "${actual_table}" "${database} table fingerprint"

		actual_columns="$(clickhouse_query "SELECT concat(toString(count()), ':', toString(cityHash64(arrayStringConcat(arraySort(groupArray(concat(table, '|', toString(position), '|', name, '|', type, '|', default_kind, '|', default_expression, '|', compression_codec))), '\\n')))) FROM system.columns WHERE database = '${database}' AND table NOT IN ('schema_migrations', 'schema_migrations_v2', 'distributed_schema_migrations_v2')")"
		assert_equal "${column_fingerprints[${database}]}" "${actual_columns}" "${database} column fingerprint"
	done
}

assert_baseline_markers() {
	local exact_history="$1"
	local database marker history_count
	for database in signoz_traces signoz_metrics signoz_logs signoz_metadata signoz_analytics signoz_meter; do
		marker="$(clickhouse_query "SELECT concat(toString(migration_id), ':', status) FROM ${database}.distributed_schema_migrations_v2 WHERE migration_id = 999 SETTINGS final = 1")"
		assert_equal "999:finished" "${marker}" "${database} baseline marker"
		if [[ "${exact_history}" == "true" ]]; then
			history_count="$(clickhouse_query "SELECT count() FROM ${database}.distributed_schema_migrations_v2 SETTINGS final = 1")"
			assert_equal "1" "${history_count}" "${database} fresh migration history"
		fi
	done
}

assert_metadata_seed() {
	local seed_count
	seed_count="$(clickhouse_query "SELECT count() FROM signoz_metadata.distributed_column_evolution_metadata WHERE (signal, column_name, column_type, version) IN (('logs', 'resources_string', 'Map(LowCardinality(String), Float64)', 0), ('logs', 'resource', 'JSON()', 1), ('traces', 'resources_string', 'Map(LowCardinality(String), Float64)', 0), ('traces', 'resource', 'JSON()', 1))")"
	assert_equal "4" "${seed_count}" "metadata seed records"
}

assert_baseline_commands() {
	local replication="$1"
	current_migrate "${replication}" sync check
	current_migrate "${replication}" async up
	current_migrate "${replication}" async check
}

echo "Building current collector"
go build -tags=remove_all_sd -o "${collector_binary}" "${repo_root}/cmd/siginsightotelcollector"

docker network create "${network}" >/dev/null
docker run -d --name "${zookeeper_container}" \
	--network "${network}" --network-alias zookeeper-1 \
	-e ALLOW_ANONYMOUS_LOGIN=yes \
	"${ZOOKEEPER_IMAGE}" >/dev/null
docker run -d --name "${clickhouse_container}" \
	--network "${network}" --network-alias clickhouse \
	-p 127.0.0.1::9000 \
	-e CLICKHOUSE_SKIP_USER_SETUP=1 \
	-v "${repo_root}/example/docker/common/clickhouse/config.xml:/etc/clickhouse-server/config.xml:ro" \
	-v "${repo_root}/example/docker/common/clickhouse/cluster.xml:/etc/clickhouse-server/config.d/cluster.xml:ro" \
	"${CLICKHOUSE_IMAGE}" >/dev/null

clickhouse_port="$(docker port "${clickhouse_container}" 9000/tcp | awk -F: 'NR == 1 { print $NF }')"
[[ -n "${clickhouse_port}" ]] || fail "could not resolve published ClickHouse port"

for _ in $(seq 1 60); do
	if clickhouse_query "SELECT 1" >/dev/null 2>&1 && clickhouse_query "SELECT name FROM system.zookeeper WHERE path='/' LIMIT 1" >/dev/null 2>&1; then
		break
	fi
	sleep 1
done
version="$(clickhouse_query "SELECT version()")"
[[ "${version}" == 25.5.6.* ]] || fail "ClickHouse version is ${version}, want 25.5.6.x"

echo "[1/4] Adopting a completed v1.0.4 schema"
legacy_migrate false bootstrap
legacy_migrate false sync up
legacy_migrate false async up
current_migrate false sync up
assert_schema_fingerprints false
assert_baseline_markers false
assert_metadata_seed
assert_baseline_commands false
drop_telemetry_databases

echo "[2/4] Creating a fresh non-replicated baseline"
current_migrate false bootstrap
current_migrate false sync up
assert_schema_fingerprints false
assert_baseline_markers true
assert_metadata_seed
assert_baseline_commands false
drop_telemetry_databases

echo "[3/4] Creating a fresh replicated baseline"
current_migrate true bootstrap
current_migrate true sync up
assert_schema_fingerprints true
assert_baseline_markers true
assert_metadata_seed
assert_baseline_commands true
drop_telemetry_databases

echo "[4/4] Rejecting a failed baseline without recovery"
current_migrate false bootstrap
clickhouse_query "INSERT INTO signoz_logs.distributed_schema_migrations_v2 (migration_id, status, created_at) VALUES (999, 'failed', now64(9))"
set +e
failure_output="$(current_migrate false sync up 2>&1)"
failure_status=$?
set -e
[[ ${failure_status} -ne 0 ]] || fail "failed baseline was accepted"
[[ "${failure_output}" == *"automatic migration recovery is disabled"* ]] || fail "failed baseline did not report recovery policy"
domain_table_count="$(clickhouse_query "SELECT count() FROM system.tables WHERE database IN ('signoz_traces', 'signoz_metrics', 'signoz_logs', 'signoz_metadata', 'signoz_analytics', 'signoz_meter') AND name NOT IN ('schema_migrations', 'schema_migrations_v2', 'distributed_schema_migrations_v2')")"
assert_equal "0" "${domain_table_count}" "business tables after rejected recovery"

echo "v1 baseline integration test passed"
