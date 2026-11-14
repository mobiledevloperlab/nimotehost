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
| P0 resources | 32 active / 256 retained sessions; 16 MiB output; bounded ledger/history/events/frames | Add configurable admission/runtime/free-disk protection, bounded HTTP helper concurrency; report CPU/RSS quotas as unsupported rather than promise an unenforced limit. |
| P0 processes | Host lock, persisted launch intent, interrupted recovery, graceful tree kill; Windows atomic kill-on-close jobs | Reap same-process-group Unix descendants after root exit. SIGKILL survival on Unix and escaped process groups need OS containment, not unsafe PID-based startup killing. |
| P0 transport | Eight bridge slots, 64 KiB output pages, retained cursors, cancelable long polls | Unblock closeable output on cancellation; reject duplicate in-flight correlation IDs; signal future output resynchronization; bound and validate persisted event files. |
| P1 adapters | Shared structuredAgent / SDK / lifecycle; native provider controls and real handshakes | Consolidate provider launch/protocol descriptors; keep provider-specific wire semantics, avoid a second adapter hierarchy. |
| P1 detection | PATH discovery and live protocol negotiation | Report installed paths and protocol requirements; version probing must be bounded and explicitly invoked, never execute CLIs on every capabilities poll. |
| P1 scheduling | Immediate bounded admission, serialized state changes, startup budget, native cancellation | Retain immediate rejection (no hidden replay queue); add session runtime ceiling and observable limits. |
| P1 diagnostics | Bounded slog file, receipt capacity status, activity/error states | Add structured error codes while preserving error strings and status; expose truthful resource enforcement capabilities. |

## Release boundary / 发布边界

This audit is not release certification. Native Linux/Windows, real restricted SSH,
real Agent approval/resume and crash/system restart testing remain required.
Full execution-capable device isolation, hard CPU/RSS quotas and Unix daemon-crash
containment remain open unless explicitly implemented and verified below.

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
operator 是“安全停止/拒绝”权限，不是任意代码执行权限；完整执行隔离仍未完成。
所有新角色每次 HTTP 请求重新读私有记录并检查撤销。已有在途操作可能完成，
撤销不回滚已读取数据。事件保留全局 cursor/resync 语义，仅过滤事件内容；
全局游标仍可暴露主机活动时序，不能声称完全隐藏流量模式。

Scoped cancel requires JSON `{ "request_id": "..." }`. Scoped `agent-message`
accepts only interrupt or approval/decline and requires its existing request key.
Permission checks precede receipt lookup. Device ledger keys are namespaced by
principal; owner keys/fingerprints and old receipts keep their original storage
contract. A guessed owner key cannot retrieve an owner receipt through a device.

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
unknown fields, null and nonzero cpu_percent/memory_bytes. Hard CPU/RSS limits
are not silently ignored and capabilities advertise both as false.

磁盘探测使用 Unix Statfs / Windows GetDiskFreeSpaceEx，不引入命令或依赖。
准入在 Manager 锁下完成，超过并发/保留或磁盘不足则在创建会话文件和进程前拒绝。
首个任务启动时创建一个属于 Manager 的监控协程，每 250ms 检查超时和输出失败、每秒检查磁盘。
低空间或检测失败会停止已有托管进程；原因按现有完成路径尝试保存为失败。
若文件系统已无法写入，持久化错误会记录日志；重启可能只能恢复为 interrupted，
不承诺磁盘彻底耗尽时仍能写入完整失败记录。
这些是尽力保护，不是内核配额：空间可在采样间隔耗尽，工作区可能在其他磁盘，
用户代码仍可能大量占用 CPU/RSS 或写入任意可写位置。资源失败不会触发自动任务重试。

The daemon admits at most 64 read handlers and 16 mutation handlers separately,
with at most four native-history helpers. Read/output pressure cannot consume the
mutation reserve. HTTP admission returns 503/Retry-After; resource admission
returns 429/Retry-After and survives receipt reload with its error classification.
There is no new durable task queue or automatic execution retry. Auto Agent startup
does not launch its terminal fallback after resource enforcement or output failure;
the original failed structured session is retained for inspection.

### Lifecycle and transport / 生命周期与流式传输

Unix process handles now kill the owned process group after root Wait and before
reporting session completion. Windows retains atomic Job Object ownership and
kill-on-close. Descendants that deliberately detach into another process group
are outside the Unix guarantee. No recovery code kills a persisted PID: PID reuse
must not turn recovery into termination of an unrelated process. SIGKILL/OS crash
on Unix still requires OS-level containment; startup marks sessions interrupted
without resurrecting or blindly reaping unidentified processes. Output persistence
failure now triggers the same supervised termination path rather than allowing an
unobservable process to keep running.

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

### Adapter and diagnostics / 扩展与诊断

A narrow internal agentAdapter interface and shared CLI descriptors centralize
executable/protocol/arguments/version requirements for live sessions and history.
Codex/ACP retain their provider-specific wire behavior and shared SDK state machine.
Capabilities add an `agents` map with path/installed/protocol/protocol_version and
probe_status=not_probed; these describe requirements, not successful negotiation.
Actual availability remains gated by the existing live handshake.

`nimotehost agents` explicitly probes installed structured adapter executables
with --version, with a two-second deadline each plus process cleanup, using the supervised native backend and
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
  resource_limits_v1, output_resync_v1; hard_cpu_limit/hard_memory_limit are false.
- Session metadata schema is unchanged. Receipt resource_limited is optional;
  the existing ledger decoder ignores unknown fields. Owner receipt identity and
  retention watermarks are unchanged. Device principal namespaces affect only
  the newly supported device mutations.
- Existing observer JSON remains byte-schema compatible (sessions omitted).
  Old Hosts reject new role records instead of treating them as unrestricted
  observers. Old ListDevices may fail on these records; this is a safe refusal,
  not a supported transparent management downgrade.
- resources.json is additive; older binaries ignore it and do not enforce these
  guards. Do not downgrade while relying on resource guards or scoped devices.
  Stop/await cleanup, back up private state, revoke scoped keys and remove their
  SSH authorizations before an intentional binary downgrade. Retain the current
  ledger, retirement watermark and revocations when upgrading back; never restore
  an older snapshot over newer mutations or revocations. The backup is for
  reviewed recovery, not an automatic state rollback. Do not delete request
  receipts to make rollback work.
- UI/client code and third-party dependencies are unchanged. The lifecycle test
  now compares stable status fields plus instance identity, because free disk is
  intentionally dynamic; it also verifies restart changes the instance ID.

中文结论：旧客户端继续使用旧接口；新增能力需要协商。不做破坏性数据迁移，
不自动清理未知文件/凭证/任务，不把受限 operator 或应用层限流描述为 OS 沙箱。
