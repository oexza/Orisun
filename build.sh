#!/bin/bash

# Default to current system's OS and architecture if not specified
TARGET_OS=${1:-"darwin"}
TARGET_ARCH=${2:-"arm64"}
VERSION=${3:-"dev"}

# Set the output binary name and the target OS/architecture
OUTPUT_NAME="orisun-$TARGET_OS-$TARGET_ARCH"

# Build the binary with version information
echo "Building for $TARGET_OS/$TARGET_ARCH with version $VERSION..."
GOOS=$TARGET_OS GOARCH=$TARGET_ARCH go build -tags development="false" -a -installsuffix cgo \
  -ldflags="-w -s -X 'orisun/common.Version=$VERSION' -X 'orisun/common.BuildTime=$(date -u +'%Y-%m-%dT%H:%M:%SZ')' -X 'orisun/common.GitCommit=$(git rev-parse --short HEAD)'" \
  -gcflags="-m" -o ./build/$OUTPUT_NAME ./main.go

# Check if the build was successful
if [ $? -eq 0 ]; then
    echo "Build successful! Binary created: ./build/$OUTPUT_NAME"
else
    echo "Build failed!"
fi