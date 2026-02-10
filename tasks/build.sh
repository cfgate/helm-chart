#!/bin/sh
# Build the manager binary with embedded version metadata.
# Sources: version.sh (provides VERSION, COMMIT, BUILD_DATE)
# Env: VERSION_SUFFIX - passed to version.sh
set -eu

. "$(dirname "$0")/version.sh"

echo "Building bin/manager ${VERSION}"
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w \
  -X main.Version=${VERSION} \
  -X main.Commit=${COMMIT} \
  -X main.BuildDate=${BUILD_DATE}" \
	-o bin/manager ./cmd/manager
