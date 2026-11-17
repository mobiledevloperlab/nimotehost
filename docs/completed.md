# Completed functionality

`2.4.0` · Updated: 2026-11-17

This page describes implemented source behavior and boundaries, not full platform, phone or real-model acceptance. Remaining work is in [pending functionality](pending.md); security evidence is in [hardening tests](host-hardening-tests.md).

## Current scope

The 2026-11-17 increment connects existing Codex control sockets and OpenCode v1/v2 HTTP services, with original pending approvals, snapshots and reconnect, using existing Sessions/approvals/events. macOS Codex passed real CLI/local-model-fixture acceptance. Windows AF_UNIX/read-only ACL code and tests compile, but Windows native execution, real OpenCode and phone/platform matrices remain pending. See [architecture and capability matrix](external-agent-sessions.md) and [test evidence](external-agent-sessions-tests.md).

Claude adds official `agents --json` discovery, status and wait-reason projection independently of ACP through owner-only discovery. ACP v2 history explicitly requests replay and excludes control snapshots. Local Claude 2.1.311/ACP 0.107.3 isolated discovery/initialization passed. Official terminal attachment and unsupported independent-process structured takeover remain distinct.

| Feature | Implemented behavior |
| --- | --- |
| Optional Go Host | Independent module through existing authenticated SSH; no Connect, cloud account or public relay required, and existing SSH/SFTP/Terminal behavior remains |
| Session Manager | Start/list/attach/detach/input/resize/cancel/delete, with daemon-owned processes surviving phone disconnect |
| Output replay | Latest 16 MiB per session, absolute byte cursors and retention-start expiry checks; commands are never replayed |
| Platform backends | creack/pty on Linux/macOS; detected ConPTY on Windows 10 1809+, with Job Object process ownership |
| Agent adapters | Installed Codex/Claude/OpenCode terminal entrypoints; Codex app-server, OpenCode ACP and installed Claude ACP adapter. Structured availability depends on installation and handshake, never automatic CLI download |
| Agent state | Protocol-derived running/idle/needs_approval/completed/failed/disconnected/unknown, separate from process state. Plain terminal output is not parsed to invent status |
| Native history | Provider list/transcript/original-conversation resume; native conversation IDs differ from Host Session IDs, and failed resume never silently creates a new conversation |
| Approval policy | `agent_execution_policy_v1` confirm/smart/auto; default confirm. Smart auto-accepts only verifiable workspace reads/search/structured ACP edits; unknown/outside/delete requests remain pending. Auto sends one native reply without ACP allow_always and preserves native sandbox/approval configuration. Update only while idle, persist metadata first; nonconfirm policies forbid terminal fallback |
| Durable idempotency | Reservations/receipts for create, terminal input and structured messages; changed operation/session/content conflicts, uncertain outcomes never auto-replay |
| Task links | Existing SSH Task entry filters Host by Task/workspace; new/explicit resume carries references, existing sessions retain IDs |
| Lifecycle | At most 256 ordered summaries per session, with output references for start/execution/approval-queue/completion/interruption |
| Foreground observation | Event cursors cover work between polls; previously connected Host observation can continue in same-workspace IDE/AI/Git |
| Git review | Opens/refetches existing authenticated workspace Git panel; no copied Git implementation or Agent claims treated as real diffs |
| Mobile views | Reuses controls/terminal/approval/tool/notification UI, search/group/pin/alias/history, output search/pause/export and landscape layouts |
| User services | Linux systemd user, macOS LaunchAgent, Windows current-user Task Scheduler; readiness checks, active-work protection and upgrade backup |
| MCP/distribution | Read-only-by-default stdio MCP, license/module manifests, checksums, source export, native and six-target CLI distributions |

## Installation and services

Users need a trusted complete distribution, not Go. Verify SHA256, extract and retain licenses, BUILD.json, MODULES.json and SHA256SUMS.

```sh
# Source build requires Go 1.26.5+
go run ./tools/build
# Targeted distribution
go run ./tools/build -os windows -arch arm64 -version 1.0.0-dev
# Unix source or extracted package
sh scripts/install.sh
# Cross-platform source installer
go run ./tools/install
```

Windows uses `.\scripts\install.ps1`. Keep the binary at a stable user PATH location referenced by the service. Installation configures neither SSH nor Agent downloads.

```sh
nimotehost start
nimotehost capabilities
nimotehost status
nimotehost install
nimotehost restart
nimotehost stop
nimotehost uninstall
```

Start is foreground; install uses the current user's service; uninstall retains identity and records. Linux logout survival depends on systemd session/lingering policy. macOS/Windows services depend on login and do not promise logout survival. Unix service installation records absolute PATH entries and XDG_CONFIG_HOME; after tool-path changes, finish tasks and stop/install to refresh. Reinstallation does not restart healthy services or forcibly interrupt work. Back up before downgrade because older binaries strictly decode metadata.

## Identity and lifecycle

Default listener: `127.0.0.1:47831`; shared private configuration may change the port, never loopback-only binding. Every request requires a random local 256-bit Bearer credential and `Nimote-Host-Protocol: 1`. Phones invoke the remote CLI through existing authenticated SSH; Host credentials stay on the Host. Unix directories/files use 0700/0600, Windows current-user DACLs; startup holds the state lock.

