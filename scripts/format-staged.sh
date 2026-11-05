#!/usr/bin/env bash
set -euo pipefail

repository_root="$(git rev-parse --show-toplevel)"
cd "${repository_root}"

mapfile -d '' -t go_files < <(
	git diff --cached --name-only -z --diff-filter=ACMR -- '*.go'
)

if (( ${#go_files[@]} )); then
	gofmt -s -w "${go_files[@]}"
	git add -- "${go_files[@]}"
fi
