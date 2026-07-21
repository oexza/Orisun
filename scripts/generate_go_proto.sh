#!/usr/bin/env bash

set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
proto_dir="${repo_dir}/proto"

cd "${proto_dir}"

# Messages remain in the transport-neutral orisun package.
protoc \
  --go_out=../orisun \
  --go_opt=paths=source_relative \
  admin.proto eventstore.proto

# gRPC stubs live in the transport-only grpcapi package. Explicit package
# mappings keep the canonical proto contracts unchanged; messages.go provides
# aliases to the transport-neutral generated message types.
protoc \
  --go-grpc_out=.. \
  --go-grpc_opt=module=github.com/OrisunLabs/Orisun \
  '--go-grpc_opt=Madmin.proto=github.com/OrisunLabs/Orisun/orisun/grpcapi;grpcapi' \
  '--go-grpc_opt=Meventstore.proto=github.com/OrisunLabs/Orisun/orisun/grpcapi;grpcapi' \
  admin.proto eventstore.proto