Browser Origin is rejected, with no CORS. Bodies are limited to 1 MiB; unknown fields, multiple JSON values and invalid Session IDs fail. Session IDs are random 64-character hex strings; Host ID persists independently. The OS account is the authority boundary, full SSH shares account privileges, and restricted observers have independent revocable credentials.

`session_disconnect_survival=true`, `session_restart_survival=false`: phone disconnect detaches, Host stop kills owned processes, Host/OS restart marks unfinished records interrupted without resurrecting processes. Structured completed/failed outcomes require protocol evidence; older records lacking evidence are unknown. Native resume starts a new owned process for the original conversation.

## RPC / Session API

Reuse SSH exec or `rpc-stream`, without another message bus:

```sh
printf '%s' '{"method":"GET","path":"/v1/capabilities"}' | nimotehost rpc
```

Cross-platform `nimotehost rpc-encoded <base64(JSON)>` avoids shell interpolation. JSONL `rpc-stream` greets with `{"stream_protocol":1}`; requests carry id/method/path/body and replies correlate by ID, at most eight in flight. Closing a bridge never cancels daemon tasks.

| Method/path | Behavior |
| --- | --- |
| GET /v1/capabilities | version/protocol/min_protocol/platform/arch/host_id/features/tools |
| GET /v1/status | Host status |
| GET /v1/sessions?after=revision | Full or process-metadata snapshots |
| POST /v1/sessions | Create task/terminal/agent sessions |
| GET /v1/sessions/{id} | Session, Agent state and activity sequence |
| DELETE /v1/sessions/{id} | Delete finished sessions, output and summaries |
| POST /v1/sessions/{id}/cancel | Terminate the owned process tree; external connections detach |
| POST /v1/sessions/{id}/input | Base64 terminal bytes |
| POST /v1/sessions/{id}/resize | rows/cols, each 1..1000 |
| GET /v1/sessions/{id}/output?offset=0 | data/start_offset/next_offset; wait_ms=0..1000 |
| GET /v1/sessions/{id}/agent-state | cursor/ready/busy/status/approvals/controls |
| POST /v1/sessions/{id}/agent-message | Provider-gated prompt/steer/interrupt/approval/discover/configure/compact |
| GET /v1/sessions/{id}/activity?after=sequence | events/cursor/resync_required |
| GET /v1/events?after=revision | Latest 1024 process-metadata events |
| GET /v1/agent-history?agent=...&directory=...&cursor=... | Native conversation list |
| GET /v1/agent-history/{conversation_id}?agent=...&directory=...&cursor=... | Paginated transcripts |
| POST /v1/agent-history | Owner connects an existing Agent service to discover original sessions; body credentials never enter responses |
| GET /v1/inbox?view=... | Scoped inbox and counts |
| POST /v1/inbox/{id} | Revision-bound handling/archive markers |
| GET /v1/sessions/{id}/verification | Evidence source, code identity and actual tests |
| POST /v1/sessions/{id}/verification | Owner-configured test commands |
| GET /v1/sessions/{id}/verification-diff | Digest-bound retained diff |
| GET /v1/conflicts | Concurrent workspace/file-event risks |
| GET /v1/github-tasks | Durable Issue tasks |
| POST /v1/github-tasks | Create and dispatch |
| GET /v1/github-tasks/{id} | Task and recovery state |
| POST /v1/github-tasks/{id}/{action} | Owner-only continue/verify/publish/refresh_pr |
| POST /v1/extensions | Owner-only HCP 1.0; see [plugin protocol](plugin-system/protocol.md) |
| POST /v1/shutdown-if-idle | Stop only while idle and reject new sessions |
| POST /v1/shutdown | Graceful Host shutdown |

Example:

```json
{"method":"POST","path":"/v1/sessions","body":{"kind":"agent","agent":"codex","interaction":"auto","directory":"/absolute/workspace","request_id":"unique-random-id","task":{"id":"existing-task-id","label":"Fix build"}}}
```

- Kind is task/terminal/agent. Directory must be a native absolute directory; command/args execute directly.
- Interaction is terminal (default), structured or auto. Auto completes handshake before use, cleans up a failed attempt before terminal fallback and never replays dispatched prompts. Startup budget is 45 seconds; transport allows longer cold-start time.
- `resume.conversation_id` is native, not a Host Session ID. Unavailable resume never becomes a new conversation.
- `agent-message` requires a request ID ≤128 bytes. Prompt supplies text; approval supplies approval_id and accept/decline. Text, terminal bytes and approvals are distinct operations.
- Codex `controls.steer=true` supports follow-up during active work. `agent-state` and `turn_started.data` expose turn ID/can_steer. Send action=steer, text, turn_id and a unique request ID; native expectedTurnId binds the active turn. Startup, compaction, stale turns and ACP reject instead of queuing or starting work. HTTP receipt only acknowledges Host acceptance; native success emits user_followup with text/turn/request IDs, failure/exit emits steer_rejected with text/request/error. Neither changes current turn state. At most 16 pending follow-ups per session; retries reuse exact ID/content. Late replies cannot revive/stop a turn.
- UTF-8 structured JSONL emits ready/user/text_delta/message_completed/tool/turn_started/turn_completed/approval/approval_resolved/turn_rejected/error with type/data. Apply snapshots only when output byte cursor equals agent-state.cursor to avoid replay races.
- Compact/model/reasoning/slash controls are provider-negotiated. Opening a terminal from structured work may create an independent CLI conversation rather than migrate its native thread to a TUI.

