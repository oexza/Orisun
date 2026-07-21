#!/usr/bin/env bash

set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
mobile_dir="${repo_dir}/build/mobile"
package_dir="${repo_dir}/clients/flutter/orisun_flutter"
aar="${mobile_dir}/orisun-flutter-mobile.aar"
xcframework="${mobile_dir}/OrisunFlutterMobile.xcframework"

if [[ ! -f "${aar}" || ! -d "${xcframework}" ]]; then
  echo "Flutter mobile artifacts are missing; run ./scripts/build_mobile.sh first" >&2
  exit 1
fi

mkdir -p "${package_dir}/android/libs" "${package_dir}/android/src/main/jniLibs" "${package_dir}/ios/orisun_flutter/Frameworks"
rm -f "${package_dir}/android/libs/orisun-flutter-mobile.aar" "${package_dir}/android/libs/orisun-flutter-mobile.jar"
rm -rf "${package_dir}/android/src/main/jniLibs"
rm -rf "${package_dir}/ios/orisun_flutter/Frameworks/OrisunFlutterMobile.xcframework"
mkdir -p "${package_dir}/android/src/main/jniLibs"
unzip -p "${aar}" classes.jar > "${package_dir}/android/libs/orisun-flutter-mobile.jar"
temporary_dir="$(mktemp -d)"
trap 'rm -rf "${temporary_dir}"' EXIT
unzip -q "${aar}" 'jni/*' -d "${temporary_dir}"
cp -R "${temporary_dir}/jni/." "${package_dir}/android/src/main/jniLibs/"
cp -R "${xcframework}" "${package_dir}/ios/orisun_flutter/Frameworks/OrisunFlutterMobile.xcframework"

echo "Prepared Flutter Android and iOS native artifacts."
