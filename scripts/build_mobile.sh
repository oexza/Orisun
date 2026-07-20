#!/usr/bin/env bash

set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "mobile build requires macOS so gomobile can produce the iOS XCFramework" >&2
  exit 1
fi

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_dir="${1:-${repo_dir}/build/mobile}"
android_api="${ORISUN_MOBILE_ANDROID_API:-24}"
android_targets="${ORISUN_MOBILE_ANDROID_TARGETS:-android/arm64,android/amd64}"
ios_version="${ORISUN_MOBILE_IOS_VERSION:-13.0}"
mobile_ldflags="${ORISUN_MOBILE_LDFLAGS:--s -w}"

if [[ -z "${ANDROID_HOME:-}" ]]; then
  if [[ -n "${ANDROID_SDK_ROOT:-}" ]]; then
    export ANDROID_HOME="${ANDROID_SDK_ROOT}"
  else
    export ANDROID_HOME="${HOME}/Library/Android/sdk"
  fi
fi
if [[ ! -d "${ANDROID_HOME}" ]]; then
  echo "Android SDK not found at ${ANDROID_HOME}" >&2
  exit 1
fi

if [[ -z "${ANDROID_NDK_HOME:-}" ]]; then
  shopt -s nullglob
  ndk_candidates=("${ANDROID_HOME}"/ndk/*)
  shopt -u nullglob
  if (( ${#ndk_candidates[@]} == 0 )); then
    echo "Android NDK not found below ${ANDROID_HOME}/ndk" >&2
    exit 1
  fi
  export ANDROID_NDK_HOME="${ndk_candidates[${#ndk_candidates[@]}-1]}"
fi

mkdir -p "${output_dir}"
aar="${output_dir}/orisun-mobile.aar"
xcframework="${output_dir}/OrisunMobile.xcframework"
xcframework_zip="${output_dir}/OrisunMobile.xcframework.zip"

# gomobile's archive writer expects a fresh destination.
rm -f "${aar}" "${xcframework_zip}"
rm -rf "${xcframework}"

cd "${repo_dir}"

echo "Building Android AAR (${android_targets}, API ${android_api})..."
go tool gomobile bind \
  -target="${android_targets}" \
  -androidapi "${android_api}" \
  -javapkg com.orisunlabs.orisun \
  -trimpath \
  -ldflags "${mobile_ldflags}" \
  -o "${aar}" \
  ./embedded/sqlite/mobile
unzip -tq "${aar}"

echo "Building iOS XCFramework (iOS ${ios_version}+)..."
go tool gomobile bind \
  -target=ios \
  -iosversion "${ios_version}" \
  -prefix Orisun \
  -trimpath \
  -ldflags "${mobile_ldflags}" \
  -o "${xcframework}" \
  ./embedded/sqlite/mobile
plutil -lint "${xcframework}/Info.plist"

ditto -c -k --sequesterRsrc --keepParent "${xcframework}" "${xcframework_zip}"
(
  cd "${output_dir}"
  shasum -a 256 "$(basename "${aar}")" "$(basename "${xcframework_zip}")" > SHA256SUMS
)

echo
echo "Mobile artifacts:"
ls -lh "${aar}" "${xcframework_zip}"
echo
cat "${output_dir}/SHA256SUMS"
