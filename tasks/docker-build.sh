#!/bin/sh
# Build the Docker image with embedded version metadata.
# Sources: version.sh (provides VERSION, COMMIT, BUILD_DATE)
# Env: IMG - image name:tag (default: cfgate:local)
# Env: VERSION_SUFFIX - passed to version.sh
set -eu

. "$(dirname "$0")/version.sh"

IMG="${IMG:-cfgate:local}"

echo "Building Docker image ${IMG}"
docker build \
	--build-arg VERSION="${VERSION}" \
	--build-arg COMMIT="${COMMIT}" \
	--build-arg BUILD_DATE="${BUILD_DATE}" \
	-t "${IMG}" .
