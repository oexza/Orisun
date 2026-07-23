#!/usr/bin/env bash

set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
proto_dir="${repo_dir}/proto"

cd "${proto_dir}"

# Generated messages and gRPC stubs are both owned by the transport adapter.
protoc \
  --go_out=.. \
  --go_opt=module=github.com/OrisunLabs/Orisun \
  --go-grpc_out=.. \
  --go-grpc_opt=module=github.com/OrisunLabs/Orisun \
  admin.proto eventstore.proto
