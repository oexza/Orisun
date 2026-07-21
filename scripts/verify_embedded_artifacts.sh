#!/usr/bin/env bash

set -euo pipefail

if (( $# == 0 )); then
  echo "usage: $0 <native artifact>..." >&2
  exit 1
fi

temporary_dir="$(mktemp -d)"
trap 'rm -rf "${temporary_dir}"' EXIT

scan_targets=()
for artifact in "$@"; do
  if [[ ! -e "${artifact}" ]]; then
    echo "embedded artifact does not exist: ${artifact}" >&2
    exit 1
  fi
  case "${artifact}" in
    *.aar)
      destination="${temporary_dir}/$(basename "${artifact}" .aar)"
      mkdir -p "${destination}"
      unzip -q "${artifact}" 'jni/*' -d "${destination}"
      scan_targets+=("${destination}")
      ;;
    *)
      scan_targets+=("${artifact}")
      ;;
  esac
done

pattern='google\.golang\.org/grpc|github\.com/nats-io/nats\.go|github\.com/OrisunLabs/Orisun/(server|nats|postgres|embedded/postgres)'
if matches="$(rg -a -l "${pattern}" "${scan_targets[@]}")"; then
  echo "embedded artifact guard found transport/server symbols:" >&2
  printf '%s\n' "${matches}" >&2
  exit 1
fi

echo "Embedded artifacts contain no transport/server symbols."
