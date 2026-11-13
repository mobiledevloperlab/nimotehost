# Deploy Nimote Host

**English** | [简体中文](deployment.zh-CN.md)

The bundled installer performs **verify → install at a fixed user path → register
the background service → check readiness** with one command. It needs no admin
privileges, Nimote account or additional public port.

## One-command setup

Prebuilt packages do not need Go. Obtain a complete distribution for your OS and
architecture from a trusted channel, verify its external SHA256 checksum, then
extract it. From the extracted directory, run as your normal SSH user:

```sh
# Linux / macOS
sh scripts/install.sh
```

```powershell
# Windows PowerShell
.\scripts\install.ps1
```

Alternatively, run `./nimote-host setup` or `.\nimote-host.exe setup`. Installation
does not download remote scripts, run sudo or change PowerShell execution policy.
If policy blocks scripts, invoke `nimote-host.exe setup` directly and follow your
organization's execution policy.

Source users run the same command from the **Host module root**. The script builds
a complete distribution first; this requires Go 1.26.5 or later and cached or
downloadable module dependencies. Run all source commands from the standalone Nimote Host repository root.
A portable source entrypoint is `go run ./tools/install`; this does not depend
on script execution policy or a Connect checkout.

Success prints the **absolute binary path** for client settings. Use your existing
SSH address, port, account and that path. Host does not configure SSH or install or
log in to third-party Agents. Confirm that the client can already SSH into this
machine through your existing network/VPN.

Default destinations:

| OS | Installation directory | Background service |
| --- | --- | --- |
| Linux | `~/.local/share/nimote-host` | Current-user `nimote-host.service` |
| macOS | `~/.local/share/nimote-host` | `com.nimote.host` LaunchAgent |
| Windows | `%LOCALAPPDATA%\NimoteHost` | Current-user `NimoteHost` logon task |

Use the printed absolute binary path with `status` or `capabilities` to check it.
On Unix accounts using zsh or bash, setup also creates `~/.local/bin/nimote-host`
and adds that directory to the shell startup PATH used by SSH exec. Existing
commands are never replaced. Reconnect SSH after setup to refresh any cached
environment snapshot. Cross-platform clients should use the fixed installation
path directly (`$HOME/.local/share/nimote-host/nimote-host` on Linux/macOS,
`%LOCALAPPDATA%\NimoteHost\nimote-host.exe` on Windows) and use PATH discovery
only for custom installations.

## Repetition, updates and rollback

- Repeating setup with an identical, verified installed bundle and a running
  service returns ready without restarting or interrupting tasks.
- An update verifies and stages the complete new bundle before requesting an
  **idle-only shutdown**. Active processes block the update. Checking for active
  processes and closing admission to new sessions are atomic.
- An older development Host without `idle_shutdown` requires a one-time `stop`
  after its tasks finish, followed by the new installer. Existing service paths
  must use the default destination above; for a manually installed service at
  another path, uninstall its service definition first, retaining its state.
- Installation directories switch by same-filesystem rename. The old bundle is
  kept beside the destination in `.nimote-host-previous-*`. Startup failure attempts
  to restore that bundle and its former running state; recovery errors are reported.
  Failed first installs remove the new service definition and retain credentials
  and records. Failed bundles remain in `.nimote-host-failed-*` directories.
- Setup does not automatically downgrade or restore private state. This version
  makes no state-format migration. Future migrations must follow their release's
  compatibility and backup requirements. Updates do not restore task processes.

A sibling `nimote-host.setup-lock` directory prevents concurrent setup. Forced
termination may leave a lock, staging directory or version backup. Confirm that
no setup process is active before inspecting or removing leftovers. Retain old
bundles for [manual rollback](maintenance.md), then remove them when no longer needed.

## Downloads and verification

