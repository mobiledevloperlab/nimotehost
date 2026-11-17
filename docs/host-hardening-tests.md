# Host hardening test record

2026-11-14 · Recorded execution, not a zero-bug guarantee or release certification.

## Environments

- Native macOS Darwin 25.2.0 arm64, Go 1.27.4.
- Native Windows amd64 Win32/ConPTY/Job tests through the supplied SSH environment: Go 1.27.4, Strawberry GCC, Windows 11 Pro for Workstations 10.0.26200 (Build 26200).
- Native Linux syscalls in RobotOS WSL2 on that Windows host: kernel 6.18.58.1-microsoft-standard-WSL2, Go 1.27.4/GCC. This is not independent Linux boot/service acceptance.
- Supplied Linux SSH authentication failed; no account, password or SSH service was changed. Authorized Windows-host WSL was used instead. All tests used dedicated temporary state, ports and workspaces.
- Windows dependency downloads timed out. A temporary file GOPROXY used 15 repository-pinned public module versions, retaining go.sum, verification and `go mod verify`. Temporary official Go SDK downloads were checked against official SHA256 lists.
- Production dependencies/go.mod/go.sum were unchanged; minimum Go 1.26.5 was not run separately.

## Baseline commands and results

These results belong to the earlier `4fb0c6e` baseline. Follow-up production changes are recorded separately; baseline and cross-build evidence do not substitute for final native execution.

| Command/check | Recorded result |
| --- | --- |
| macOS `go test ./...`, final `go test -json ./...` | PASS: 381 test/subtest passes, 5 explicit environment skips, zero failures; not 381 independent end-to-end scenarios |
| macOS `make check` | PASS: 5 Python checks, module verify, readonly full race suite and vet; no race reported |
| macOS direct race/vet | PASS; final production changes also covered by make |
| Windows JSON tests/race/vet/`go run ./tools/build` | PASS: 321 tests/subtests, 13 platform/environment skips; race/vet/native distribution exit 0 |
| WSL Linux same four checks | PASS: 382 tests/subtests, 6 skips; race/vet/native distribution exit 0 |
| Linux `NIMOTE_TEST_ROOT_ISOLATION=1 <host-test> -test.run=^TestRootIsolatedExecutionBoundary$ -test.v` | PASS, 0.56s: root-owned public helper reentry, dedicated UID, actual kernel cgroup settings and revocation cleanup |
| Same root boundary with native race test binary | PASS, 4.59s; isolation, concurrent revocation and cleanup without reported races |
| Windows revocation/pinned-reader atomic replacement, `-test.count=30` | PASS: each repeated 30 times; old readers retain complete old snapshots while new paths expose complete new snapshots |
| macOS supervisor/SIGKILL/job-control/version probes, `-race -count=3` | PASS, 30.934s; real SIGKILL Host fixture and task/terminal descendants cleaned |
| ACP v1 resume/v2 lifecycle/permission subjects/initialization/version probes, `-race -count=3` | PASS after real-model fixes and new regressions |
| GUI readonly race suite with `-tags ci` | PASS, 24.519s; headless Fyne, UI unchanged; existing duplicate `-lobjc` warning retained |
| GUI vet with/without `-tags ci` | PASS |
| Six-target readonly test compilation | PASS: darwin/linux/windows × amd64/arm64, CGO disabled; additional targets compile only |
| Six-target `go run ./tools/build -os ... -arch ...` | PASS: existing distribution format, manifests and checksums |
| `sh scripts/check-clean-build.sh` | PASS: fresh module/build caches, readonly race/vet/native distribution all exit 0 |
| Source export/Python docs/export checks/diff/gofmt | PASS: environment, credentials and local task state excluded; 5 Python checks and Go documentation checks passed |

## Live Agent/model acceptance

`TestLiveAgentApprovalDeclineAndConversationResume` runs only with explicit `NIMOTE_TEST_LIVE_AGENT` and isolated CLI/model configuration. Default CI installs no accounts and makes no model calls. The baseline uses macOS owner sessions; Linux isolated-UID Codex is separate below. Model calls used temporary workspaces, random nonsensitive markers and supplied deepseek-flash configuration. Keys entered child environments only; logs were redacted. Profiles, credentials, model history and env.md were not committed.

| Provider/version | Result |
| --- | --- |
| Codex CLI 0.167.2, Responses API, read-only/on-request | PASS, 14.49s |
| OpenCode 1.19.5, isolated HOME/XDG, all tools ask | PASS, 8.35s |
| Claude Code 2.1.268 + `@agentclientprotocol/claude-agent-acp` 0.107.0, isolated CLAUDE_CONFIG_DIR, Write/Edit/Bash ask | PASS, 10.21s |