## Capabilities, receipts and history

| Feature | Contract |
| --- | --- |
| mutation_idempotency_v1 | Optional create/input keys; structured messages already require keys |
| mutation_expiry_v1 | Seven-day keys, safe receipt rotation and status capacity diagnostics |
| agent_status_v1 | Agent state separate from process state; unknown native stop reasons remain unknown |
| session_task_v1 | Required task.id ≤512 bytes, optional label ≤160; never executes the original Task command |
| session_activity_v1 | Increasing per-session sequence, ≤256 summaries; expired/future cursors require resync |
| agent_history_v1 / provider_history_v1 | Provider installation/live protocol negotiation, no client-filesystem scan |
| rpc_stream / agent_events_v1 / agent_controls_v1 | Separately negotiated bridge/events/controls |

Gate fields by capability. `agent_structured_approval=false` remains conservative pending broader real acceptance; it does not mean Codex/ACP approval code is absent.

Request IDs bind operation/session/content. Reserve synchronously before effects, persist receipts afterward, and return the original receipt on retry rather than a fresh snapshot. Create receipts contain start_request_id. Hosts advertising expiry accept `v2.<Unix-seconds>.<64-lowercase-random-hex>`; Go uses `host.NewRequestID()`. GUI negotiates in the background for approvals/resume; old Hosts receive legacy opaque keys, and failed negotiation prevents mutation.

Expiry is seven days with five minutes of future clock tolerance. Expired requests fail 400 before effects; do not automatically replace the key, inspect the Session first. Startup and hourly mutation-triggered cleanup persist a monotonic expiry boundary before deleting expired receipts. Clock rollback/restart cannot revive them. Legacy receipts remain reserved indefinitely. `request_storage` in status and GUI diagnostics report key/byte/legacy counts with an 80% warning.

`accepted` is queue acceptance, `terminal_written` means terminal delivery; neither proves provider acknowledgement/completion. Content conflicts are 409. In-flight reservations, crash-lost outcomes, partial writes or receipt-save failure return 503 uncertain. Do not evade uncertainty with a new key. Auth/Origin/protocol failures use 401/403/426. Phones retry a lost create response at most once, only for the same Host identity with idempotency, never replay terminal input. Filesystem/process atomic transactions are not promised.

Process revision, activity sequence and byte offset are separate. Lifecycle stores summaries/output references, never prompts/output/tool diffs; queued approval is not upstream execution. Corrupt summaries report unavailable without killing sessions. A corrupt output log no longer blocks Host startup: Session status remains with an output-unavailable error, reads fail, and the original file is not overwritten. Future event cursors require full sync. Unix replacement syncs file and parent; Windows flushes files without claiming POSIX directory-sync semantics.

Notification cursors persist per Host in existing preferences; initial history establishes a baseline, reconnect catches up, and missing/repeated sequence prevents cursor advancement. Expired windows request resync rather than flood old approvals. Notification/preferences are not a cross-process transaction, so crashes may duplicate notices.

| Resource | Limit |
| --- | --- |
| Sessions | 32 active, 256 retained |
| Output | Latest 16 MiB per Session, absolute cursors; legacy .log readable |
| Process events | Latest 1024 |
| Lifecycle | ≤256 summaries and ≤256 KiB per Session for write/load; evict oldest first, reject an oversized single event without advancing cursor |
| Request ledger | 65,536 keys, 64 in flight, 64 MiB including result reservations, ≤2 MiB per receipt |
| Structured work | Reject new work after 4096 distinct requests while retaining approval/interrupt handling |
| Client cache | Four Session views in LRU, 100 app notifications; no duplicate large outputs |
| Foreground observation | Four concurrent activity queries, 30-second failure backoff; hidden unreachable Hosts probed no more often than every 15 seconds |

Only previously connected new Hosts are observed from same-workspace IDE/AI/Git foreground views. Hidden panels read no terminal output and send no input. Background/workspace change/disposal stops observation. Unvisited Hosts never auto-connect; older Hosts pause when hidden. No offline/background system push exists.

## MCP and development checks

MCP `2025-11-25` stdio defaults to host_capabilities, host_sessions, host_output and host_capability_query. Local `NIMOTEHOST_MCP_WRITE=1` adds host_start, host_cancel and host_capability_invoke. See [plugin mapping](plugin-system/protocol.md). MCP starts no daemon or listener; licenses are in [third-party notices](../THIRD_PARTY_NOTICES.md).

Adapters remain in the existing Go provider/SDK/authentication/startup/output/event/lifecycle boundaries. No automatic CLI download, approval bypass or dispatched-prompt replay. New providers need real handshake/cancel/exit/approval/replay/cleanup evidence; fake peers are not model acceptance.

```sh
go test -mod=readonly -race ./...
go vet ./...
python3 -m unittest discover -s test -p 'test_*.py'
go run ./tools/build
sh scripts/check-clean-build.sh
```

Earlier NimoteCode Host/Task/notification regression: 318 passed, 3 skipped for native SSH/paramiko/go-task/just prerequisites; static checks passed. Six CLI targets built, but cross-builds are not device acceptance and this increment did not rerun that entire client matrix. Current Host evidence is separate in [tests](host-hardening-tests.md).

Source/distribution/maintenance: [README](../README.md), [contributing](../CONTRIBUTING.md), [security](../SECURITY.md), [changelog](../CHANGELOG.md). Raw private historical audits were removed from public history. Current contracts are here and in [pending](pending.md), with [hardening decisions](host-hardening.md) and maintained test records.

