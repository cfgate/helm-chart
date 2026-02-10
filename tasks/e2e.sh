#!/bin/sh
# Run E2E tests with ginkgo against a live Cloudflare API.
# Env: E2E_USE_EXISTING_CLUSTER - use existing cluster (default: "true")
# Env: E2E_PROCS              - parallel ginkgo procs (default: 4)
# Env: E2E_FOCUS              - ginkgo --focus regex (default: disabled)
set -eu

export E2E_USE_EXISTING_CLUSTER="${E2E_USE_EXISTING_CLUSTER:-true}"

echo "Running E2E tests"

mkdir -p out

focus_flag=""
if [ -n "${E2E_FOCUS:-}" ]; then
	focus_flag="--focus=${E2E_FOCUS}"
fi

ginkgo -vv \
	--procs="${E2E_PROCS:-4}" \
	--keep-going \
	--silence-skips \
	--poll-progress-after=15s \
	$focus_flag \
	--output-dir out \
	--json-report=run.json \
	--cover --covermode atomic --coverprofile coverage.out \
	--race \
	./test/e2e