Each provider actually requested approval to write approved.txt, repeated the same approval HTTP request ID with only one native reply, canceled/resumed the same conversation ID and reproduced the random marker without reading the previous file, then declined denied.txt and verified absence. Both approve and decline traversed a genuine native permission request; automatic allow was not counted.

CLIs lived in temporary tool directories, without global or production auto-installation. Old `@zed-industries` 0.23.1 was rejected by existing major-version checks. A new optional native CLI download timed out; acceptance explicitly reused a verified installed Claude executable rather than broken downloads. Temporary npm relinking interrupted OpenCode startup; restoring the same verified native version and rerunning passed. These environmental failures were not counted as product successes.

Configuration references: [DeepSeek Codex](https://api-docs.deepseek.com/quick_start/agent_integrations/codex/), [Claude](https://api-docs.deepseek.com/quick_start/agent_integrations/claude_code/), [OpenCode](https://api-docs.deepseek.com/quick_start/agent_integrations/opencode/), [ACP v1 session setup](https://agentclientprotocol.com/protocol/v1/session-setup), [v2 migration](https://agentclientprotocol.com/protocol/v2/migration).

## Regression coverage

| Files/area | Assertions |
| --- | --- |
| hardening_devices_test.go, devices_test.go | Per-request revocation, concurrent auth, scope filtering, cross-session rejection, principal receipt isolation, restart dedup, oversized/corrupt records, preserved observer/operator semantics |
| execution_identity_linux_test.go | Real dedicated UID/GID, no supplementary groups/owner environment, no_new_privs, Host token/process denial, blocked AF_UNIX broker/io_uring bypass with IP allowed, finite private delegation/protected helper, memory/swap/task kernel settings, two-device isolation, revoked receipt refusal, setsid cleanup without stopping the other device, no identity regrant |
| hardening_approvals_test.go, agent_execution_policy_test.go, acp_agent_test.go | Thread/turn/session context, one-time decisions, concurrent duplicate approval, stale accepts after turn end, ACP empty-object resume/changed-ID/null handling, insertion ACK not completion, foreign/unknown states, duplicate idle, conflicting/unknown permission subjects |
| admission_test.go | Device 8-read/2-write limits, concurrent acquire/release, owner reserve, connection cap, closing wakes blocked Accept |
| hardening_resources_test.go, resource_quota_windows_test.go | Admission/slot release, disk/output/runtime failures, no unconstrained fallback; native Win32 CPU/memory/process queries, 512 MiB VirtualAlloc succeeds unconstrained and fails under 256 MiB Job |
| supervisor_unix_test.go, hardening_process_unix_test.go, native PTY/ConPTY | Host SIGKILL/root exit, descendant heartbeats, exit codes, private-entry denial, interactive job control/reconnect, interrupted recovery without guessed PID ownership |
| recovery_test.go | Stopped-Host locks, stale-plan rejection, corrupt metadata/events backup/quarantine, healthy session/receipt/output/activity retention, unknown-file refusal, monotonic reset and cursor resync |
| storage_windows_test.go, config_test.go, migration_test.go, structured_windows_test.go | Pinned readers/atomic replacement, locks across migration/rollback, bounded logs, original migration state, full Unicode/multiline paging and stderr separation |
| Existing history/SDK/ledger/output/events/service/install/desktop suites | Cancel/timeout/recovery, receipt rotation/retention, GUI/CLI daemon lifecycle; production UI unchanged |

Disk faults were injected without filling the development disk. Linux aggregate cgroup tests touched only dedicated child trees, not global controllers/accounts/services. RobotOS lacked CPU delegation; later Ubuntu acceptance below tested actual throttling. Linux baseline checked kernel memory configuration, not a complete OOM matrix. Windows tested actual committed-allocation refusal.

## Diagnosed failures and fixes

1. Windows empty cmd.Dir became an invalid CreateProcess directory. Nil now inherits cwd; nil Env follows exec.Cmd inheritance. Native Job/allocation tests passed.
2. Append-only Windows log handles could not truncate. RunLog seeks under its mutex before writing; capacity assertions remain.
3. Directory migration was blocked by child handles despite share-delete. Management locks moved to the parent with normalized legacy/current names. Original-state/rollback and locked-rename tests passed; old managers must close before upgrade.
4. Windows read/ACL handles raced replacement. Protected share-delete readers and FileRenameInfoEx POSIX publication use bounded legacy fallback. A missing terminal UTF-16 NUL produced intermittent ERROR_INVALID_NAME; keeping NUL with a length excluding it passed 30 repeats.
5. The Windows large-output test read only the first 64 KiB page, missing completion. It now accumulates offsets through all pages, retaining Unicode/content/stderr assertions and the production page limit.
6. Race-instrumented macOS reexec cleanup consumed version-probe time. Target completion is reported separately from guardian reaping; one reaper always runs and Close joins it. The two-second target budget, timeout assertions and race diagnostics remain.
7. Real OpenCode exposed successful empty ACP resume acknowledgements. Requested ID is retained and another conversation is rejected.
8. Real Claude exposed ACP v2 initialization, insertion ACK, asynchronous state_update and permission-subject differences. Shared adaptation passed the real three-stage flow and race regressions.
9. Repair reset must include persisted event high-water marks. Clock rollback/old-high-water regressions ensure monotonic repaired cursors.
10. Test drivers used a cleared WSL /tmp path and misquoted dotted PowerShell arguments. Drivers were fixed and rerun; tests that never started were not passes. No failed assertion was removed, weakened or skipped.

## Explicit limits

The five default macOS test-level skips were Linux systemd parsing, three installed-provider opt-ins and real models. Explicit model runs above do not rely on skips. Linux's six skips were macOS zsh, three installed providers, root isolation and real models; root isolation ran separately.

Windows's 13 skips comprised two installer symlink cases without developer privileges (developer mode was not enabled), Unix-only service environment capture/systemd/zsh/command-link/bash/legacy-alias/POSIX-mode tests, two POSIX smart-command/ripgrep recognizers, a POSIX version fixture and real models. Native Job/ConPTY/pipes/resource/revocation tests actually ran. Existing platform conditions were retained without new failure-hiding skips.

Not accepted: independent Linux operations, three-platform login/logout/reboot, sustained saturation, macOS/Windows real forced-command/SFTP/forwarding restrictions, all Agent/model versions, graphical desktops or phones. Linux isolated Codex is covered only by follow-up below; isolated Claude/OpenCode are not. The full three-provider baseline is macOS owner evidence, not universal isolation compatibility. No macOS hard quotas, macOS/Windows identities or workspace disk quotas exist. Default Unix groups do not contain deliberate escape; Linux cgroups cover the tested revocation case. Guardian kill/kernel failure/power loss durability and independent security approval are not claimed. See [pending](pending.md) and [deployment/rollback](host-hardening.md).

## Follow-up execution

2026-11-14: no machine reboot/login/logout, service registration, production SSH/authorized_keys change, privilege grant, mount or system-level installation was performed.

| Check | Result |
| --- | --- |
| Ubuntu WSL existing OpenSSH + real CLI/HTTP + temporary restricted keys | PASS, 1.94s: forced command, scope, shell/SFTP/forwarding/PTY denial, durable revocation, owner access retained |
| Dedicated UID/controller + Codex 0.169.0 + deepseek-flash | PASS, 17.96s: two UIDs, real approve/decline/dedup, fresh Host Session resuming original conversation and random marker |
| Linux socket kernel regressions | PASS: anonymous streams transfer data but cannot reconnect to named broker, disconnect/reuse with AF_UNSPEC, reconnect after shutdown/close, or listen; DGRAM/SEQPACKET/named/vsock/io_uring refused, including WSL interop negative probe |
| Native CPU workload | PASS, 0.11s: cpu.max and increasing nr_throttled, parent controllers unchanged |
| macOS owner Codex rerun | PASS, 13.00s: shared acceptance-helper refactor preserved original mode |
| Ubuntu WSL JSON suite | PASS: 382 test/subtest passes, 11 explicit skips, zero failures; new root/model/SSH opt-ins executed separately |
| Linux race/vet/module verify/native CLI build | PASS, all exit 0, no race reported |
| Native race binary root-boundary/CPU/memory/SSH | PASS, 4.59s/1.12s/3.32s/3.92s; 128 MiB touched, succeeds under 512 MiB and kernel-killed under 64 MiB |
| Isolated Codex, same race binary | PASS, 19.12s: two approvals, one decline, one reply per repeated approval, cross-UID resume, no race reported |
| Windows current Host package default native suite | PASS, exit 0; not a new full Windows module race/vet/build run. Default SSH skip does not cover explicit failure below |
| macOS current full tests/make check/native distribution | PASS: readonly full race, vet, module verification and 5 Python checks |
| Current six-target test compilation | PASS: 36 binaries, darwin/linux/windows × amd64/arm64; additional platforms compile only |
| Temporary administrator Windows sshd | FAILED, prerequisites/approval pending: private-daemon auth handshake failed; production sshd unchanged, no weakened assertions/skips |

Read-only Windows privilege inspection reported SeTcbPrivilege but not SeAssignPrimaryTokenPrivilege. Handshake timeouts/initial daemon diagnostics were captured. This is not the standard SYSTEM service context; cause still needs service-context confirmation, and upstream similar issues do not establish a fix. A dedicated temporary service or isolated host requires approval. Default environment skips do not turn this explicit failure into success.

Linux used an existing native ELF Codex in Ubuntu WSL. Only the executable was copied to a root-protected test directory, not existing account profiles. Model credentials traveled through encrypted SSH stdin into root-private templates and UID-owned 0600 profiles, never Windows shared storage, argv, source or logs. Results were redacted; shared storage held only code, nonsensitive paths and logs. Private TMPDIR separated credential-home from the temporary root.

Tokio anonymous UnixStream signal wakeups had panicked under the old blanket policy. Only connected anonymous SOCK_STREAM pairs are now allowed, with named-broker reachability/reconnect/listen/other-family/interop negative tests. SSH initially failed StrictModes under /tmp and moved to a root-protected temporary path. Client LogLevel=ERROR hid forwarding refusal; only test -W/-tt client verbosity increased. StrictModes, forwarding restrictions, role boundaries and denial assertions remained intact.

Additional opt-in commands require the stated external conditions and dedicated temporary directories:

```sh
# Existing root/cgroup delegation, unprivileged test login and private CLI
NIMOTE_TEST_ROOT_ISOLATION=1 NIMOTE_TEST_ROOT_CPU=1 \
NIMOTE_TEST_SSH_LOGIN=<existing-login> NIMOTE_TEST_SSH_HOST_BINARY=<private-cli> \
<race-host-test> -test.v -test.run='^Test(RootIsolatedExecutionBoundary|RootLinuxCPUQuotaEnforcesThrottling|RootLinuxMemoryQuotaKillsExcessAllocation|NativeRestrictedSSHAndRevocation)$'
# Isolated private Agent profile; credentials never appear in arguments
NIMOTE_TEST_ROOT_LIVE_AGENT=codex NIMOTE_TEST_ROOT_AGENT_PROFILE=<private-profile> \
<race-host-test> -test.v -test.run='^TestRootLiveAgentControllerApprovalAndResume$'
```

Reboots still require separate approval. Windows SSH failure/unimplemented backends remain open; prerequisites are in [pending](pending.md). Linux and Windows test scratch directories, including private model profiles, were removed with exit 0 after checking no process still used them. Only test-created directories were removed; existing services/environment and local env.md remained. Redacted logs stay outside the repository; documentation and diff checks passed.

## Agent workflows and phone integration regression (2026-11-14)

Host inbox, verification cards, concurrent-workspace risks, history continuation and GitHub orchestration were integrated with sibling NimoteCode's existing Host/SSH/shared UI. Scope is in [completed](completed.md), remaining real-world acceptance in [pending](pending.md).

- Root full tests and GUI tests passed, retaining the existing duplicate `-lobjc` warning.
- `GORACE=atexit_sleep_ms=0 go test -race ./internal/host -run 'Inbox|Verification|GitHubWorkflow|TaskPrompt|AgentSelfClaim|ResumeReuses|Conflicts|Worktree' -count=1` passed. Zero exit sleep prevents each reexecuted helper adding one second to protocol readiness; race detection remains enabled.
- Targeted tests passed for handled completed turns surviving restart, Agent self-claims never establishing test success, worktree reuse after definitely-unsent startup failure, and hook-changed code requiring fresh verification without duplicate commits.
- Windows amd64/Linux arm64 Host test binaries compiled; this is not native acceptance.
- NimoteCode Host/UI governance: 301 passed, 3 default opt-in/native skips, static analysis passed; 770 Dart files, zero shared-UI violations.
- Explicit real local Go Host integration passed through encoded RPC/authenticated HTTP: inbox, >64 KiB full diff with digest, actual test subprocess evidence, repeated requests, stale state and restart persistence. This is not real SSH/phone acceptance.
- Real-font/theme screenshot tests covered English/Chinese light/dark inbox/verification/dispatch; these are UI artifacts, not device evidence. Android debug APK built.

GitHub tests used fixtures, temporary Git repositories and local bare remotes, not actual PRs. Agent fixtures are not model acceptance. ADB found no devices; no APK installation or device operation ran. No real test Issue, usable Host gh authorization or isolated real Agent profile was supplied, so Issue→development→verification→PR→phone review and real interruption recovery remain unaccepted.
