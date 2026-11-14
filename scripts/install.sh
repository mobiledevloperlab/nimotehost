#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 Nimote contributors
# Per-user installer. Bundles need no network; source uses the Go module toolchain.
set -eu
if [ "$#" -gt 0 ]; then
    case "$1" in
        -h|--help)
            printf '%s\n' 'Usage: sh scripts/install.sh' 'From source: build with Go, then verify and install the complete bundle.' 'From a distribution: verify and install without Go. Run without sudo.'
            exit 0 ;;
        *) printf '%s\n' 'Unexpected argument. Use --help.' >&2; exit 2 ;;
    esac
fi
if [ "$(id -u)" -eq 0 ]; then
    printf '%s\n' 'Run the installer as your normal user, without sudo.' >&2
    exit 1
fi
installer_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
bundle_dir=$(CDPATH='' cd -- "$installer_dir/.." && pwd)
if [ -f "$bundle_dir/nimotehost" ] && [ -f "$bundle_dir/SHA256SUMS" ]; then
    exec "$bundle_dir/nimotehost" setup
fi
if [ ! -f "$bundle_dir/go.mod" ] || [ ! -f "$bundle_dir/tools/install/main.go" ]; then
    printf '%s\n' 'Complete NimoteHost source or distribution required.' >&2
    exit 1
fi
if ! command -v go >/dev/null 2>&1; then
    printf '%s\n' 'Source installation requires Go 1.26.5+. Prebuilt distributions do not need Go.' >&2
    exit 1
fi
cd -- "$bundle_dir"
exec go run ./tools/install
