# Host hardening audit / 加固审计

English / 简体中文 · 2026-11-14 · Implemented increment with remaining release gates / 已实施增量与剩余发布门槛

## Baseline / 现状

Audit covers the CLI/SSH bridge, RPC/MCP, Manager, persistence and request ledger,
Codex/ACP state machines, history helpers, Unix/Windows process backends,
service/install lifecycle, desktop facade and build/test contracts. UI changes
and NimoteCode are outside this work. Existing completed/pending documents were
read before implementation; existing protections are retained.

| Priority | Existing / 已有 | Gap and decision / 缺口与决策 |
| --- | --- | --- |
| P0 identity | Owner token; observer credentials; per-request durable revocation; restricted SSH bridge | Add explicit session-scoped identities. Same-user arbitrary execution cannot be isolated by an HTTP ACL; do not grant shell/input/accept/configure through a purported sandbox. |
| P0 approvals | SDK request correlation, local permission IDs, one-time ACP grants, durable mutation receipts, bounded queues | Validate optional Codex thread/turn context and active lifecycle before publishing permissions; invalidate queued replies at turn end; enforce offered native decisions. |
| P0 resources | 32 active / 256 retained sessions; 16 MiB output; bounded ledger/history/events/frames | Configurable admission/runtime/free-disk protection; Windows jobs and opt-in Linux cgroup v2 native quotas; unsupported configurations fail closed. |
| P0 processes | Host lock, persisted launch intent, interrupted recovery, graceful tree kill; Windows atomic kill-on-close jobs | Use a lifetime-pipe supervisor and reap after owned-group cleanup; Linux delegated cgroups additionally contain escaped groups. Never guess ownership from stored PIDs. |
| P0 transport | Eight bridge slots, 64 KiB output pages, retained cursors, cancelable long polls | Unblock closeable output on cancellation; reject duplicate in-flight correlation IDs; signal future output resynchronization; bound and validate persisted event files. |
| P1 adapters | Shared structuredAgent / SDK / lifecycle; native provider controls and real handshakes | Consolidate provider launch/protocol descriptors; keep provider-specific wire semantics, avoid a second adapter hierarchy. |
| P1 detection | PATH discovery and live protocol negotiation | Report installed paths and protocol requirements; version probing must be bounded and explicitly invoked, never execute CLIs on every capabilities poll. |
| P1 scheduling | Immediate bounded admission, serialized state changes, startup budget, native cancellation | Retain immediate rejection (no hidden replay queue); add session runtime ceiling and observable limits. |
| P1 diagnostics | Bounded slog file, receipt capacity status, activity/error states | Add structured error codes while preserving error strings and status; expose truthful resource enforcement capabilities. |

## Release boundary / 发布边界

This audit is not release certification. Native macOS/Windows and WSL Linux tests,
Linux root-only execution-boundary acceptance and real Codex approval/resume are
recorded separately from remaining native service/restart/SSH/other-Agent gates.
See the [actual test record](host-hardening-tests.md) for completed runs.

本轮不修改 UI/客户端、不新增云或依赖、不把交叉编译当作实机验收。

## Implemented architecture / 已实施架构

### Device authority / 设备权限

The owner token and existing `observer` records retain their semantics. New
`session_observer` and `session_operator` records contain a nonempty explicit
`sessions` array (at most 256 unique valid IDs). Device records have a bounded
32 KiB read budget; credential/resource files retain 16 KiB budgets. `devices add-scoped` is a local
owner operation, serialized by the existing control lock. There is no remote
self-enrollment, wildcard, implicit workspace grant or grant-edit endpoint.

```sh
nimotehost devices add-scoped phone session_observer <session-id>...
nimotehost devices add-scoped emergency session_operator <session-id>...
nimotehost devices revoke <device-id>
```

| Principal / 身份 | Allowed / 允许 | Denied / 禁止 |
| --- | --- | --- |
| Owner | Existing full RPC, process and native history control | Existing protocol/validation restrictions remain |
| observer | Existing account-wide read-only endpoints | Every write and native-history process helper |
| session_observer | Host identity/capabilities; scoped session metadata/output/activity/Agent state; filtered lists/events | Other sessions; all writes; native history; global resource/receipt counts |
| session_operator | Scoped reads plus cancel, Agent interrupt and approval decline | Create/delete, terminal input/resize, prompt/steer, approval accept, configuration, compact/discover, history helpers, Host management |

