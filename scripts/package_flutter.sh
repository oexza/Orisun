#!/usr/bin/env bash

set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exec "${repo_dir}/scripts/package_flutter_desktop.sh" "$@"
