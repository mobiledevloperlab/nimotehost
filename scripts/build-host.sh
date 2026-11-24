#!/usr/bin/env bash
# Compatibility wrapper; the Go builder also works directly on Windows.
set -euo pipefail
host_repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$host_repo_root"
exec go run ./tools/build -os "${1:-$(go env GOOS)}" -arch "${2:-$(go env GOARCH)}" -version "${3:-2.5.0-community}"
