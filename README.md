# NimoteHost

**English** | [简体中文](README.zh-CN.md)

A standalone Go runtime for persistent tasks, terminals and installed coding
agent CLIs over an existing authenticated SSH connection. Host listens only on
loopback, authenticates every RPC request and requires no Nimote account,
control plane, Tailcat or public relay.

Host is a **development preview (`2.4.0`)**. It supports Linux/macOS/Windows
on amd64 and arm64, plus Linux/Windows 32-bit x86 (386) CLIs. Full native-platform and mobile/Agent acceptance remains
outstanding; see
[pending functionality](docs/pending.md).

Current functionality: [completed](docs/completed.md) and [pending](docs/pending.md).
Hardening decisions and verification: [audit](docs/host-hardening.md) and [test results](docs/host-hardening-tests.md).

## Agent upstream monitoring

The [compatibility workflow](.github/workflows/agent-compatibility.yml) checks upstream releases
every six hours (00:23, 06:23, 12:23 and 18:23 UTC). On its first run or a version change,
it temporarily installs exact versions of Codex, OpenCode, the Claude ACP adapter and Claude Code,
checks real structured session initialization, runs existing Host Agent/protocol/approval regressions, and exports
Codex JSON Schema. It sends no prompts and makes no model calls. Claude Agent SDK, ACP SDK,
and ACP Go SDK releases are also watched without automatically updating project dependencies.

Push to the private repository's default branch, enable Actions and Issues, and manually run
`Agent compatibility monitor` to establish a baseline. Subscribe to the issue it creates;
subsequent updates post comments, with email delivery controlled by your GitHub notification settings.
A failed version snapshot is retried once; use the manual `force` option for further retries.
Unchanged polls do not install CLIs or run tests. No extra secrets are needed: the workflow uses
the built-in `GITHUB_TOKEN`, with issue write permission limited to the reporting job.

A pass covers Linux startup and existing protocol fixtures; full approval, streaming and resume
acceptance still requires model tests. Failures can also be caused by installation, authentication
or networking. SDK latest versions are watch signals; checks use resolved adapter dependencies
and the repository's pinned Go SDK. Schema artifacts are exported without semantic diffing.
Logs and schemas expire after seven days. Scheduled runs can be delayed, and free usage is shared
across the account: configure an Actions budget with usage stopping in GitHub Billing to control
costs. The workflow cannot guarantee zero charges. Preserve the issue's hidden state markers.

## CPU architectures

| Platform | CLI | GUI |
| --- | --- | --- |
| Linux | amd64, arm64, 386 | amd64, arm64 |
| macOS | amd64, arm64 | amd64, arm64 |
| Windows | amd64, arm64, 386 | amd64, arm64 |

`amd64` means 64-bit Intel/AMD x86; `arm64` means 64-bit ARM (including Apple
Silicon). `386` is legacy 32-bit x86, CLI only. macOS has no 32-bit build target.
32-bit ARM is outside this distribution matrix. CLI and GUI in a bundle must match
the target architecture. GUI CI uses six native runners with the required desktop
SDKs; cross-building a CLI alone does not verify the GUI or interactive behavior.
On Windows ARM64, use MSYS2 CLANGARM64 (`CC=clang`, `CXX=clang++`), rather than the
x64 MinGW compiler. See [MSYS2 ARM64](https://www.msys2.org/docs/arm64/).

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
go run ./tools/build -os windows -arch arm64 -version 2.4.0
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
go run ./tools/build -gui -version 2.4.0
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
three native platforms and eight CLI cross-build targets. Tests do not install real
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

## Windows portable discovery

Run the CLI from any local directory accessible to the SSH user, including paths
with spaces or Unicode: `& 'D:\My Tools\nimotehost.exe' start` in PowerShell.
A GUI bundle must keep its GUI and CLI together; a standalone CLI executable does
not include the GUI and does not start the server when launched without arguments.
On successful Windows startup, Host publishes its absolute executable path in
`%APPDATA%\nimotehost\discovery.json`. Updated clients read this record over SSH
before trying `%LOCALAPPDATA%\NimoteHost\nimotehost.exe` and PATH. No PATH update
or SSH service restart is required. Restart from the new location after moving it.
The record remains when stopped; discovery does not start the service. Older clients
must be updated. Use the same Windows account for SSH and Host.

Installation is optional for portable use. For a fixed installation and login
startup, run `scripts\install.ps1` or `nimotehost setup` from the complete bundle.
Setup requires the complete distribution, not just an executable. Windows CLI targets
are amd64 (x64), arm64, and 386 (32-bit x86). GUI distribution targets remain amd64
and arm64 and require a native desktop toolchain.

## Community plugins

See the [plugin platform](docs/plugin-system/architecture.md),
[public HCP 1.0 specification](docs/plugin-system/spec/v1/README.md) and
[SDK / HTTP API Inspector guide](docs/plugin-system/plugin-sdk.md). Plugins are
disabled until explicitly approved; review the [security boundary](docs/plugin-system/security.md)
before running third-party executables.