受限 SSH key 必须仍使用 `restrict` 和固定绝对路径 `device-bridge <id>`；
不得为同一 key 另留普通 Shell 登录。Host 不自动改写 SSH 配置。
operator 是“安全停止/拒绝”权限；完整控制只提供给以下明确部署的 Linux 独立执行身份。
所有新角色每次 HTTP 请求重新读私有记录并检查撤销。已有在途操作可能完成，
撤销不回滚已读取数据。事件保留全局 cursor/resync 语义，仅过滤事件内容；
全局游标仍可暴露主机活动时序，不能声称完全隐藏流量模式。

Scoped cancel requires JSON `{ "request_id": "..." }`. Scoped `agent-message`
accepts only interrupt or approval/decline and requires its existing request key.
Permission checks precede receipt lookup. Device ledger keys are namespaced by
principal; owner keys/fingerprints and old receipts keep their original storage
contract. A guessed owner key cannot retrieve an owner receipt through a device.

### Linux independent controller / Linux 独立控制身份

`StartRequest.execution_identity={"uid":N,"gid":N}` is an owner-only, opt-in
Linux execution boundary. Each UID/GID is 1000000..2147483646, unknown to NSS,
unused by existing processes, reserved permanently in private `identities/` and
bound to exactly one Session. Administrators must reserve this numeric pool across
all Host instances and OS account creation. IDs are not automatically recycled.
Workspace is a private UID-owned directory; Host and credential state remain root-private.

Prerequisites: root Host, cgroup v2 delegated memory+pids, root-owned 0700 delegation
with finite aggregate memory.max/pids.max, and a root-owned public-readable/executable
Host binary under protected searchable ancestors (root-owned sticky /tmp permitted).
No setuid binary or account creation is involved. Unsupported conditions reject before launch.
Each isolated session defaults to 512 MiB/64 tasks/3600 seconds if that budget is unspecified.
The guardian launches the target atomically into the cgroup and drops UID/GID and
supplementary groups before executing user code. The final stage enables no_new_privs,
allows only IPv4/IPv6 socket creation and connected anonymous AF_UNIX stream
socketpairs for runtime signal wakeups. Named Unix socket creation, reconnectable
datagram/seqpacket pairs, other socket families (including vsock) and io_uring setup
are denied to block privileged socket/interop brokers escaping the job. The stage
then lowers priority and execs the command. Only amd64/arm64 are supported.
The target inherits a minimal environment with HOME/TMPDIR=workspace, not owner
API/SSH credentials. Provision required Agent credentials separately for this UID.
This is an OS account boundary, not a filesystem/network container: world-readable
files and IP networking remain available; administrator and kernel compromise are outside it.

`devices add-controller <name> <session-id>...` grants up to 64 such sessions,
with immutable UID bindings in `controllers/`. It allows scoped input/resize,
Agent prompts/approval/configuration, cancellation and completed-session deletion.
It cannot create sessions, run native-history helpers, access other sessions or
manage the Host. Authorization verifies live backend prerequisites, sidecar identity,
record, binding and revocation before receipt lookup. Input receipts also bind the
principal. Revoking a controller rejects new requests immediately; the 250ms
monitor kills its entire owned cgroup, including setsid descendants. Already read
data/finished effects cannot be undone. Bindings/reservations survive deletion and
revocation; a revoked UID cannot be reassigned to another device. A failed partial
multi-session grant is not usable for bindings that were not durably written.

Darwin/Windows return unsupported for execution_identity/controller grants. Ordinary
same-account owner sessions and observer/operator behavior retain their established semantics.

### Native approvals / 原生审批

The existing SDK, 64 pending permissions, bounded dispatcher and durable ledger
remain authoritative. Publication now holds the Agent mutex throughout validation
and insertion; the Codex unlock/relock publication gap is removed. A live thread
and active work are required. Supplied Codex threadId/turnId must be nonempty,
well typed and match the active context. Legacy Codex requests without these
optional fields remain supported; ACP sessionId must match and cancellation
rejects new requests. Canceled SDK contexts never publish pending approvals.

Turn completion/ACP finish/Agent close settle outstanding native handlers with
decline/cancel and remove their local handles. Dispatcher lookup drops stale
queued accepts. Manual Codex acceptance honors availableDecisions and never
selects accept-for-session or ACP allow_always. Existing per-session atomic
approval consumption and HTTP request receipts prevent duplicate delivery. Core retry
keys additionally bind to a SHA256 of the full AgentMessage, rejecting changed
content/action even for direct Manager callers without HTTP.
Already-dispatched native replies cannot be recalled.

