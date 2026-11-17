# Pending functionality

`2.4.0` · Updated: 2026-11-17

Implemented contracts live in [completed functionality](completed.md). Checked items are complete only within their stated implementation/evidence scope. Unchecked items include incomplete implementation, missing acceptance or required approval; partial completion does not close an entire feature.

## Features

- [x] Existing-service connections (2026-11-17): Codex control socket, OpenCode v2 HTTP, native pending, state calibration and reconnect. Real Codex CLI/local-model fixture passed; see [implementation](external-agent-sessions.md).
- [ ] Remaining external attachment acceptance: real OpenCode v1/v2, Claude tools/models, native Linux/Windows and phone/SSH matrices. OpenCode v1 and Windows read-only Codex socket code are implemented; Windows tests have not run natively. Independent Claude structured takeover and Antigravity remain unsupported. Terminal attach may restart stopped sessions; history resume is not takeover. See [tests](external-agent-sessions-tests.md).
- [x] Official Claude JSON discovery, isolated local Claude 2.1.311/ACP 0.107.3 initialization and explicit ACP v2 history replay. Official `claude attach` is a terminal entrypoint that may restart sessions, not a safe structured takeover handshake; this refines the earlier local-entrypoint limitation.
- [ ] Unified AI-mode Session dashboard: cross-host/workspace aggregation, search/filtering and timestamped offline summaries without presenting cached state as live.
- [x] Linux full-control device isolation: opt-in root/private finite cgroup v2/per-session UID backend and native WSL acceptance. Administrators reserve the UID pool across Hosts; Agent accounts/credentials require independent provisioning. This is not a general container/network sandbox.
- [ ] macOS/Windows full-control isolation: no independent execution-identity backend; full-control grants are rejected.
- [x] New request keys: seven-day retry window, safe online rotation and capacity warnings.
- [ ] Legacy key migration: old receipts cannot safely expire; capacity exhaustion rejects new operations. Do not delete receipts to enable delayed replay. Client migration and explicit protocol retirement remain required.
- [ ] Same-name Session wrappers: session.list/start/attach/detach/peek/send/watch/stop/events map to existing REST/RPC/subscriptions, without a complete identically named JSON-RPC family.
- [ ] Broader Task/Timeline integration: existing references, reattachment and lifecycle summaries do not import all external terminals, merge full native conversations or replace TaskService execution.
- [ ] Live sessions surviving Host lifecycle: Host/OS restart does not restore processes. tmux/psmux extensions need ownership, cursor, cancel and platform validation; platform names do not prove recovery.
- [ ] Background/offline push: observation currently requires foreground, a connected Host and LAN reachability. Android/iOS background policies and permissions need evaluation; offline delivery is not guaranteed.

## Release acceptance

- [ ] Full Android/iOS device × Linux/macOS/Windows Host × Codex/Claude/OpenCode matrix: real model coding, same-conversation control, approve/decline, timeout/repeated requests, Git diff review and continuation.
- [ ] Foreground/background, disconnect/reconnect, multiple Hosts/Agents, observation/input scope, expired retention windows, timeout cancellation, unreachable/mismatched protocols and older Host fallback.
- [ ] Native three-platform service, reboot, login/logout and long-running operation, explicitly verifying that live processes are not restored. Cross-compilation is insufficient.
- [ ] Existing SSH/SFTP, Terminal, IDE, AI, Git, Tasks and Timeline with Host absent, installed, disconnected and removed.
- [ ] Native scoped SSH and optional go-task/just integration currently skipped for missing environment prerequisites.
- [ ] Native CI, installation/upgrade/downgrade and matched app/Host distribution before promoting development preview to release. The umbrella `agent_structured_approval` flag stays conservative until broad real approval acceptance; source or fake-peer tests alone are insufficient.

Removed historical reports do not establish current release acceptance. Raw private audit evidence has been purged from the public history; maintained test records state the supported evidence.

## Desktop follow-up

- [ ] Clean-system installation/upgrade/restart/uninstall on candidate macOS 13+, Windows 11 and Ubuntu 22.04 across architectures. Pin the release Go version before publishing minimum requirements. The Linux glibc 2.35 ceiling does not validate GPU drivers, desktop libraries or other distributions.
- [ ] Real observer-device connection telemetry, automatic SSH key installation/removal and Windows forced-command acceptance. Registration/list/revocation/GUI exist; administrators still bind keys, and connection state is explicitly unknown.
- [ ] Real tray/window/theme/login/reboot services on all three desktops. Linux no-tray fallback exists, but Fyne's test driver does not prove tray support.
- [ ] Execute the configured native GUI CI targets. Windows/Linux ARM64 runtime acceptance remains pending; Go cross-build or local macOS results do not substitute. MSI, macOS signing/notarization and Linux system packages need further evaluation.
- [ ] Desktop SSH connection history, background version-check policy and automatic installation. Native history pages/transcripts/original-ID resume and explicit official version/download links exist. Codex transcript pagination is implemented; ACP truncation is disclosed, with upstream resumable history, real-provider acceptance and package signatures still needed.
- [ ] Real NimoteCode phone connection using default/custom ports, existing SSH RPC, and Host absent/stopped/running. Repository tests are not mobile-device acceptance.

## Remaining hardening gates

### Approval boundaries

