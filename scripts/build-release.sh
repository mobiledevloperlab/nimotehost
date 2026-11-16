#!/usr/bin/env bash
set -euo pipefail
host_repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exec python3 "$host_repo_root/scripts/build-release.py" "$@"