<a id="desktop-architecture-audit"></a>

## Desktop architecture audit

The CLI/server/storage/manager/provider, service/install, builder, CI and tests retain `internal/host` as the single core; no mechanical directory migration or duplicate terminal/Agent implementation.

| Area | Reuse and boundary |
| --- | --- |
| CLI | Existing help/version/setup/install/uninstall/start/stop/restart/status/capabilities/rpc/rpc-encoded/rpc-stream/mcp arguments and semantics |
| Lifecycle | Foreground daemon, state lock, authenticated shutdown, Manager.Close-owned trees; GUI is a client and starts detached CLI only when no service is installed |
| Core | Existing tasks, session/output/receipt/event/SDK/history/MCP limits and behavior |
| SSH/auth | Existing SSH exec, local 256-bit credential, revocable observers/scoped roles/Linux opt-in controller; GUI does not scan authorized_keys to invent device lists |
| IPC | Existing authenticated loopback HTTP, no extra management port/UDS/named pipe/public listener |
| Services | Existing systemd/launchd/login tasks, explicit CLI-path constructors prevent launching GUI as a service |
| Platforms | Unix PTY/flock/groups, Windows ConPTY/Jobs/LockFileEx/DACL; detached GUI launch uses setsid or Windows process groups |
| Configuration | Private shared config.json; default port unchanged; edit only while stopped, lifecycle serialized by control.lock |
| Environment | Existing MCP switch, PATH/XDG/LOCALAPPDATA/SHELL; Fyne owns theme/preferences, credentials are not copied |
| Distribution | Existing builder/install/license/export/six-target CLI plus native -gui bundles and GUI licenses |
| Mobile protocol | Protocol 1 and endpoints/negotiation retained; old clients discover port through updated CLI; no P2P/DERP |

