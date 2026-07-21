#!/usr/bin/env bash

set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_dir}"

targets=(
  ./orisun
  ./sqlite
)

dependencies="$(go list -tags=orisun_embedded -deps "${targets[@]}")"
for forbidden in \
  '^google\.golang\.org/grpc($|/)' \
  '^github\.com/nats-io($|/)' \
  '^github\.com/OrisunLabs/Orisun/(server|nats|postgres|embedded/postgres)($|/)'
do
  if matches="$(printf '%s\n' "${dependencies}" | grep -E "${forbidden}")"; then
    echo "embedded dependency guard rejected transport/server packages:" >&2
    printf '%s\n' "${matches}" >&2
    exit 1
  fi
done

echo "Embedded dependency graph is transport-free."
