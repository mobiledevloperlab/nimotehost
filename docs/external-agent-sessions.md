# External Agent session discovery and continuation

Updated: 2026-11-17. Supported connection paths are implemented, but native-platform and phone acceptance still has gaps. History reads, resuming an original ID, and controlling an existing process are separate capabilities.

## Architecture and reuse

| Area | Existing implementation and additions |
| --- | --- |
| Manager | Reuses `StartRequest`, `Session`, `resumeMu`, `resumeExisting` and admission; no second session system. |
| Adapters | `externalSessionAdapter` reuses Codex/ACP listing/history; optional `externalLiveSessionAdapter.AttachSession` connects existing services. `ExternalAttachment` credentials are separate from persistable `ExternalConnection`. |
| Codex | Existing launches use app-server stdio; attachments use WebSocket over AF_UNIX to the same running app-server. Windows reuses the standard library and `x/sys/windows`; SDK retains JSON-RPC ordering, correlation and approvals. |
| OpenCode | Existing ACP remains. HTTP v1 and v2 `/api` HTTP/SSE share state, approvals and event projection. SSE triggers authoritative snapshots; periodic snapshots recover durable messages and pending approvals missed during disconnect. |
| Claude | Official `claude agents --json` discovery is independent of ACP and reuses `runEvidenceProcessBounded`. ACP v2 history explicitly supplies `replayFrom` and filters replay-end/control snapshots. |
| Ownership | External handles implement `processHandle` without owning external processes. `Kill/Close` detach; only explicit `AgentMessage.interrupt` requests interruption. |
| Synchronization | Reuses `AgentMessage`, approval cards, `AgentSnapshot`, output byte cursors, activity sequence, event revisions and durable receipts. |
| Persistence | Existing atomic writes/reservations remain. Optional Session `external` stores credential-free identity. Restart marks old records interrupted; clients explicitly reconnect the native ID. |
| Access | Existing loopback HTTP, SSH RPC/streams and device ACL remain. Discovery/connection creation are owner-only; scoped devices retain their original role checks. |
| Automation | Attachment runs no extra verification and changes no native approval, sandbox, workspace or model configuration. |

