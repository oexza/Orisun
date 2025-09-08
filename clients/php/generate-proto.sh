#!/bin/bash

# Script to generate PHP gRPC client stubs from eventstore.proto

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Generating PHP gRPC client stubs...${NC}"

# Check if protoc is installed
if ! command -v protoc &> /dev/null; then
    echo -e "${RED}Error: protoc is not installed. Please install Protocol Buffers compiler.${NC}"
    echo "On macOS: brew install protobuf"
    echo "On Ubuntu: sudo apt-get install protobuf-compiler"
    exit 1
fi

# Check if grpc_php_plugin is available
if ! command -v grpc_php_plugin &> /dev/null; then
    echo -e "${RED}Error: grpc_php_plugin is not installed.${NC}"
    echo "Please install gRPC PHP plugin:"
    echo "git clone -b v1.57.0 https://github.com/grpc/grpc"
    echo "cd grpc && make grpc_php_plugin"
    exit 1
fi

# Create generated directory
mkdir -p generated/Orisun/Generated

# Get the absolute path to the proto file
PROTO_DIR="../../eventstore"
PROTO_FILE="$PROTO_DIR/eventstore.proto"

if [ ! -f "$PROTO_FILE" ]; then
    echo -e "${RED}Error: Proto file not found at $PROTO_FILE${NC}"
    exit 1
fi

echo -e "${YELLOW}Generating PHP classes...${NC}"

# Generate PHP classes
protoc --proto_path="$PROTO_DIR" \
    --php_out=generated \
    --grpc_out=generated \
    --plugin=protoc-gen-grpc=`which grpc_php_plugin` \
    "$PROTO_FILE"

echo -e "${GREEN}PHP gRPC stubs generated successfully in generated/ directory${NC}"

# List generated files
echo -e "${YELLOW}Generated files:${NC}"
find generated -name "*.php" | sort

echo -e "${GREEN}Done!${NC}"