### Resource ownership / 资源管理

`resources.json` lives beside `config.json` in the private Host configuration
folder, is loaded at daemon start and must be a private regular file (Unix 0600).
It is separate so an old GUI saving port/log configuration cannot erase limits.
Stop Host before editing; restart to apply. Missing fields inherit defaults.

```json
{
  "max_active_sessions": 8,
  "max_retained_sessions": 128,
  "max_runtime_seconds": 3600,
  "min_free_disk_bytes": 67108864
}
```

Defaults preserve 32 active/256 retained sessions and unlimited runtime, adding a
64 MiB free-space floor. Limits may only tighten the existing session ceilings;
retained must be at least active. Runtime is 0..31536000 seconds, zero unlimited.
Disk floor zero explicitly disables that guard. Startup rejects invalid policy,
unknown fields and null. Optional cpu_percent (0..100), memory_bytes (zero or >=16 MiB),
max_processes (0..4096) and cgroup_root are validated against the actual backend.
Windows applies CPU hard cap, aggregate committed-memory limit and active process
limit before atomic Job assignment. Linux requires a canonical cgroup v2 root with
the requested controllers already delegated; cpu.max, memory.max/swap.max=0/oom.group
and pids.max are written before clone3 cgroup assignment. Kernel/startup errors fail
launch; constrained/isolated auto mode never bypasses a failure with terminal fallback.
macOS rejects native quotas and cgroup_root. Capabilities report configured enforcement,
not an invented successful operation. Linux tasks include threads; Windows counts
processes. Memory is cgroup-charged memory on Linux, committed bytes on Windows,
not a portable RSS promise. CPU percent covers available host CPU capacity (Linux
uses runtime.NumCPU); no global cgroup controllers are auto-enabled.

磁盘探测使用 Unix Statfs / Windows GetDiskFreeSpaceEx，不引入命令或依赖。
准入在 Manager 锁下完成，超过并发/保留或磁盘不足则在创建会话文件和进程前拒绝。
首个任务启动时创建一个属于 Manager 的监控协程，每 250ms 检查超时和输出失败、每秒检查磁盘。
低空间或检测失败会停止已有托管进程；原因按现有完成路径尝试保存为失败。
若文件系统已无法写入，持久化错误会记录日志；重启可能只能恢复为 interrupted，
不承诺磁盘彻底耗尽时仍能写入完整失败记录。
这些是尽力保护，不是内核配额：空间可在采样间隔耗尽，工作区可能在其他磁盘，
未配置内核约束时用户代码仍可大量占用 CPU/内存；工作区写入硬配额尚未实现。资源失败不会触发自动任务重试。

The daemon admits at most 128 TCP sockets, 64 device read handlers and 16 device
mutation handlers separately, with per-device limits of 8 reads/2 mutations and
a separate owner reserve of 8 reads/4 mutations,
with at most four native-history helpers. Read/output pressure cannot consume the
mutation reserve. HTTP admission returns 503/Retry-After; resource admission
returns 429/Retry-After and survives receipt reload with its error classification.
There is no new durable task queue or automatic execution retry. Auto Agent startup
does not launch its terminal fallback after resource enforcement or output failure;
the original failed structured session is retained for inspection.

### Lifecycle and transport / 生命周期与流式传输

On Unix a private re-exec guardian observes the Host lifetime pipe. Host SIGKILL,
cancellation or target completion triggers cleanup. waitid(WNOWAIT) on Linux or
kqueue NOTE_EXIT on macOS observes exit without reaping; the root PID remains
pinned until group cleanup, preventing reuse races. The guardian stays outside the
target's group, keeps PTY foreground job control working, and closes unused stdin.
Without a cgroup, intentionally escaped groups remain outside this guarantee.
With configured Linux cgroups, the guardian uses a pinned cgroup FD and cgroup.kill;
restart cleanup uses validated Session `.scope` ownership evidence, never a stored
numeric PID. Windows retains atomic Job assignment and kill-on-close ownership.
Native history helpers share this lifecycle, bounded frames, context cancellation
and joined wait workers. Output failure terminates the owned session.
Host restart marks records interrupted; no live conversation/process resurrection
is claimed. If the guardian itself is externally killed, or the kernel fails,
this user-space pipe mechanism alone cannot establish cleanup/transaction durability.

