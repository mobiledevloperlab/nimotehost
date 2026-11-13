# Nimote Host

**English** | [简体中文](README.zh-CN.md)

A standalone Go runtime for persistent tasks, terminals and installed coding
agent CLIs over an existing authenticated SSH connection. Host listens only on
loopback, authenticates every RPC request and requires no Nimote account,
control plane, Tailcat or public relay.

Host is a **development preview (`1.0.0-dev`)**. It supports Linux/macOS/Windows
on amd64 and arm64. Full native-platform and mobile/Agent acceptance remains
outstanding; see
[pending functionality](docs/pending.md).

Current documentation: [completed functionality](docs/completed.md) and [pending functionality](docs/pending.md).

## Deploy a prebuilt release

End users do not need Go. See [implemented installation and service behavior](docs/completed.md#安装与服务--installation)
for bundle verification, stable installation paths and the per-user service commands.

## One-command installation

From the Host source root (Go 1.26.5+) or an extracted distribution (no Go needed):

```sh
sh scripts/install.sh
```

For source installation, `go run ./tools/install` also works across platforms.
Windows PowerShell: `.\scripts\install.ps1`. The installer verifies the full bundle,
installs to a fixed user directory, starts the service and checks readiness.
Repeating a healthy installation does not restart it. Updates preserve the old
bundle and refuse to interrupt active tasks. See [implemented installation behavior](docs/completed.md#安装与服务--installation)
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

The complete distribution includes current bilingual feature documentation, licenses,
a module manifest, build metadata and SHA256SUMS. Builds also produce a versioned
`.tar.gz` (Linux/macOS) or `.zip` (Windows) and an adjacent `.sha256`. A Bash wrapper, `./scripts/build-host.sh <os> <arch> [version]`,
is also available. Preserve the entire distribution when sharing binaries.

Put the binary in a fixed location on the remote user's PATH, then use
`nimote-host install` to start a per-user background service and wait until ready, or `nimote-host start`
for foreground operation. This does not configure SSH or install Agent CLIs.
Tasks outlive client disconnection; Host/OS restart marks unfinished sessions
interrupted and does not resurrect their processes.

## Desktop edition (Fyne)

The CLI and desktop manager share the existing Go Host and authenticated loopback
RPC. Desktop dependencies live in `cmd/nimote-host-gui/go.mod`; a CLI build does
not compile or download Fyne. No mobile protocol or public transport is changed.

```sh
# Native desktop SDK / C compiler required; also builds the CLI distribution
go run ./tools/build -gui -version 1.0.0-dev
# Or: make desktop
```

Downloads are named `nimote-host-<version>-<os>-<arch>` (CLI) and
`nimote-host-gui-<version>-<os>-<arch>` (desktop, including CLI). Desktop archives
include dependency licenses, GUI-MODULES.json and checksums. Run the existing
installer from the extracted desktop bundle to copy the complete bundle to a stable
path and start the login service. Then open the installed `nimote-host-gui.exe`
(Windows), `Nimote Host.app` (macOS), or `nimote-host-gui` (Linux).
Unix installation: `~/.local/share/nimote-host`; Windows: `%LOCALAPPDATA%/NimoteHost`.
The Linux `.desktop` file assumes the installed GUI directory is on PATH; copy it
to `~/.local/share/applications` or set its Exec to the absolute GUI path.

You can also run directly from `dist/gui-<os>-<arch>`; keep that directory stable
when enabling login startup. Start attaches to a running Host; otherwise it uses
the installed service or starts an independent background CLI. Closing/quitting
GUI keeps Host and its sessions alive. Stop/restart explicitly ends Host tasks.
Linux defaults to normal window close because trays are desktop-dependent; enable
tray use in Settings only with a working tray. Windows/macOS close hides to tray.

Settings share `config.json` in the existing OS user configuration directory under
`nimote-host`. Defaults remain port 47831 and log level info; changes require Host
stopped. The port always binds 127.0.0.1, **not LAN IPs**. SSH remains externally
managed. Runtime logs are owner-private and bounded at 64 KiB; diagnostic exports
exclude logs, credentials, command arguments and session contents. GUI preferences
(theme, Linux tray) are stored separately by Fyne.

Sessions and structured Agent approvals use live existing RPC data. Devices explains
the current SSH-account identity boundary: per-device identity/revocation is not
implemented. SSH health is reported as externally managed rather than guessed.
See [desktop audit, build prerequisites and limits](docs/completed.md#desktop-architecture-audit)
and [remaining acceptance](docs/pending.md).

## Interfaces

- Authenticated SSH exec: `nimote-host rpc` or `rpc-encoded`.
- Local authenticated HTTP RPC: task/session lifecycle, terminal input/resize,
  byte-offset output replay and persistent state events.
- Stdio MCP: read-only by default; writes require explicit local opt-in.
- Agent terminal adapters: installed Codex, Claude Code and OpenCode CLIs.
  Structured app-server/ACP approvals and native history are implemented; runtime
  capabilities and broader real-agent acceptance remain separate.

Read [implemented functionality and protocol](docs/completed.md),
[pending functionality and acceptance](docs/pending.md),
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
to this standalone Host source tree, extracted from `nimote-connect/host` at
Connect commit `4701d14`. Third-party components retain their own
licenses; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
