#!/bin/sh
# Verify standalone source with no inherited workspace or Go caches.
set -eu
source_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
check_dir=$(mktemp -d "${TMPDIR:-/tmp}/nimotehost-clean.XXXXXXXX")
cleanup() {
    # Go's module cache contains read-only directories.
    chmod -R u+w "$check_dir"
    rm -rf "$check_dir"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
mkdir -p "$check_dir/source" "$check_dir/modules" "$check_dir/cache"
# Run the same standalone source export used for releases.
bash "$source_root/scripts/export-source.sh" "$check_dir/source.tar.gz"
tar -xzf "$check_dir/source.tar.gz" -C "$check_dir/source"
cd "$check_dir/source"
export GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly
export GOMODCACHE="$check_dir/modules" GOCACHE="$check_dir/cache"
unset GOOS GOARCH CGO_ENABLED
# Avoid local go env -w settings such as private module/cache overrides.
export GOENV=off
go version
python3 -m unittest discover -s test -p 'test_*.py'
go mod download
go mod verify
go test -mod=readonly -race ./...
go vet ./...
go run ./tools/build
printf '%s\n' 'Clean standalone tests and distribution build passed.'
