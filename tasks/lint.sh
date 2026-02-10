#!/bin/sh
# Run golangci-lint with ginkgolinter enabled.
# Env: LINT_FIX - set to "1" or "true" to auto-fix (default: disabled)
set -eu

echo "Running lint"

fix_flag=""
case "${LINT_FIX:-}" in
1 | true) fix_flag="--fix" ;;
esac

golangci-lint -E ginkgolinter run $fix_flag