`internal/desktop` provides authenticated facade, status/sessions/Agent approval, process locks, detached start and redacted diagnostics without Fyne. `cmd/nimotehost-gui` is a separate Fyne module with sidebar/tray/theme/six-language management. Tray/close use [Fyne APIs](https://developer.fyne.io/explore/systray). `config.go/log.go` provide private lock-protected config and 64 KiB logs; credentials/Agent/terminal output never enter new logs, and diagnostic export contains health summaries only.

Opening GUI does not start Host. Start detects existing Host, otherwise reuses the user service or detached CLI. Five-second metadata polling never reads terminal output; closing/crashing GUI does not own daemon cancellation. Windows/macOS close hides. Linux exits by default because Fyne cannot reliably detect tray availability; hide requires explicit opt-in to a known-working tray. Quit GUI and Stop Host are separate. Login-start enable installs/starts the service; disable removes it, both requiring stopped Host. Login, not unattended machine boot, is the guarantee.

### Desktop interaction and localization

Overview/Sessions/Devices/Settings are primary pages. Overview shows local service state/actions, active/pending summaries and phone instructions without implying SSH connectivity. Sessions search/filter by name/workspace and prioritize approvals; full IDs stay in details. Past conversations show native text/tool results, pagination and original-ID resume. Stop/restart live behind confirmed secondary actions; port/log settings, diagnostics and raw data are disclosed on demand. Existing Fyne widgets are reused, without web/custom frameworks.

Theme/ThemeOverride/List/Layout provide horizontal icon/text navigation, service summaries, activity/phone cards and layered text/status colors. Light/dark updates immediately without stale palette retention. Unknown counts show an em dash, not zero. Pending navigation clears stale search/history filters. Minimum logical size 960×640 protects navigation/long strings while content scrolls. Tests check body/secondary/button contrast, both themes across six languages and control sizes; not complete accessibility certification.

Languages align with NimoteCode: en/zh-Hans/es/ja/ko/ru, default English. Settings changes/saves Fyne preferences immediately; rebuilding cancels view queries, not Host/tasks. Embedded JSON uses existing go-i18n/x/text versions. Unknown locales fall back to English. Names/commands/paths/user or Agent text/technical JSON are not translated, and file pickers follow OS/framework language. Production Fyne uses system fonts; Linux needs CJK fonts. CI disables system-font scanning and cannot prove real CJK glyphs; macOS non-CI preview checked them.

Tests cover catalog key/template parity, literal references, preferences, filtering/sorting, unknown states, recycled-row sizing, named-device forms and four-page/six-language layouts. Optional previews are in CONTRIBUTING; real trays, accessibility and native-speaker review remain pending. Navigation principles reference [Docker](https://docs.docker.com/desktop/use-desktop/), [Tailscale](https://tailscale.com/docs/features/client/manage-preferences) and [Apple disclosure](https://developer.apple.com/design/human-interface-guidelines/disclosure-controls), without copying assets or account/network systems.

### Desktop build and installation

```sh
# Native target runner and desktop SDK required; CLI remains CGO-free
# Go 1.26.5+
go run ./tools/build -gui -version 1.0.0-dev
```

- Linux: gcc, pkg-config, libgl1-mesa-dev, xorg-dev, libxkbcommon-dev, libwayland-dev, wayland-protocols; runtime needs compatible OpenGL/X11 (Wayland through XWayland). CLI-only servers need none. Native Ubuntu 22.04 containers check glibc symbols ≤2.35; this candidate baseline still needs actual distribution/desktop acceptance.
- macOS amd64/arm64: Xcode Command Line Tools; app includes GUI/CLI. Bundles are unsigned/unnotarized. Release signing requires the developer's own Developer ID for nested CLI/GUI/app, then notarize/staple and regenerate archives/checksums; signing is not claimed complete.
- Windows amd64: native Go/MinGW-w64 GCC, CI msys64/mingw64/bin; console-free GUI plus CLI zip, existing per-user installer, no admin requirement/MSI/Authenticode.
- Windows/Linux ARM64 GUI have native CI configuration; runtime acceptance remains pending.

Desktop distributions use `dist/gui-<os>-<arch>` and `nimotehost-gui-<version>-<os>-<arch>`, retaining original CLI names. Install the whole fixed directory; macOS must launch the installed app rather than enable a service from a temporary extraction and delete it. A v* tag publishes CLI/GUI/source after all required checks, using [GitHub runners](https://docs.github.com/en/actions/reference/runners/github-hosted-runners). GUI cross-compilation without native SDKs fails explicitly.

Existing tests cover protocol/auth/sessions/Agent/idempotency/install/services; additions cover real detached CLI surviving manager exit, reconnect/repeated starts, configuration locking/consistency, port conflicts, hide/close/reopen and active tasks after GUI exit. Headless Fyne is not tray/login/reboot/phone acceptance.

The desktop increment passed full root readonly race/vet, GUI tests/vet with ci tags, both module verifications and five Python checks. Six CLI targets built; native macOS ARM64 GUI compiled, packaged and opened, and GUI exit did not stop Host. Archive manifests matched every file. Missing Fyne 2.8 picker translations used its public resource API, without SDK/environment modifications. Platform trays/signing/reboots/phones remain pending.

## Observer devices, history and updates

`devices list|add <name>|revoke <id>` and GUI Devices register/revoke independent random credentials stored privately on Host; records contain hashes, lists expose neither credentials nor hashes. Every HTTP request rereads records, so subsequent calls on old RPC streams fail after durable revocation. In-flight requests may finish and disclosed data cannot be recalled. Other devices/owner/tasks remain unaffected.

Default observer reads account-wide status/capabilities/session metadata/output/activity/Agent state, but cannot create/input/approve/shutdown or launch history helpers. Output visibility requires deliberate trust; scoped identities can restrict sessions. Full same-account SSH is not retroactively isolated and could read owner credentials.

Administrators install a distinct restricted SSH key per device using GUI's `restrict,command="..."` line, substituting a real PUBLIC-KEY and fixed absolute `nimotehost device-bridge <id>`. Do not also authorize unrestricted login. The bridge accepts only existing rpc/rpc-encoded/rpc-stream/status/capabilities, never executes SSH original-command text. `restrict` disables forwarding/PTY/user rc; see [OpenSSH](https://man.openbsd.org/sshd). Host neither edits SSH config nor existing unrestricted keys. Windows commands depend on server shell; GUI does not emit an unverified Windows authorization line. Remove the SSH key separately after Host revocation; Host denial does not wait for that step.

Past conversations query provider APIs for paginated lists/transcripts. Codex Next transcript page retains native cursors. Each projected JSON row is ≤24 KiB with explicit truncation; up to 50 rows fit 2 MiB RPC. ACP lacks continuation cursors: ≤4 MiB raw notifications are drained into ≤2000 rows/1 MiB projection; either overflow reports truncated, never complete history. GUI explicitly resumes original conversation IDs without replay/new-conversation fallback. Query settings remain bound to displayed selections even if edited afterward.

Settings explicitly queries [official GitHub releases](https://docs.github.com/en/rest/releases/releases) and opens downloads after confirmation for existing checksum/install flows; no downloaded code executes or active service silently updates. Cross-host dashboards, offline push and daemon/OS live-process survival remain pending.

Baseline device evidence: full race, both module vets, Fyne lifecycle, real CLI bridge, old-stream revocation, persistence, principal isolation and write rejection passed; approximately 510,000 permission fuzz cases had no failures. This is not SSH key-server setup evidence. Later WSL SSH/controller results are below. Physical phones, Windows/Linux graphics and universal full-control release acceptance remain open.

## Host hardening increments

2026-11-14 preserved protocol 1, SSH/RPC/MCP, Session/ledger compatibility and client/UI. Implemented scope:

- session_observer/session_operator and add-scoped, with item/list/event checks and per-request revocation; operators only stop/interrupt/decline.
- Root/private finite cgroup Linux numeric UID/GID execution, permanent per-session reservations and add-controller grants; cleared owner environment, no privilege elevation/named Unix brokers, only connected anonymous stream pairs for runtime wakeups, other reconnectable pairs/io_uring rejected. Unsupported Darwin/Windows/Linux prerequisites fail rather than invent same-user isolation.
- Native active-thread/turn/offered-decision validation, turn-end expiry, full-message/principal receipt binding.
- Private resources.json admission/retention/runtime/disk limits; Windows CPU/committed-memory/process Jobs, delegated Linux CPU/memory/tasks, explicit macOS hard-quota rejection.
- 128 TCP connections, per-device 8 reads/2 writes, separate owner reserve and ≤4 history helpers, without hidden retry queues.
- Lifetime-pipe Unix supervisor, pinned root PID until cleanup, optional Linux escaped-descendant containment, shared history-helper ownership/cancel.
- Stopped-Host SHA-bound repair with backup, quarantine, event reconstruction, preserved receipts/credentials/output and monotonic resync watermarks; unsafe/unknown state rejected.
- Shared adapter descriptors/helper lifecycle, explicit probes, configuration-aware capabilities/resource status and stable error classes. Native Windows migration-lock/log/default-directory fixes are regressed.

Deployment/quotas/rollback are detailed in [hardening](host-hardening.md); evidence and remaining risks are in [tests](host-hardening-tests.md) and [pending](pending.md).

### Completed checklist and scope

The following ten items match the completed pending-list entries. Original implementation references were `82e86d2`, `4fb0c6e`, `876f481`, and status synchronization `059d55e`; public history sanitization changes commit IDs.

- [x] Linux root/private finite cgroup/dedicated UID/controller isolation, native Ubuntu WSL boundary and race acceptance.
- [x] Seven-day new keys, safe online rotation/capacity warnings; legacy retirement remains open.
- [x] Linux CPU cpu.max and actual nr_throttled growth in dedicated test subtrees, no automatic global controller changes.
- [x] Linux memory: actual 128 MiB touch succeeds under 512 MiB and is killed under 64 MiB; native CPU/memory race acceptance.
- [x] Supervisor SIGKILL ordinary-group cleanup and cgroup setsid revocation; non-cgroup escape remains a limitation.
- [x] WSL real OpenSSH private daemon/ports/keys, forced command/scope/shell/SFTP/forwarding/PTY rejection/revocation/owner access; native race acceptance.
- [x] macOS owner real Codex/Claude/OpenCode/deepseek-flash approve/decline/dedup/original-conversation/random-marker continuity.
- [x] Linux isolated Codex 0.169.0/controller/deepseek-flash approve/decline/dedup/fresh-UID resume with explicit profile transfer; Tokio stream-pair fix and named-broker negatives.
- [x] Fair per-device read/write/connection limits, owner reserve, explicit repair/backup/monotonic recovery watermark regressions.
- [x] Native Windows CPU/committed-memory/process Job limits/allocation rejection/lifecycle; no Windows independent identity claim.

macOS full/make-check/native build and Ubuntu full/race/vet/native build plus explicit root/CPU/memory/SSH/real-Codex race acceptance passed. Current Windows Host package native regression passed; that increment did not rerun the complete Windows module race/vet/build. Six-target compilation only proves compilation. Versions/commands are in [test records](host-hardening-tests.md).

Windows temporary SSH still failed; macOS SSH, independent Linux operations, login/logout/reboot, sustained load, disk/macOS hard quotas and macOS/Windows identity backends remain pending. No machine reboot or production SSH change occurred; temporary directories/model credentials were removed. Checked items do not establish universal release readiness.

## Agent task workflows (protocol 1, additive extensions)

## Delivery status

This repository contains Host and the Fyne desktop manager. NimoteCode phone UI is in the sibling repository and is now integrated through its existing Host panel. Existing SSH `rpc` and `rpc-stream` transport carries these endpoints without sending Host credentials to the phone. Existing protocol fields and endpoints remain compatible. New clients must negotiate feature flags.

### 1. Single Host task inbox

Reused: Session, TaskReference, AgentSnapshot, activity journal, atomic private snapshots, restart interruption, original approval controls and durable mutation ledger. Added: `agent_inbox_v1`, `GET /v1/inbox?view=approval|failed|review|running|handled|archived|all`, `POST /v1/inbox/{session_id}` with `request_id`, `revision` (opaque token from GET), `status` (`open`, `handled`, `archived`). Default excludes archives. Counts obey the same device scope as items. Each item includes full Session/task/workspace, update time, execution summary, and session link ID. Desktop Sessions → Task inbox shows counts, details, approval controls, handling and archiving.

Handling is Host-wide metadata and persists in private `.review` snapshots. Mutations acknowledge an exact session revision/activity sequence. A completed turn that the user already handled remains handled when its idle protocol process disconnects or Host restarts; new prompts, approvals and outcomes reopen it. Active approvals cannot be hidden or archived. Approvals are read from current protocol state through the existing session endpoint; interruption never restores actionable approvals. Operators/controllers may mark authorized records; observers cannot mutate. Review retries reuse the existing receipt ledger and are namespaced by device. Failed writes do not advance the marker. Deleting a Session removes auxiliary inbox records.

Tests: persistence, reopening on revision, stale writes, live approval hiding rejection, interruption and scoped counts/ACL. Real multi-provider Agent and phone interaction remain unaccepted.

### 2. Verification cards

Reused: TaskReference, Session process execution, exit/time metadata, output replay, resource limits, private storage and durable mutation ledger. Added: `task_verification_v1`, `GET /v1/sessions/{id}/verification`, `GET /v1/sessions/{id}/verification-diff`, owner-only `POST /v1/sessions/{id}/verification` with `request_id` and `tests: [{command, args}]`. Commands are executable/argv, not implicitly a shell. Desktop session details and inbox link to a card, captured diff, retained raw evidence, and explicit verification execution.

Host captures Git HEAD, branch, dirty state, tracked diff/stat/file paths, and hashes untracked file contents. Unknown, unreadable or oversized evidence yields no trusted fingerprint. Pre/post snapshots must match; report reads compare the current version and activity. Agent completion/exit alone yields `not_run`, never `passed`. Each explicit test has a separate Host Session, timestamps, exit code, tested version and raw output link. Restarted in-flight verification is `interrupted`, never replayed automatically. User may explicitly launch a new verification; uncertain requests require inspection. Command output follows existing rolling retention, not an unlimited archive.

The task goal is client supplied; execution result is a Host lifecycle observation, not proof of the goal. Git snapshots are actual Host observations and do not attribute changes in a shared worktree. Agent tool command text is not trusted Host test execution. PR URL is null until linked by a workflow. Missing repositories/commits and oversized evidence are explicitly unknown. Worktrees without an initial commit cannot be validated. Reports cover the current worktree against HEAD; committed implementation ranges need a workflow base revision (see later delivery notes). Only explicit configured tests are assessed, not test completeness or semantic correctness.

Tests use real temporary Git repositories and local subprocesses for not-run/pass/fail, exit code, staleness and persisted evidence. They do not stand in for real coding Agent or phone acceptance.

### 3. Concurrent workspace risks

Reused: active Sessions and normalized provider tool events. Added `workspace_conflicts_v1`, `GET /v1/conflicts` and desktop inbox risk viewer. Canonical directories resolve symlinks; Git worktree root and common directory distinguish same directory, same worktree and independent worktrees in one repository. Independent worktrees are informational, not shared directory conflicts. Scope filtering precedes pair comparison and never reveals another device's hidden Session.

`shared_directory` / `shared_worktree` mean possible risk. `observed_tool_overlap` requires both Sessions to report completed file-change tool events for an identical canonical path during overlapping lifetimes. Evidence is explicitly attributed to provider protocol events, not independently proven OS write authorship. Pending approvals, prose and a dirty Git status never establish authorship. Event retention is bounded to 256 paths per Session; absent evidence means attribution is unknown, not absence of risk. No process scanning, rollback, overwriting or conflict resolution occurs.

`isolated_worktree_v1`: owner `POST /v1/sessions` accepts optional `worktree: {branch?, base?}` and a mandatory `request_id`. Host creates a new branch and worktree under its private state before launching the original adapter. Default base is committed HEAD; existing uncommitted files are not copied. Retry uses the original start receipt. Worktrees and branches survive failures/deletion for manual recovery. Resume and execution identity modes cannot be combined with this option. Workflow dispatch uses this execution mode; general Session creation remains an API operation.

### 4. History discovery and continuation

Existing GET/history resume remains compatible. The explicit attach path added on 2026-11-17 connects original threads through existing Codex control sockets or OpenCode HTTP services; see [architecture/capabilities](external-agent-sessions.md). Arbitrary PTY processes and historical records are not claimed as controlled.

Reused: PATH detection, explicit version probes, Codex app-server history/resume, ACP history/list/load/resume, replay and startup handshake. Added `agent_history_aggregate_v1`, `GET /v1/agent-history?agent=all&directory=...&cursor=...`. Aggregate responses retain `items`/`next_cursor` and add per-provider status. Each row has provider, original conversation ID, title, workspace and update time. Errors remain per-provider; one failing adapter does not suppress successful results. Desktop history supports All agents and shows provider/workspace/update time.

Capabilities separately expose `cli_discovered`/`cli_path`, adapter `installed`/`path`, history/resume requiring a successful protocol operation, and `live_attach=false` with reason. PATH/version success never asserts history or running-process attachment. Queries use the current OS user's provider protocol; no other users' stores are scanned. Installation flags are candidates, not promises that upstream supports a method.

Resume keeps the original conversation ID. Concurrent resume requests reuse an active matching Host Session and reject a different workspace, policy or task binding. Original mutation receipts also protect retries across restart. Replay is presentation history and does not resubmit old prompts. Existing adapters start owned protocol processes; attaching arbitrary live CLI processes is explicitly unsupported.

Tests cover capability separation, invalid aggregate cursors, unavailable providers and active conversation deduplication. Existing upstream protocol fixture tests continue to apply. Live installed CLI and phone acceptance is still required.

## Phone integration checklist

- Negotiate each feature flag; maintain old Session screens for older Hosts.
- Add inbox views/counts, archived filter and opaque revision handling; refresh on conflict.
- Fetch current `agent-state` before approving and use original `agent-message` with a fresh request ID. Preserve operator decline-only restrictions.
- Show verification origin/unknown fields, expiry, code identity, raw output retention and null PR values. Never display lifecycle completion as validation success.
- Show risk severity and evidence source, including separate worktrees as informational.
- Support all-provider history cursor and partial provider errors. Use original conversation ID and keep the same start request ID for network retries.
- Device observers remain read-only; no new same-user execution privilege is granted by these endpoints.

## Acceptance still required

Real Codex, Claude Code and OpenCode multi-Agent tasks, installed-provider history/resume, GUI interaction on supported operating systems, phone over SSH reconnection, and the real GitHub Issue → PR flow. Unit/protocol fixture tests are not claimed as these acceptances.


### 5. GitHub Issue workflow

`github_issue_tasks_v1` advertises persisted task reads; `github_issue_dispatch_v1` advertises the local Git/gh prerequisites. CLI presence does not prove account authorization. Host uses the existing local gh authorization and authenticated SSH owner channel; no new token storage or device execution permission is introduced. `POST /v1/github-tasks` accepts `request_id`, `issue_url` or `repository` + `issue_number`, absolute `directory`, `agent`, `execution_policy`, `dispatch_policy` and 1–8 `tests` (executable/argv). Default `manual` waits after passing verification; explicit `draft_pr_after_verification` permits commit/push/draft PR after validation. No merge endpoint exists.

Issue title/body are fetched through gh; PR masquerading as Issue and mismatched local GitHub origin are rejected. The remote default branch is fetched and its exact SHA binds the new worktree. Stages are `queued`, `developing`, `needs_approval`, `verifying`, `verification_failed`, `ready_for_pr`, `review`, `completed`. Errors retain `failure_stage`, workspace, Session, commit/PR intent and explicit recovery operations. Completed means GitHub confirms merge; PR creation only means review. Reads never cause GitHub writes.

`GET /v1/github-tasks` lists tasks; `GET /v1/github-tasks/{id}` reads one; owner-only `POST /v1/github-tasks/{id}/continue|verify|publish|refresh_pr` accepts a mandatory request ID. Session approval, verification, diff and raw output stay on existing/new Session endpoints. Scoped devices see only tasks whose Session is authorized; queued tasks are owner/global-observer only. Full SSH owner authority is required to dispatch, run tests or publish. Devices gain no same-user arbitrary command execution.

Creation/start/prompt/test/action receipts deduplicate repeats. Workflow changes are staged in the isolated worktree before verification so newly created files retain the same fingerprint through publication. Branches derive from the durable task ID; a commit intent plus tree SHA and commit marker reconciles interrupted commit creation. Push repeats the same SHA without force. PR lookup by branch precedes creation; an uncertain create is not sent again when no existing PR can be confirmed. This deliberately stops for inspection instead of promising exactly-once remote effects. Startup does not silently resume agents or tests. Explicit continuation uses the original conversation ID and a new continuation prompt, never resubmits the old Issue prompt. Missing conversation identity, failed validation or uncertain operations retain work and explain the next inspection step.

GitHub API contracts: [GitHub CLI API](https://cli.github.com/manual/gh_api) and [pull requests REST API](https://docs.github.com/en/rest/pulls/pulls). GitHub Enterprise hosts/forks/cross-repository PRs and automatic merge are outside this first implementation. Real Issue/Agent/phone acceptance remains pending; local protocol fixtures must not be called real GitHub acceptance.

## Additional evidence boundaries

Reports reject untracked executable-mode changes, unverifiable submodule contents and capture errors. Identity-isolated sessions do not expose Host-user Git snapshots or run verification as Host identity; a separate test scope is unimplemented. Original scoped output remains under its existing ACL. Phone success requires Host process evidence, exit/timing, matching tested version and at least one configured test.

## Delivery and client integration (2026-11-14)

| Stage | Reuse | Modules | Delivered behavior |
| --- | --- | --- | --- |
| Inbox | Sessions/activity/approvals/device ACL/ledger | internal/host/inbox.go, GUI inbox.go, client host_task_hub.dart | Five counts, handling/archive, stale-version refusal and completed-result restart retention |
| Verification | Process backend/private snapshots/output | verification.go, server.go, GUI/client report views | Base/current SHA, Agent self-claim attribution, real process evidence, staleness and complete bounded diff |
| Conflicts | Normalized provider events/Git/active Sessions | conflicts.go, worktree.go, new-session form | Workspace risks, attributed file intersections and optional independent worktrees |
| History | Native Codex/ACP history/resume/replay | history_discovery.go, agent_adapter.go, desktop/client browser | Aggregation, partial failure, CLI/adapter separation and active-resume dedup |
| GitHub | Adapters/approvals/test evidence/SSH/local gh auth | github_tasks.go, host_client.dart, client journal/hub | Durable stages, branches, explicit publication policy, recovery and commit/PR dedup |

NimoteCode reuses NimoteButton, NimoteListRow, NimoteFormField, NimoteSelectField, NimoteTabBar, AppModal and AppSheetScaffold with existing theme/type/spacing tokens and six locales. Approval remains in the conversation view. Negotiated new tasks/Agents can choose independent worktrees while source-workspace metadata preserves Task filtering. Phone OAuth stays on the phone; Host publication uses existing local gh auth. Dispatch IDs/configuration digests survive phone restart; uncertain delivery/access denial never discards identity. Credentials and execution bodies are not persisted in that journal. No cross-host aggregation, arbitrary CLI takeover, automatic merge, semantic duplicate-work detection or new device execution privilege is introduced.

Recovery boundaries:

- A definitely unsent Issue prompt may be sent once after adapter startup is repaired. Accepted receipts recover; incomplete delivery reservations stop for inspection rather than resend. Resume retains original conversation IDs.
- A hook changing code invalidates prior evidence. Fresh explicit verification can adopt the same existing commit/tree without a duplicate commit. Refreshing a changed remote PR head marks evidence stale.
- Complete diffs retain at most 4 MiB, delivered in 64 KiB byte pages with SHA. Raw output follows existing rolling retention/offsets; metadata/untracked-content budgets remain bounded. Oversized/unreadable evidence is unknown, never a pass.
- Independent worktrees are not shared-directory conflicts. Completed tools editing the same external canonical path can still produce overlap warnings across worktrees.

[Host test records](host-hardening-tests.md) and the sibling client's docs/host/completed.md record UI tests, local Host integration, rendered fixtures and APK builds. They do not establish physical-device, real-model, real-SSH or real GitHub Issue acceptance.
