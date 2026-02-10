# Shared version calculation — source this file, do not execute directly.
# Sets: TAG, VERSION, COMMIT, BUILD_DATE
# Env: VERSION_SUFFIX - appended to version string (e.g. "-ci")

TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)

if [ -n "$TAG" ] && [ "$(git describe --tags --exact-match 2>/dev/null)" = "$TAG" ]; then
	VERSION="${TAG#v}"
else
	BASE="${TAG#v}"
	VERSION="${BASE:-0.0.0}-dev+${COMMIT}"
fi

if [ -n "${VERSION_SUFFIX:-}" ]; then
	VERSION="${VERSION}${VERSION_SUFFIX}"
fi
