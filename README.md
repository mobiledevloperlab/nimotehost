# NimoteHost

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
`dist/<os>-<arch>/nimotehost` (`nimotehost.exe` on Windows):

```sh
./dist/linux-amd64/nimotehost start
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
`nimotehost install` to start a per-user background service and wait until ready, or `nimotehost start`
for foreground operation. This does not configure SSH or install Agent CLIs.
Tasks outlive client disconnection; Host/OS restart marks unfinished sessions
interrupted and does not resurrect their processes.

## Desktop edition (Fyne)

The public executable and package name is now `nimotehost` (`NimoteHost` in the GUI).
Close the previous GUI before running `nimotehost setup` from a complete bundle.
Setup migrates the previous state directory only after an authenticated idle shutdown;
active sessions and conflicting state directories block migration. Credentials, Host
identity and records are retained, and failed installation restores the previous service.
The GUI imports the previous language/theme preferences once. On Unix the old SSH
command and absolute executable path forward to the new CLI, with the old executable
retained for recovery. On Windows, update any manually configured absolute SSH forced
command to `nimotehost.exe`. Only upgrade compatibility retains the old spelling.

The CLI and desktop manager share the existing Go Host and authenticated loopback
RPC. Desktop dependencies live in `cmd/nimotehost-gui/go.mod`; a CLI build does
not compile or download Fyne. No mobile protocol or public transport is changed.

```sh
# Native desktop SDK / C compiler required; also builds the CLI distribution
go run ./tools/build -gui -version 1.0.0-dev
# Or: make desktop
```

Downloads are named `nimotehost-<version>-<os>-<arch>` (CLI) and
`nimotehost-gui-<version>-<os>-<arch>` (desktop, including CLI). Desktop archives
include dependency licenses, GUI-MODULES.json and checksums. Run the existing
installer from the extracted desktop bundle to copy the complete bundle to a stable
path and start the login service. Then open the installed `nimotehost-gui.exe`
(Windows), `NimoteHost.app` (macOS), or `nimotehost-gui` (Linux).
Unix installation: `~/.local/share/nimotehost`; Windows: `%LOCALAPPDATA%/NimoteHost`.
The Linux `.desktop` file assumes the installed GUI directory is on PATH; copy it
to `~/.local/share/applications` or set its Exec to the absolute GUI path.

You can also run directly from `dist/gui-<os>-<arch>`; keep that directory stable
when enabling login startup. Start attaches to a running Host; otherwise it uses
the installed service or starts an independent background CLI. Closing/quitting
GUI keeps Host and its sessions alive. Stop/restart explicitly ends Host tasks.
Linux defaults to normal window close because trays are desktop-dependent; enable
tray use in Settings only with a working tray. Windows/macOS close hides to tray.

The four main pages are Overview, Sessions, Devices and Settings. Overview shows
service status and one primary action; Sessions uses readable names, search and
status filters, with saved conversations under Past conversations. Settings groups
general preferences, local service and about information. Local service controls
include start/stop, port validation, unsaved change status and a fixed save bar.
Silver-gray light and dark appearances use consistent rounded surfaces and spacing.
Changing the display language preserves configuration drafts and session filters.
Conversations can expand into a reading view with selectable text and copying;
diagnostics and logs have large tabbed viewports in Help & diagnostics. About
explains the NimoteCode companion workflow, local data, access and agent privacy,
and appearance. Shortcuts use Cmd on macOS or Ctrl on Windows/Linux: 1–4 switch
pages, comma opens Settings, F searches sessions, and R refreshes status. Command and file-change approvals still show their actual
content before you decide.

Choose English, 简体中文, Español, 日本語, 한국어 or Русский in Settings → Language.
English is the default; language changes apply immediately and persist without
restarting Host. Fyne uses installed system fonts; Linux needs suitable CJK fonts
(such as `fonts-noto-cjk`) for Chinese, Japanese and Korean.

Settings share `config.json` in the existing OS user configuration directory under
`nimotehost`. Defaults remain port 47831 and log level info; changes require Host
stopped. The port always binds 127.0.0.1, **not LAN IPs**. SSH remains externally
managed. Runtime logs are owner-private and bounded at 64 KiB; diagnostic exports
exclude logs, credentials, command arguments and session contents. GUI preferences
(language, theme, Linux tray) are stored separately by Fyne.

Sessions and structured Agent approvals use live existing RPC data. Devices manages restricted read-only identities and revocation; unrestricted
SSH accounts remain outside device isolation. SSH health is reported as externally managed rather than guessed.
See [desktop audit, build prerequisites and limits](docs/completed.md#desktop-architecture-audit)
and [remaining acceptance](docs/pending.md).

## Interfaces

- Authenticated SSH exec: `nimotehost rpc` or `rpc-encoded`.
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
the versioned RPC/MCP protocol. Module path: `github.com/mobiledevloperlab/nimotehost`.

## License

[Apache-2.0](LICENSE), copyright 2026 Nimote contributors. The license applies
to this standalone Host source tree, extracted from `nimote-connect/host` at
Connect commit `4701d14`. Third-party components retain their own
licenses; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## Device authorization / 设备授权补充

GUI Devices 和 `nimotehost devices add <name>` 可注册独立只读设备，
`nimotehost devices list` 列出设备，`nimotehost devices revoke <id>` 撤销。
管理员须把独立 SSH 公钥绑定到 `restrict` + `device-bridge` 强制命令；
不能同时给这把密钥普通 Shell 登录权限。凭证不发送给手机，撤销针对后续 Host 请求生效。
完整 SSH/任务执行权限仍属于系统账号，不受只读设备撤销隔离。
Observer identities use restricted SSH keys, not unrestricted same-user shell access.
设备注册及绑定方法、安全边界见 [设备授权说明](docs/completed.md#只读设备授权历史与更新入口--observer-devices-history-and-updates)。
GUI History 已接入原生历史查询/恢复；Settings 可查询最新官方版本并打开下载入口。
