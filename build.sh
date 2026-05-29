#!/bin/bash
set -euo pipefail

# Default to current system's OS and architecture if not specified
TARGET_OS=${1:-"darwin"}
TARGET_ARCH=${2:-"arm64"}
VERSION=${3:-"dev"}
FLAVOR=${4:-"all"}
BUILD_TIME=${BUILD_TIME:-$(date -u +'%Y-%m-%dT%H:%M:%SZ')}
GIT_COMMIT=${GIT_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}

# Set the output binary name and the target OS/architecture
case "$FLAVOR" in
  all)
    OUTPUT_NAME="orisun-$TARGET_OS-$TARGET_ARCH"
    PACKAGE="./cmd/main.go"
    ;;
  pg|postgres)
    OUTPUT_NAME="orisun-pg-$TARGET_OS-$TARGET_ARCH"
    PACKAGE="./cmd/orisun-pg"
    ;;
  sqlite)
    OUTPUT_NAME="orisun-sqlite-$TARGET_OS-$TARGET_ARCH"
    PACKAGE="./cmd/orisun-sqlite"
    ;;
  *)
    echo "Unknown flavor '$FLAVOR' (expected: all, pg, postgres, sqlite)"
    exit 1
    ;;
esac

echo "Building $FLAVOR for $TARGET_OS/$TARGET_ARCH with version $VERSION..."

if CGO_ENABLED=0 GOOS=$TARGET_OS GOARCH=$TARGET_ARCH go build -tags development="false" -a -installsuffix cgo \
  -ldflags="-w -s -X 'orisun/common.Version=$VERSION' -X 'orisun/common.BuildTime=$BUILD_TIME' -X 'orisun/common.GitCommit=$GIT_COMMIT'" \
  -o "./build/$OUTPUT_NAME" "$PACKAGE"; then
  echo "Build successful! Binary created: ./build/$OUTPUT_NAME"
else
  echo "Build failed!"
  exit 1
fi