RPC streams retain eight in-flight slots and bounded frames; duplicate concurrent
correlation IDs are rejected. Context cancellation closes closeable input/output
so blocked Scanner/Encoder operations can unwind before workers are joined.
A generic non-closeable io.Writer cannot be forcibly unblocked. Clean EOF still
drains accepted calls; disconnect never owns daemon task cancellation.

输出页沿用原字节 offset/start_offset/next_offset 语义；可选 `resync_required`
提示过期或超前游标，不改变旧客户端读超前位置得到空页的行为。
输出关闭可重复调用，并唤醒等待者。事件恢复限制 256 KiB / 1024 条，校验合法
Session ID、严格递增 revision、溢出边界；损坏日志拒绝加载且保留原文件，
不会自动删除账本、输出或活动历史来“修复”状态。

### Explicit state recovery / 显式状态修复

`nimotehost repair inspect` produces a SHA256-bound review plan for session JSON,
identity sidecars and event journal. `repair apply <plan-id>` rechecks the plan
under control+Host locks and requires Host stopped. Unknown filenames, unsafe or
oversized records and insufficient backup space block repair. Before changes it
writes private backup copies and plan, then commits a monotonic event-reset watermark,
quarantines malformed metadata and rebuilds events. Older event cursors require
resync even after a partial apply. Error diagnostics include the backup path.
Receipts, credential files, revocations, output and activity are never reset/replayed.
Corrupt ownership evidence requires operator review; repair does not infer process
ownership from a filename/PID or resurrect tasks. No automatic destructive repair.

### Adapter and diagnostics / 扩展与诊断

A narrow internal agentAdapter interface and shared CLI descriptors centralize
executable/protocol/arguments/version requirements for live sessions and history.
Codex/ACP retain their provider-specific wire behavior and shared SDK state machine.
Capabilities add an `agents` map with path/installed/protocol/protocol_version and
probe_status=not_probed; these describe requirements, not successful negotiation.
Actual availability remains gated by the existing live handshake.
ACP v1 load/resume accepts its standard empty acknowledgement while preserving
and checking the requested conversation ID. Claude ACP v2 initialization uses
info/capabilities, prompt messageId acknowledges insertion rather than completion,
state_update owns foreground completion, and tool-call permission subjects enter
the same context/one-time approval checks. Unknown states remain busy; unknown or
conflicting permission subjects fail closed. Legacy stopReason responses remain
compatible. The broad approval umbrella stays conservative; full advanced v2
message upsert/terminal display is not claimed by a minimal model acceptance run.

`nimotehost agents` explicitly probes installed structured adapter executables
with --version, using a two-second probe context and independently bounded supervisor startup/cleanup, plus
8 KiB diagnostic tails. Statuses distinguish not_installed/not_probed/ok/failed/
timeout/canceled. Version text is untrusted descriptive CLI output, not a protocol
compatibility decision. Normal capability polling does not execute third-party
code or spend these probe budgets.

`GET /v1/status` adds resource limits/counts/free-space availability and a fresh
`instance_id` per daemon. Unlike persisted host_id, instance_id changes on restart.
Errors preserve existing error strings and HTTP classes (resource admission adds 429), adding a stable `code`
category; uncertain mutation results use request_uncertain, separate from transient
capacity/unavailable errors. No token, environment, prompt, terminal output or tool diff is added to
operational logs. Self-review and tests are not independent security approval.

## Compatibility and rollback / 兼容及回滚

- Protocol remains 1; SSH/RPC/MCP routes and provider command interfaces remain.
  New capability flags: device_session_acl_v1, device_safe_control_v1,
  resource_limits_v1, output_resync_v1, process_supervision_v1, isolated_execution_v1;
  hard limits/isolated_execution_v1 depend on the configured OS backend.
- Session JSON schema is unchanged; optional `.scope`, `identities/`, `controllers/`
  and `event-reset` files carry new security/recovery state. Receipt resource_limited is optional;
  the existing ledger decoder ignores unknown fields. Owner receipt identity and
  retention watermarks are unchanged. Device principal namespaces affect only
  the newly supported device mutations.