Machine reboot requires separate explicit prior approval. Access to an environment or `env.md` is not reboot authorization. Login/logout, production SSH/service/firewall changes, system installations, mounts and filesystem quotas require a concrete impact window and recovery plan before approval. Use private temporary directories, independent ports/keys/processes for reversible validation; missing conditions remain open.

- [ ] Independent full-control execution identities for macOS/Windows. `session_operator` remains stop/decline only.
- [x] Linux CPU hard quotas: existing delegated Ubuntu WSL `cpu.max` and kernel throttling validated. Undelegated configurations reject; no global controllers are enabled automatically.
- [x] Linux memory hard quotas: touching 128 MiB succeeded under 512 MiB and was kernel-killed under 64 MiB; native CPU/memory race acceptance passed.
- [ ] Native hard CPU/memory/process quotas on macOS.
- [ ] Workspace disk hard quotas: current free-space/output/event/ledger/session bounds do not limit arbitrary workspace writes. Untrusted work requires a dedicated volume or administrator filesystem quotas.
- [x] Unix supervisor cleanup after Host SIGKILL for ordinary process groups, and Linux cgroup revocation of setsid descendants.
- [ ] Cleanup of intentionally escaped groups without cgroups. No transaction guarantee if the supervisor is killed, the kernel crashes or power fails.
- [ ] Independent non-WSL Linux operational acceptance, native Windows/macOS reboot/login/logout/long workloads and upgrade/downgrade. Native syscall tests do not establish these scenarios.
- [x] Native WSL SSH end to end: existing OpenSSH, independent loopback port/temp keys, forced command, Session scope, shell/SFTP/forwarding/PTY rejection and revocation passed native race acceptance. Existing services/authorized_keys were not modified.
- [ ] Native Windows/macOS SSH: temporary Windows sshd authentication failed in an administrator context without reported SeAssignPrimaryTokenPrivilege. A separately approved service identity is needed; the failure is not a pass.
- [x] macOS owner Codex/Claude/OpenCode with deepseek-flash: real approve/decline/dedup/resume; versions are in the test record.
- [x] Linux dedicated UID/session_controller with Codex 0.169.0: real approve/decline/dedup and fresh-UID resume passed native race tests; anonymous stream socketpair compatibility and negative kernel tests passed.
- [ ] Wider real Agent/model/platform matrix. Linux Claude/OpenCode runtimes and isolated profiles were not supplied/accepted. Full ACP v2 message upsert, background work, terminal display and complex recovery need targeted conformance tests.
- [ ] Independent security review, sustained saturation and fault injection. Generic non-closeable `io.Writer` cannot be forcibly unblocked. Linux identity isolation protects private Host/session files, not world-readable files/IP networking/kernel vulnerabilities or another root administrator's misconfiguration.
- [x] Per-device fair admission, bounded connections, explicit backed-up repair and recovery-watermark regressions.
- [x] Native Windows Job CPU/committed-memory/process limits, allocation refusal and lifecycle.

Checked items are bounded by their stated evidence. See [hardening contract/rollback](host-hardening.md) and [actual test limits](host-hardening-tests.md). Remaining items prevent claiming all P0/P1 release goals are closed.

## Missing prerequisites and deferred approval

| Item | Condition | Execution boundary |
| --- | --- | --- |
| Any reboot/login/logout | Explicit approval and interruption window | Identify machine, service/session effects and recovery access first; no reboot performed |
| Windows scoped SSH | Administrator temporary-daemon acceptance failed | After approval, a temporary SYSTEM service may use loopback-only private port/keys/config and be removed afterward. Do not alter production sshd/login rights to bypass prerequisites |
| macOS scoped SSH | Approved sshd test environment absent | Do not enable Remote Login or edit user authorized_keys without authorization |
| Long workloads and independent Linux operations | Dedicated isolated host/window absent | Short fault/concurrency regressions are insufficient |
| Disk quotas | Backend unimplemented and no designated test volume | Select project/volume policy first; mounts/quota enablement await approval |
| macOS hard quotas and macOS/Windows identities | Backends absent; currently reject | Verify OS identity/container/permission design before approved account/AppContainer/ACL changes |
| Linux Claude/OpenCode isolation | Selected WSL lacks supported CLI/ACP/Node runtime | Supply versions and isolated credentials; no automatic system installation |

These items are not accepted risks or release approvals. Completed behavior and commands are recorded in [completed functionality](completed.md) and [tests](host-hardening-tests.md).

## Agent workflow acceptance (2026-11-14)

Host and sibling NimoteCode integration are described in [completed functionality](completed.md). Remaining acceptance:

- [ ] Real phone/tablet SSH, foreground/background and network recovery. ADB found no devices during delivery.
- [ ] An approved real test Issue, installed/authenticated gh account and configured real Agent completing development, tests and PR creation. No test Issue was supplied and gh was absent locally; Git/protocol fixtures are not a real closed loop.
- [ ] Installed Codex, Claude/ACP and OpenCode history/continuation, real approvals and attributable edit events. Discovery/fixtures alone do not establish runtime support.
- [ ] Native Windows/Linux execution of new workflow behavior; compilation is insufficient.
- [ ] Cross-host aggregation, semantic duplicate-work detection, identity-isolated verification execution, larger evidence retention and advanced fork/Enterprise publication remain outside this version.
