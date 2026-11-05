#!/usr/bin/env bash
set -euo pipefail

repository_root="$(git rev-parse --show-toplevel)"
compose_dir="${repository_root}/example/docker"
rendered="$(mktemp)"
trap 'rm -f "${rendered}"' EXIT

docker compose \
  --env-file "${compose_dir}/.env.example" \
  -f "${compose_dir}/docker-compose.yaml" \
  config >"${rendered}"

required=(
  "ghcr.io/siginsight/siginsight:v2.2.2"
  "ghcr.io/siginsight/siginsight-otel-collector:v2.0.1"
  "localhost:8080/api/v5/healthz"
)
for value in "${required[@]}"; do
  if ! grep -Fq -- "${value}" "${rendered}"; then
    echo "compose contract is missing: ${value}" >&2
    exit 1
  fi
done

for stale in "v0.117.1" "v1.5.0" "/api/v1/health"; do
  if grep -Fq -- "${stale}" "${rendered}"; then
    echo "compose contract contains stale value: ${stale}" >&2
    exit 1
  fi
done