- Existing observer JSON remains byte-schema compatible (sessions omitted).
  Old Hosts reject new role records instead of treating them as unrestricted
  observers. Old ListDevices may fail on these records; this is a safe refusal,
  not a supported transparent management downgrade.
- New resource fields are additive; previous hardened versions reject unknown fields,
  older versions may ignore the entire policy. None enforce the new UID/cgroup boundary. Do not downgrade while relying on resource guards or scoped devices.
  Stop/await cleanup, back up private state, revoke scoped keys and remove their
  SSH authorizations before an intentional binary downgrade. Keep identity reservations
  and controller bindings; remove new resource fields only after no session relies on them.
  Newer event-reset watermarks must not be restored backwards. Old binaries do not
  understand repaired-event resync, so use a fresh client cursor after downgrade. Retain the current
  ledger, retirement watermark and revocations when upgrading back; never restore
  an older snapshot over newer mutations or revocations. The backup is for
  reviewed recovery, not an automatic state rollback. Do not delete request
  receipts to make rollback work.
- Windows management locks live beside the state directory and normalize the
  legacy/current names, retaining exclusion across rename/rollback. Close older
  GUI/CLI managers before upgrade: older binaries use the in-directory lock.
  Private readers use protected share-delete handles; atomic publication uses
  FileRenameInfoEx POSIX replacement on supported kernels, with a bounded legacy
  sharing-conflict fallback. Failure never reports successful revocation; retries
  only publish the same metadata, never rerun a user command. RunLog seeks under
  its mutex instead of relying on a Windows append-only handle for truncation.
- UI/client code and third-party dependencies are unchanged. The lifecycle test
  now compares stable status fields plus instance identity, because free disk is
  intentionally dynamic; it also verifies restart changes the instance ID.

中文结论：旧客户端继续使用旧接口；新增能力需要协商。不做破坏性数据迁移，
不自动清理未知文件/凭证/任务，不把受限 operator 或应用层限流描述为 OS 沙箱。

## Follow-up acceptance and deployment / 后续验收及部署决策

Linux native Codex acceptance exposed Tokio's use of anonymous Unix stream pairs
for signal wakeups. Blanket socketpair denial caused startup to panic. The filter
now permits only connected stream pairs (including CLOEXEC/NONBLOCK flags), with
kernel tests proving data transfer, rejection of reconnect/listen/AF_UNSPEC/closed-peer
reuse against a reachable dummy broker, and continued rejection of datagram,
seqpacket, named Unix and vsock sockets. The Linux 6.18 implementation of
[Unix sockets](https://github.com/torvalds/linux/blob/v6.18/net/unix/af_unix.c)
provides the connection semantics; native tests also attempt harmless Windows interop
and require that it cannot execute under the isolated identity. This is still not
an IP network or filesystem container.

Real Agent acceptance uses an explicitly assigned private profile, not inherited
owner credentials. A root-owned test launcher reads a 0600 JSON credential file
inside the UID-owned private HOME and supplies only the intended provider key.
Codex uses a private TMPDIR distinct from CODEX_HOME; its security checks refuse
helper aliases when the credential home is nested inside TMPDIR. There is no
production auto-install or secret environment inheritance added by the test fixture.

Each resumed Host Session must use a fresh UID. The owner stops the previous owned
cgroup before explicitly transferring the selected private workspace/profile to
the new UID and granting a new controller. UID reservations are never reused and
profiles are not automatically shared across devices. The new fixture exercises
actual controller credentials for prompts/approvals, not an owner-token substitute.

Native SSH acceptance uses a separate daemon process, ephemeral host/device keys,
a pinned test known_hosts entry, a loopback-only port and private config. Linux
uses a root-protected temporary directory under /var/lib because OpenSSH StrictModes
correctly rejects authorized-key paths under world-writable /tmp. The real Host
runs under an existing unprivileged login using explicit private XDG_CONFIG_HOME;
no production state, user keys or service definitions are edited. Test-owned daemon
trees are placed in private cgroups/Windows Jobs and stopped before scratch cleanup.
Windows admin-mode daemon authentication has not passed; an approved service-context
acceptance remains required. A service-context limitation is described by the
[upstream Windows OpenSSH troubleshooting issue](https://github.com/PowerShell/Win32-OpenSSH/issues/1304),
but the current failure is not claimed resolved merely from that reference.

All machine reboots require explicit approval. See the concrete deferred actions
and prerequisites in [pending](pending.md); this follow-up has performed no reboot.
