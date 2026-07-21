#!/usr/bin/env bash

set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target_os="${1:-$(go env GOOS)}"
target_arch="${2:-$(go env GOARCH)}"

case "${target_os}" in
  darwin)
    library_name="liborisun.dylib"
    ;;
  linux)
    library_name="liborisun.so"
    ;;
  windows)
    library_name="orisun.dll"
    ;;
  *)
    echo "unsupported desktop OS '${target_os}' (expected darwin, linux, or windows)" >&2
    exit 1
    ;;
esac

case "${target_arch}" in
  amd64|arm64)
    ;;
  *)
    echo "unsupported desktop architecture '${target_arch}' (expected amd64 or arm64)" >&2
    exit 1
    ;;
esac

output_dir="${repo_dir}/build/desktop/${target_os}-${target_arch}"
mkdir -p "${output_dir}"

ldflags="-s -w"
if [[ "${target_os}" == "darwin" ]]; then
  # Dart/Flutter native assets rewrite Mach-O install names while bundling.
  # Reserve enough load-command space for the rewritten application path.
  ldflags="${ldflags} -extldflags=-Wl,-headerpad_max_install_names,-install_name,@rpath/liborisun.dylib"
fi

echo "Building Orisun desktop library for ${target_os}/${target_arch}..."
cd "${repo_dir}"
./scripts/verify_embedded_dependencies.sh
CGO_ENABLED=1 GOOS="${target_os}" GOARCH="${target_arch}" go build \
  -tags=orisun_embedded \
  -buildmode=c-shared \
  -trimpath \
  -ldflags="${ldflags}" \
  -o "${output_dir}/${library_name}" \
  ./cmd/orisun-ffi
./scripts/verify_embedded_artifacts.sh "${output_dir}/${library_name}"

(
  cd "${output_dir}"
  shasum -a 256 "${library_name}" > SHA256SUMS
)

echo
ls -lh "${output_dir}/${library_name}"
cat "${output_dir}/SHA256SUMS"