Long-lived workers, correlation and reconnect ideas were reviewed in [sessions.mjs](https://github.com/oxmumu3/ag-mobile-bridge/blob/main/lib/sessions.mjs) and [acp-hub.mjs](https://github.com/oxmumu3/ag-mobile-bridge/blob/main/lib/acp-hub.mjs); private-database guessing, history-injection handoff and public tunnels were not ported.

Protocol references: [Codex app-server](https://github.com/openai/codex/tree/main/codex-rs/app-server), [OpenCode v2 API](https://opencode.ai/docs/api), [v2 protocol](https://github.com/anomalyco/opencode/blob/dev/packages/protocol/openapi.json), [ACP sessions](https://github.com/agentclientprotocol/agent-client-protocol/blob/main/docs/protocol/v1/session-setup.mdx), [Claude Remote Control](https://code.claude.com/docs/en/remote-control). Main-branch documents do not establish compatibility with every installed version.

OpenCode v1 follows official v1.19.6 [session routes](https://github.com/anomalyco/opencode/blob/v1.19.6/packages/opencode/src/server/routes/instance/httpapi/groups/session.ts), [permissions](https://github.com/anomalyco/opencode/blob/v1.19.6/packages/opencode/src/server/routes/instance/httpapi/groups/permission.ts) and [message schema](https://github.com/anomalyco/opencode/blob/v1.19.6/packages/schema/src/v1/session.ts). Windows reads existing security descriptors following [Microsoft AF_UNIX guidance](https://devblogs.microsoft.com/commandline/af_unix-comes-to-windows/).

JSON-RPC uses `acp-go-sdk`, WebSocket uses `coder/websocket`, HTTP/Basic/JSON/timeouts/streams use the standard library, and Windows permissions use existing `x/sys/windows`. Custom logic only validates identity/authorization, maps official fields, projects capabilities and defines reconnect behavior. SSE does not introduce a separate replay or queue protocol. The official OpenCode SDK is JavaScript/TypeScript; this Go implementation adds no Node process.

## Capability matrix

| Agent/transport | Discovery/history | Original-ID resume | Existing-session control |
| --- | --- | --- | --- |
| Codex stdio | `thread/list`, `thread/items/list` | `thread/resume` with ID validation | No arbitrary stdio/PTY attachment |
| Codex `codex-control` | Native list/replay through an explicit existing socket | Validate ID/workspace/loaded state, then resume on the same service using only threadId | Events/messages/approvals/interrupt implemented; macOS CLI 0.169.0 tested with local model fixture; Windows AF_UNIX/read-only ACL tests compile, Linux/Windows native execution pending |
| Claude `claude-cli` | Official foreground/background JSON, workspace, IDs, process/wait state; no transcript | Not through this transport | Discovery only. Official terminal attach lacks this transport's structured approval IDs and safe attach-only handshake |
| Claude ACP | `session/list`, history through `session/resume` with `replayFrom:{type:start}` | ACP v2 `session/resume` | A new ACP process resuming history is not control of an independent CLI. Real 0.107.3 initialization/list/new-session handshake passed; history/tool/model matrix pending |
| OpenCode ACP | Existing list/load replay | ACP v1 `session/load` | Does not claim control of another process |
| OpenCode `opencode-http-v1` | Existing `/session`, `/session/status`, messages/tools | Same session ID, no create/fork | `/prompt_async`, native `/permission`, once/reject, `/abort`, SSE/snapshot recovery; fixtures passed, real version pending |
| OpenCode `opencode-http-v2` | Existing `/api/session`, state and paginated messages | Same ID, no create/fork | Prompt/pending/once/reject/interrupt/SSE/snapshot recovery implemented; fixture-only acceptance |
| Antigravity and others | No adapter | Unsupported | Unsupported |

States are `supported/conditional/unsupported`. Installation or a listed ID does not prove live control. Legacy `resumable` remains, but resume requires a successful handshake. `live_transports` enumerates explicitly selectable paths; history is not marked controllable without endpoint/native-session evidence.

ACP history uses provider load/resume replay and reports `history_mode=native_load_replay`, not a side-effect-free file read. Existing-service reads report `native_read`. Host never directly writes third-party databases, configuration or session files.

## API and connections

Protocol remains `1`. Negotiate `external_session_metadata_v1` and `external_session_attach_v1` before sending new fields. Owner-only `POST /v1/agent-history` accepts agent, optional directory/cursor and connection, returning existing `items/next_cursor`:

```json
{"agent":"opencode","directory":"/absolute/workspace","connection":{"transport":"opencode-http-v2","endpoint":"http://127.0.0.1:4096"}}
```

Protected OpenCode uses native Basic authentication from request-body `username/password`, default username `opencode`. Credentials never enter URLs, argv, responses or Session records. Endpoints must be literal loopback IPs with explicit ports; credentials, paths, queries, redirects, proxies and DNS names are forbidden.

Claude discovery uses the same owner endpoint:

```json
{"agent":"claude","directory":"/absolute/workspace","connection":{"transport":"claude-cli"}}
```

Directory is optional. `claude-cli` rejects endpoints/credentials and cannot be used for `StartRequest.attach`. `claude_live_discovery_v1` means the CLI is in PATH; `discovery_transports` lists it without promising installed-version support. Failure does not start a replacement service. `conversation_id` is the optional original UUID, `external_session_id` the optional background short ID; missing UUIDs stay empty rather than borrowing a PID. `native_kind/native_pid/native_state/native_status/waiting_for` retain official state with `status_source=claude_agents_json`. Native waiting does not create an actionable approval handle; `working` without a live process does not prove execution.

Discovery budgets: four seconds, 2 MiB, 2000 rows, existing process-tree cancellation, 50-row pages. Missing cursor objects require refresh. Duplicate identities, malformed JSON and incompatible structures fail without reading private session files. Results report `resumable=false`, `live_control=unsupported`; ACP resume is separate. Results are snapshots, not live subscriptions.

`POST /v1/sessions` adds optional `attach`, requiring the original native ID/workspace and `interaction=auto` or `structured`. Auto waits for full handshake:

```json
{"kind":"agent","agent":"opencode","interaction":"auto","directory":"/absolute/workspace","request_id":"client-generated-unique-id","resume":{"conversation_id":"ses_original"},"attach":{"transport":"opencode-http-v2","endpoint":"http://127.0.0.1:4096"}}
```

For v1 use `opencode-http-v1` against the user's existing service. Host never runs `opencode serve`, since a new service cannot prove attachment to the original TUI. Every v1 request carries the original directory; global pending is filtered to the session, and messages/parts/tools retain validated original IDs. Lists page by native ID, removed cursor objects require refresh, and changing upstream lists provide no transactional snapshot. Full v1 message reads are bounded: more than 2000 messages or response budget fails rather than claiming truncated history is complete.

Codex uses `agent=codex`, `transport=codex-control` and an absolute existing socket path. Linux/macOS resolve official long-path rendezvous symlinks read-only, validating alias-directory and private-socket ownership/permissions without chmod or service startup. Windows uses standard-library AF_UNIX and read-only owner/DACL checks on socket/parent, rejecting other ordinary principals' write or socket-content-read rights; administrators and SYSTEM are trusted. A read-only parent handle denies delete sharing until handshake completes; parent reparse points are rejected. Host never calls file-preparation code that changes third-party ACLs. A supported Windows AF_UNIX environment and verifiable permissions are required; native acceptance remains pending. Missing sockets, incompatible versions, unloaded threads and workspace mismatch fail explicitly.

Connected sessions reuse `agent-state`, output, activity, `agent-message` and `/v1/events`; no arbitrary RPC forwarding endpoint is added.

| Field | Meaning |
| --- | --- |
| `session_capabilities` | Separates discovery/history/resume/live control, history mode and transports |
| `origin` | `external_history`, explicit-service `external_live`, or matched `host_managed` connection |
| `execution_status/status_source` | Protocol evidence only; history is unknown, never inferred from timestamps |
| `host_session_id` | Live connection matching Agent, ID, workspace and explicit transport identity; reconnect uses original Host ID |
| `external_connection` | Credential-free discovered connection identity |
| Session `external` | Persisted credential-free identity; old clients retain existing fields. Older strict readers may reject it: retain backups and compatible binaries before downgrade |
| Aggregate `partial` | Transient failures retain provider cursors without rereading completed providers; persistent failures need backoff |

Existing GET history remains; omitted agent defaults toward Codex and `agent=all` aggregates three providers. Ties sort by Agent/ID and the largest aggregate cursor round-trips.

## State, approvals and reconnect

- Concurrent attach reuses `resumeMu` and native-ID matching; endpoint/workspace/task/policy/identity conflicts reject rather than create parallel controllers.
- Only genuine pending approval IDs are actionable. OpenCode sends `once/reject`, never `always`. Duplicate consumption and changed content under the same request ID fail. Uncertain network writes are retained and never resent automatically.
- External connections remain `confirm` only; smart/auto cannot be enabled afterward, preserving native approval and reconnect identity. Existing Host-launched policies remain compatible.
- Codex retains native request IDs for desktop `serverRequest/resolved`; an earlier resolution never recreates pending approval. Unknown native questions remain desktop-owned rather than failing the task with MethodNotFound. Capacity exhaustion detaches only.
- OpenCode recalibrates state/pending before control; SSE wakeups coalesce and snapshots project in order. Content hashes prevent duplicate message events; durable upstream SSE replay is not assumed.
- Snapshot/HTTP requests each have four-second budgets; responses ≤2 MiB, snapshots ≤2000 messages/4 MiB, pending ≤64. Limits fail explicitly. Frequent SSE wakeups are coalesced to intervals of at least 250 ms.
- Client disconnect leaves Host connections running. OpenCode reports disconnected and rereads state/messages/pending to recover; Codex requires explicit reattach. After Host restart, either can reconnect the same native ID without replaying input or approval.
- `/cancel` detaches external connections. Interrupt requires explicit `AgentMessage.interrupt`; Host shutdown/quota cleanup sends neither decline nor interrupt and does not fabricate task completion.
- Native services own lifecycle after subscribers leave. Real Codex survival tests retain the desktop connection and make no promise about the last subscriber disappearing.
- External processes are outside Host identity/kernel-quota ownership, so attach rejects those configurations. There is no global desktop/phone writer lease; native services arbitrate writers.

## Implementation and acceptance status

Implemented: adapter contracts, existing Codex and OpenCode v1/v2 connections, Windows read-only socket security/AF_UNIX code, official Claude discovery, explicit ACP v2 history replay, ID/workspace validation, supported pending recovery, approval deduplication/out-of-order handling, projections/reconnect, resource/authorization checks and regressions.

Unsupported: arbitrary PTY/stdio without a control endpoint, structured attachment to an independent Claude process, Antigravity, context injection as a substitute for resume, or automatic user-service launch/restart.

The earlier conclusion that Claude lacked any local attach entrypoint was too broad. [Official Agent View](https://code.claude.com/docs/en/agent-view) provides JSON discovery and background terminal attach. Discovery is implemented; terminal attach may restart stopped sessions and lacks structured pending IDs, tool correlation and an attach-only handshake for this Host. Host never automatically runs `attach`, `--bg`, `respawn` or `daemon`, or infers approval from terminal text. Users can use official terminal attach themselves, but it is not Host structured takeover. [Remote Control](https://code.claude.com/docs/en/remote-control) uses official services rather than a LAN API. [ACP adapter](https://github.com/agentclientprotocol/claude-agent-acp) live-session resume refers to its own process objects, not independent CLIs. Not every Agent execution mode is supported.

Local installation: Claude Code 2.1.311 at `~/.local/bin/claude`, ACP 0.107.3 under `~/.local/share/nimotehost-claude-acp`, launcher `~/.local/bin/claude-agent-acp`. Upstream `CLAUDE_CODE_EXECUTABLE` reuses the CLI, preserving explicit user overrides. Existing npm packages add no Node dependency to the Go module. Global installation exhausted disk while unpacking optional binaries and rolled back; isolated installation succeeded and temporary caches were removed. Installation does not log in or run model work.

Pending: real OpenCode, broader Claude/ACP/external-model matrices, Linux/Windows native execution, phones/tablets/real SSH, long workloads and complex tools. The independent phone repository still needs new attach parameters; this Host change does not modify it. The only new direct dependency was cached `github.com/coder/websocket v1.8.14` for official handshake/framing/limits/close, not a new daemon, public service or tunnel.
