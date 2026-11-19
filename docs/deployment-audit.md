# NimoteHost deployment audit

The review covered standalone NimoteHost at commit `78790cf`, its repository configuration, and its public GitHub Releases. The implementation scope is local-network SSH deployment, native service management, health, updates and removal.

| Capability | Before this change | Code | Gap | Treatment |
| --- | --- | --- | --- | --- |
| Installation | Both bundle wrappers run non-interactive `setup`. Source installation builds with Go first. Setup stages a full release at a fixed user path | `scripts/install.sh`, `scripts/install.ps1`, `tools/install/main.go`, `cmd/nimotehost/setup.go` | Callers select and fetch release assets themselves; setup prints prose | Add a versioned JSON deployment interface around setup |
| Background service | Linux systemd user unit and macOS GUI LaunchAgent start on login and restart on failure. Windows uses a least-privilege interactive scheduled task | `internal/hostservice/service.go`, `host_windows_task.go` | macOS and Windows require a user login. Linux lifetime depends on its user manager. Windows lacked a failure restart policy | Keep user scope and add Windows failure retries |
| GitHub Releases | Tags publish eight CLI archives and six GUI archives in separate jobs. Existing filenames contain edition, version, OS and architecture. Source is exported independently | `.github/workflows/ci.yml`, `tools/build/main.go`, `archive.go`, `gui.go` | Sidecar SHA-256 and internal SHA256SUMS cannot independently identify the release source. GUI assets can arrive after CLI assets | Retain archive names; use GitHub API asset digests for automatic download validation and add bootstrap assets |
| Version and health | `--version`, JSON `status`, JSON `capabilities` and explicit GUI release metadata lookup exist | `cmd/nimotehost/main.go`, `internal/host/server.go`, `internal/desktop/releases.go` | Existing status reports a running daemon only | Add machine-readable offline installation and authenticated readiness probes |
| SSH and devices | Existing SSH OS account owns the Host. Restricted SSH device credentials can be listed or revoked. Standalone Host has no Firebase, P2P or Nimote Connect control plane | `cmd/nimotehost/devices.go`, `internal/host/devices.go`, `docs/client-api.md`, `SECURITY.md` | Scope is satisfied by existing transports | Reuse existing SSH and RPC |
| Update | Setup stages a verified bundle, locks the destination, requests idle shutdown, switches directories, and rolls back on startup failure. Legacy migration preserves state | `cmd/nimotehost/setup.go`, `migration.go`, `internal/hostinstall/transaction.go` | No automatic stable release lookup or preflight protocol range. Windows cannot reliably replace a currently executing binary | Fetch and validate before setup; run updates from outside the target |
| Removal | Existing CLI uninstall stops Host and removes the native definition, keeping state and program files | `cmd/nimotehost/service.go`, `internal/hostservice` | It can terminate active tasks and offers no packaged uninstall | Add guarded, idle-only deployment uninstall with explicit data purge |

The Go CLI is cross-built for Linux, Windows and macOS on amd64 and arm64. Linux and Windows also have 386 CLI builds. The desktop command is a separate Go/Fyne module using native SDKs. Its Linux build targets glibc 2.35 and its macOS archive contains an app bundle. Windows uses ZIP and Unix uses tar.gz.

The CLI launches the Host server, which owns tasks, terminals, agent sessions and plugins. The desktop manager controls the same Host via `internal/desktop`. The server listens on IPv4 loopback with default port 47831. NimoteCode connects using the existing SSH account and stdio bridge. The bridge reads the owner credential locally. One-shot RPC, legacy encoded RPC, and multiplexed JSONL RPC remain available. Host HTTP, stream framing and plugin protocols have separate versions. Host HTTP currently supports protocol 1.

Program files go under `~/.local/share/nimotehost` on Unix and `%LOCALAPPDATA%\NimoteHost` on Windows. Configuration, credentials, task records and runtime logs live under `nimotehost` in the user configuration directory. Existing code applies private Unix modes or checks Windows ACLs. Native service definitions include a limited environment. Unix setup requires a non-root account.

See [GitHub Releases](https://github.com/mobiledevloperlab/nimotehost/releases), [GitHub Release assets](https://docs.github.com/en/rest/releases/assets#get-a-release-asset), [client protocol](client-api.md), [security](../SECURITY.md) and [deployment instructions](deployment.md).

## Files changed

| Area | Files |
| --- | --- |
| Deployment command and platform detection | `cmd/nimotehost/deploy.go`, `deploy_release.go`, `deploy_arch_darwin.go`, `deploy_arch_linux.go`, `deploy_arch_other.go`, `deploy_arch_windows.go`, `deploy_test.go`, `main.go`, `service.go`, `setup.go` |
| Release verification and bundle transactions | `internal/hostinstall/release.go`, `extract.go`, `bundle.go`, `transaction.go`, `release_test.go`, `transaction_test.go` |
| Native service management | `internal/hostservice/service.go`, `host_windows_task.go`, `host_test.go` |
| Build and test automation | `.github/workflows/ci.yml`, `tools/build/main.go`, `tools/build/docs_test.go`, `scripts/test-deployment-linux.sh` |
| Documentation | `README.md`, `README.zh-CN.md`, `docs/deployment.md`, `docs/deployment-audit.md` |

Local validation used macOS ARM64 for deployment tests, cross-builds for eight CLI targets, and the production packager for a macOS ARM64 tar.gz and a Windows ARM64 ZIP. The Linux CI job exercises native systemd and localhost SSH on AMD64 and ARM64. CI execution, native Windows Task Scheduler and macOS LaunchAgent login/logout/reboot behavior require their respective environments. See [deployment instructions](deployment.md) for the service lifetime assumptions.
