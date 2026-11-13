# Maintenance, compatibility and releases

**English** | [简体中文](maintenance.zh-CN.md)

## Module boundary

This repository builds as `github.com/mobiledevloperlab/nimote-host`. No local replacement,
parent workspace, Connect account or relay package is needed. Keep transport
clients outside this module and use the documented RPC/MCP interfaces.
`internal/host` owns the protocol, persistence and process manager;
`internal/hostservice` owns native per-user service definitions;
`cmd/nimote-host` owns CLI routing; `tools/build` owns distributions.

## Development and dependency updates

Run `make check`, or the equivalent Go commands from CONTRIBUTING.md. Connect is a separate repository with its own module and checks. Host checks
never require a sibling checkout. Dependabot proposes weekly module and Actions updates.

For a Go dependency update:

1. Run `go get module@version` and `go mod tidy` in this module only.
2. Review the upstream changes and license changes. Update the corresponding
   `third_party/<directory>/LICENSE` and any patent/notice files from the exact
   module version (`go mod download -json module@version` locates its directory).
3. Update `third_party/dependencies.json` and THIRD_PARTY_NOTICES.md. New modules
   need a license directory and a record. Do not add local `replace` directives
   to release commits. The builder checks the entire module graph against these
   records and rejects unknown modules or versions.
4. Run tests/race/vet and all six build targets. Windows/macOS runtime changes
   also require their native CI. Review the diff, including go.sum and licenses.

For a Go toolchain update, update go.mod and the documented minimum, refresh
`third_party/go/LICENSE` and PATENTS from the new toolchain, and run the same
checks. The binary builder also bundles the actual build toolchain's license
and patent files. Keep source notices consistent with the minimum toolchain.

## Protocol and state changes

Version and wire protocol are separate: release builds set `host.Version`, while
`Protocol` and minimum compatibility are explicit. Prefer additive fields;
introducing a new request field requires capability/version negotiation because
servers reject unknown request fields. Keep capabilities truthful when adapters
or structured approvals are unavailable.

Preserve existing state readers, output offsets, Host identity and credentials.
Document migrations before changing persisted formats. During development there
is no general downgrade compatibility guarantee for changed state formats.
Changes need recovery tests, including interrupted processes and old events.

## Build and release

Use the cross-platform Go entry point, including from PowerShell:

```sh
go run ./tools/build -os linux -arch amd64 -version 1.0.0-dev
go run ./tools/build -os darwin -arch arm64 -version 1.0.0-dev
go run ./tools/build -os windows -arch amd64 -version 1.0.0-dev
```

Each `dist/<os>-<arch>/` contains the binary, project and dependency licenses,
MODULES.json, BUILD.json and SHA256SUMS. Distribute the entire directory, not only
the executable. Version must start with a digit. Use prerelease versions until
native services and real client/Agent acceptance are recorded.

The CI workflow checks native platforms and builds/uploads six complete bundles.
It supports manual versioned builds through workflow_dispatch. Download and
review those artifacts before creating a release; publication is a maintainer
action. CI artifacts and local checks are not code signing or notarization.

## Installed binary upgrades and rollback

1. Check `nimote-host version` and capabilities. Download/build the target bundle
   and verify SHA256SUMS against the trusted release's published checksums.
2. Allow running tasks to finish, then run `nimote-host stop`. Stopping Host
   terminates remaining managed processes. Confirm it has stopped before replacing
   files; do not overwrite a running Windows executable.
3. Preserve the private Host state directory and keep a private backup before
   any documented state migration. Backups contain credentials and task output.
4. Replace the binary at the same fixed path referenced by the user service.
   Run `nimote-host install` to refresh the service definition and start it, or
   `nimote-host start` for foreground operation. Moving the binary requires
   refreshing the registered service path.
5. Check version, status and capabilities through authenticated SSH.

For rollback, stop Host and restore the previous binary at that path. If a state
migration occurred, follow the release's downgrade instructions; do not assume
older binaries can read new state. Restoring credentials or state from backup
requires the user's deliberate choice. Upgrades never restart interrupted tasks
or automatically approve an Agent operation.

Builds clear the selected `dist/<os>-<arch>/` before writing fresh files; do not
store hand-managed files there. Each build also writes a versioned `.tar.gz`
(Linux/macOS) or `.zip` (Windows) in `dist/`, with an adjacent `.sha256`.
See [deployment](deployment.md) for installing these packages without Go.
