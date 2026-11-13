# Nimote Host

**English** | [简体中文](README.zh-CN.md)

A standalone Go runtime for persistent tasks, terminals and installed coding
agent CLIs over an existing authenticated SSH connection. Host listens only on
loopback, authenticates every RPC request and requires no Nimote account,
control plane, Tailcat or public relay.

Host is a **development preview (`1.0.0-dev`)**. It supports Linux/macOS/Windows
on amd64 and arm64. Native macOS/Windows service setup, restart, upgrade and
uninstall have been tested in logged-in user sessions. Full mobile-client/Agent
acceptance remains outstanding; see [CHANGELOG.md](CHANGELOG.md).


Documentation and acceptance records: [index](docs/README.md). Repository origin: [source split](docs/repository-split.md).

## Deploy a prebuilt release

End users do not need Go. Follow the [deployment guide](docs/deployment.md) to
download and verify a bundle, install it at a fixed path, configure the per-user
service and verify access through SSH. It includes Linux/macOS and Windows
PowerShell instructions, diagnostics and uninstall steps.

## One-command installation

From the Host source root (Go 1.26.5+) or an extracted distribution (no Go needed):

```sh
sh scripts/install.sh
```

For source installation, `go run ./tools/install` also works across platforms.
Windows PowerShell: `.\scripts\install.ps1`. The installer verifies the full bundle,
installs to a fixed user directory, starts the service and checks readiness.
Repeating a healthy installation does not restart it. Updates preserve the old
bundle and refuse to interrupt active tasks. See [deployment](docs/deployment.md)
for prerequisites, older preview upgrades and recovery.

## Build and run

Install Go 1.26.5 or later. Clone this repository and run from its root:

```sh
go run ./tools/build
```

This works from PowerShell as well as Unix shells. Run the generated binary at
`dist/<os>-<arch>/nimote-host` (`nimote-host.exe` on Windows):

```sh
./dist/linux-amd64/nimote-host start
```

For other targets or a versioned distribution:

```sh
go run ./tools/build -os windows -arch arm64 -version 1.0.0-dev
```

The complete distribution includes bilingual deployment instructions, licenses,
a module manifest, build metadata and SHA256SUMS. Builds also produce a versioned
`.tar.gz` (Linux/macOS) or `.zip` (Windows) and an adjacent `.sha256`. A Bash wrapper, `./scripts/build-host.sh <os> <arch> [version]`,
is also available. Preserve the entire distribution when sharing binaries.

Put the binary in a fixed location on the remote user's PATH, then use
`nimote-host install` to start a per-user background service and wait until ready, or `nimote-host start`
for foreground operation. This does not configure SSH or install Agent CLIs.
Tasks outlive client disconnection; Host/OS restart marks unfinished sessions
interrupted and does not resurrect their processes.

## Interfaces

- Authenticated SSH exec: `nimote-host rpc` or `rpc-encoded`.
- Local authenticated HTTP RPC: task/session lifecycle, terminal input/resize,
  byte-offset output replay and persistent state events.
- Stdio MCP: read-only by default; writes require explicit local opt-in.
- Agent terminal adapters: installed Codex, Claude Code and OpenCode CLIs.
  Unified structured approval is not implemented.

Read [installation and protocol details](docs/host.en.md),
[upgrade and maintenance instructions](docs/maintenance.md),
[contribution guidelines](CONTRIBUTING.md) and [security policy](SECURITY.md).

## Development

```sh
go test -mod=readonly -race ./...
go vet ./...
```

`make build` and `make check` are optional shortcuts. The standalone CI covers
three native platforms and six cross-build targets. Tests do not install real
system services. All runtime packages are internal; external integrations use
the versioned RPC/MCP protocol. Module path: `github.com/mobiledevloperlab/nimote-host`.

## License

[Apache-2.0](LICENSE), copyright 2026 Nimote contributors. The license applies
to this standalone Host source tree. Third-party components retain their own
licenses; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
