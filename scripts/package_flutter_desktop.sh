#!/usr/bin/env bash

set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
package_dir="${repo_dir}/clients/flutter/orisun_flutter"
output_dir="${repo_dir}/build/flutter"
stage_dir="${output_dir}/orisun_flutter"
version="$(awk '/^version:/ { print $2; exit }' "${package_dir}/pubspec.yaml")"

if (( $# > 0 )); then
  targets=("$@")
else
  shopt -s nullglob
  target_paths=("${repo_dir}"/build/desktop/*)
  shopt -u nullglob
  targets=()
  for target_path in "${target_paths[@]}"; do
    if [[ -d "${target_path}" ]]; then
      targets+=("$(basename "${target_path}")")
    fi
  done
fi

if (( ${#targets[@]} == 0 )); then
  echo "no desktop libraries found; run scripts/build_desktop.sh first" >&2
  exit 1
fi

mobile_aar="${repo_dir}/build/mobile/orisun-flutter-mobile.aar"
mobile_xcframework="${repo_dir}/build/mobile/OrisunFlutterMobile.xcframework"
if [[ ! -f "${mobile_aar}" || ! -d "${mobile_xcframework}" ]]; then
  echo "Flutter mobile artifacts are missing; run scripts/build_mobile.sh first" >&2
  exit 1
fi

rm -rf "${stage_dir}"
mkdir -p "${stage_dir}" "${output_dir}"

tar \
  --exclude='.dart_tool' \
  --exclude='.idea' \
  --exclude='build' \
  --exclude='coverage' \
  --exclude='native' \
  --exclude='android/libs' \
  --exclude='android/src/main/jniLibs' \
  --exclude='ios/orisun_flutter/Frameworks' \
  --exclude='pubspec.lock' \
  --exclude='.flutter-plugins-dependencies' \
  --exclude='ephemeral' \
  --exclude='GeneratedPluginRegistrant.*' \
  --exclude='generated_plugin_registrant.*' \
  --exclude='generated_plugins.cmake' \
  --exclude='local.properties' \
  --exclude='example/ios/Podfile' \
  --exclude='example/ios/Podfile.lock' \
  --exclude='example/web' \
  --exclude='*.iml' \
  -C "${package_dir}" \
  -cf - . | tar -C "${stage_dir}" -xf -

for target in "${targets[@]}"; do
  source_dir="${repo_dir}/build/desktop/${target}"
  if [[ ! -d "${source_dir}" ]]; then
    echo "desktop target '${target}' was not built at ${source_dir}" >&2
    exit 1
  fi
  case "${target}" in
    darwin-*) library_name="liborisun.dylib" ;;
    linux-*) library_name="liborisun.so" ;;
    windows-*) library_name="orisun.dll" ;;
    *)
      echo "unsupported desktop target directory '${target}'" >&2
      exit 1
      ;;
  esac
  if [[ ! -f "${source_dir}/${library_name}" ]]; then
    echo "missing ${source_dir}/${library_name}" >&2
    exit 1
  fi
  mkdir -p "${stage_dir}/native/${target}"
  cp -p "${source_dir}/${library_name}" "${stage_dir}/native/${target}/${library_name}"
done

mkdir -p "${stage_dir}/android/libs" "${stage_dir}/android/src/main/jniLibs" "${stage_dir}/ios/orisun_flutter/Frameworks"
unzip -p "${mobile_aar}" classes.jar > "${stage_dir}/android/libs/orisun-flutter-mobile.jar"
temporary_dir="$(mktemp -d)"
trap 'rm -rf "${temporary_dir}"' EXIT
unzip -q "${mobile_aar}" 'jni/*' -d "${temporary_dir}"
cp -R "${temporary_dir}/jni/." "${stage_dir}/android/src/main/jniLibs/"
cp -R "${mobile_xcframework}" "${stage_dir}/ios/orisun_flutter/Frameworks/OrisunFlutterMobile.xcframework"

(
  cd "${stage_dir}"
  find native android/libs android/src/main/jniLibs ios/orisun_flutter/Frameworks -type f | LC_ALL=C sort | xargs shasum -a 256 > NATIVE_SHA256SUMS
)

archive="${output_dir}/orisun_flutter-${version}.tar.gz"
rm -f "${archive}"
tar -C "${output_dir}" -czf "${archive}" orisun_flutter

echo "Flutter native package:"
ls -lh "${archive}"
echo
cat "${stage_dir}/NATIVE_SHA256SUMS"