Versioned packages can be obtained from
[Releases](https://github.com/mobiledevloperlab/nimote-host/releases) once published.
This document does not imply an existing release; use complete local builds or
maintainer-supplied CI packages during development.

Supported targets are Linux, macOS (`darwin`) and Windows on amd64/arm64. Windows
terminals require Windows 10 1809+. Archives are
`nimote-host-<version>-<os>-<arch>.tar.gz`, or `.zip` on Windows. They retain licenses,
upstream notices, module inventory, build information, bilingual docs and installers.

Linux example (replace version/architecture with the actual download):

```sh
bundle=nimote-host-1.0.0-dev-linux-amd64.tar.gz
sha256sum -c "$bundle.sha256"
# Only extract after verification succeeds, into a newly created empty directory.
```

On macOS use `shasum -a 256 -c "$bundle.sha256"`. Windows example:

```powershell
$bundle = 'nimote-host-1.0.0-dev-windows-amd64.zip'
$expected = ((Get-Content "$bundle.sha256") -split '\s+')[0]
if ((Get-FileHash $bundle -Algorithm SHA256).Hash -ne $expected) {
    throw 'Archive checksum mismatch'
}
Expand-Archive -LiteralPath $bundle -DestinationPath '.\nimote-host-package'
```

Checksums detect changes, not publisher identity. Trust the download channel and
checksum information. Current packages are not signed/notarized. Setup verifies
all extracted manifest files again and rejects missing/extra files, symlinks and
unsafe paths. It copies no credentials, source workspace or Git history.

## Environment and troubleshooting

Linux requires a systemd user manager. To continue after logout, check
`loginctl show-user "$USER" -p Linger`; an administrator can enable lingering under
local policy with `loginctl enable-linger "$USER"`. Under WSL, Host is available
only while the distribution runs. macOS requires a GUI login session. Windows
uses LeastPrivilege and requires a logged-in user; this is not a LocalSystem service.

Linux/macOS installation saves absolute PATH entries and XDG_CONFIG_HOME, without
other environment variables or credentials. Install from a normal user shell and
avoid temporary tool directories. To refresh tool paths, let tasks finish, run
`stop`, then `install` at the fixed binary path. `restart` does not recapture the
environment. Windows uses the login environment. Install and authenticate Agents
beforehand as the same OS user.

Checks (replace `nimote-host` with the printed absolute path):

```sh
nimote-host status
nimote-host capabilities
# Linux
systemctl --user status nimote-host.service
journalctl --user -u nimote-host.service -n 50 --no-pager
# macOS
launchctl print "gui/$(id -u)/com.nimote.host"
```

Windows: `schtasks.exe /Query /TN NimoteHost /V /FO LIST`. Frequent Linux starts may
hit systemd's start limit. Resolve the logged failure first, then run
`systemctl --user reset-failed nimote-host.service` before retrying.

Windows and WSL mirrored networking can retain ownership of this fixed loopback
port briefly after the other platform stops. If binding fails although no listener
is listed, stop the other Host, allow the shared port to be released, and retry.
Do not run both platform Hosts on the shared port simultaneously. After a failed
Linux start, also clear the native start limit as described above.

Without a service manager, use foreground `start`. Do not run two instances.
Stop/restart/uninstall terminate active tasks. Do not remove runtime locks, signal
stale PIDs or expose port 47831 publicly. For Linux overrides, prefer
`systemctl --user edit nimote-host.service`: drop-ins survive reinstall, while
manual edits to the generated main definition are overwritten.

## Client verification and removal

From another device, reuse your account, SSH port and actual machine address:

```sh
ssh -p 2222 user@host '"$HOME/.local/share/nimote-host/nimote-host" capabilities'
```

Check Host ID, protocol and tool paths. The remote CLI reads its local token; do
not transfer it to mobile settings. Deployment does not add mobile UI; clients
must implement the [RPC/MCP protocol](host.en.md). Verify disconnection and
reconnection in the actual client before relying on it.

Run `uninstall` at the fixed binary path, then optionally remove its directory
and version backups. Credentials, identity, task records and output remain in
`os.UserConfigDir()/nimote-host`. Deleting state is a separate deliberate action;
ordinary updates and uninstall retain it.
