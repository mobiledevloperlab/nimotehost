#!/bin/sh
# Verify standalone source with no inherited workspace or Go caches.
set -eu
source_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
check_dir=$(mktemp -d "${TMPDIR:-/tmp}/nimote-host-clean.XXXXXXXX")
cleanup() {
    # Go's module cache contains read-only directories.
    chmod -R u+w "$check_dir"
    rm -rf "$check_dir"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
mkdir -p "$check_dir/source" "$check_dir/modules" "$check_dir/cache"
# Copy only the standalone source contract, never installed binaries or state.
(cd "$source_root" && tar -cf - .github .gitignore README* CONTRIBUTING* SECURITY* CHANGELOG* THIRD_PARTY_NOTICES* LICENSE NOTICE Makefile go.mod go.sum cmd internal scripts docs tools third_party test) |
    (cd "$check_dir/source" && tar -xf -)
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
