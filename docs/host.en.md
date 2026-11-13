# Nimote Host

**English** | [简体中文](host.md)

## Install and run

Host is an independent Go module for tasks, terminals and Agent CLIs over an
existing authenticated SSH connection. It requires no Nimote account or public
relay. Services run as the current user on Linux, macOS and Windows.

From the module root:

```sh
go install ./cmd/nimote-host
nimote-host start           # foreground; no account binding or control plane
nimote-host capabilities    # JSON capabilities and detected CLI paths
nimote-host status
nimote-host stop
nimote-host install         # install and start the per-user service
nimote-host restart
nimote-host uninstall       # stop/remove service; retain records and credentials
```

Put the binary at a fixed path on the remote SSH user's PATH before installing.
Do not move a binary referenced by an installed service. Linux uses systemd user;
use foreground mode without a user service manager. Configure user lingering
according to system policy if it must run after logout. macOS uses a launchd
LaunchAgent. Windows uses a current-user Task Scheduler InteractiveToken logon
task with LeastPrivilege; no administrator privileges are required. It is not a
LocalSystem service, and operation after logout is not guaranteed. Windows
terminals use ConPTY (Windows 10 1809+) and process trees use Job Objects.

On Linux/macOS, installation saves absolute PATH entries and XDG_CONFIG_HOME.
After tool paths change, let tasks finish, then run stop and install to refresh.
Windows tasks use the login environment. Check capabilities for actual tool paths.
Host does not download Agent CLIs or bypass their login and approval requirements.

Host listens only on `127.0.0.1:47831`: one active instance per machine/network
namespace. State is in `os.UserConfigDir()/nimote-host`. Unix directories use 0700
and files 0600; Windows uses protected DACLs restricted to the current user.
Every endpoint, including capabilities, requires a random 256-bit local token.
Host ID is independently generated and persisted. A file lock covers startup,
recovery and shutdown. Keep the token out of phone settings, arguments and logs.

## RPC v1

Every HTTP request requires `Authorization: Bearer <local token>` and
`Nimote-Host-Protocol: 1`. Errors use non-2xx JSON: 426 for incompatible protocols,
401 for missing authentication and 403 for browser Origin requests. CORS is not
provided. JSON writes require `Content-Type: application/json`. Requests are
limited to 1 MiB; unknown fields, multiple JSON values and invalid session IDs
are rejected.

Prefer an authenticated SSH exec from mobile clients:

```sh
printf '%s' '{"method":"GET","path":"/v1/capabilities"}' | nimote-host rpc
```

Cross-platform clients can use `nimote-host rpc-encoded <base64(JSON)>`. Pass the
encoded data as one safely quoted argument; do not interpolate commands,
directories or input into the SSH shell. The argument is not an authentication
secret but may contain task content, so do not include secrets. The remote CLI
reads the local token and calls the fixed loopback address with proxies and
redirects disabled. SSH forwarding is also possible, but requires secure access
to the token; SSH does not replace RPC authentication.

| Method/path | Behavior |
| --- | --- |
| GET /v1/capabilities | version, protocol, min_protocol, platform, arch, host_id, features, tools |
| GET /v1/agent-history?agent=...&directory=...&cursor=... | Paginated native Codex/OpenCode/Claude sessions; gated by provider capabilities |
| GET /v1/agent-history/{conversation_id}?agent=...&directory=...&cursor=... | Native transcript and tool history |
| GET /v1/status | Runtime status without credentials |
| GET /v1/sessions?after=revision | Full or changed session snapshots |
| POST /v1/sessions | Create a task, terminal or agent |
| GET /v1/sessions/{id} | Read session status |
| DELETE /v1/sessions/{id} | Remove a finished session and its output |
| POST /v1/sessions/{id}/cancel | Terminate its managed process tree |
| POST /v1/sessions/{id}/input | `{"data":"base64 bytes"}` for an active terminal |
| POST /v1/sessions/{id}/resize | `{"rows":40,"cols":120}` |
| GET /v1/sessions/{id}/output?offset=0 | base64 data, start_offset, next_offset, retention_bytes; absolute byte cursors; wait_ms=0..1000 waits for output |
| GET /v1/events?after=revision | events, cursor, resync_required; most recent 1024 persisted state events |
| POST /v1/shutdown-if-idle | Atomically stop only when idle; active processes return 409 |
| POST /v1/shutdown | Authenticated graceful shutdown |

Example request:

```json
{"method":"POST","path":"/v1/sessions","body":{"kind":"task","command":"go","args":["test","./..."],"directory":"/absolute/workspace"}}
```

Use `kind: terminal` for a terminal, or `kind: agent` with
`agent: codex|claude|opencode` for an Agent. Other arguments are passed to the
installed CLI using its native interactive terminal behavior, not private
protocols. Windows `.cmd/.bat` wrappers use `cmd.exe` for simple arguments;
wrapper paths/arguments with shell expansion characters are rejected. Use an
explicit `cmd.exe` command for complex shell tasks. Native executable arguments
use Win32 argv quoting.

Creation generates a random session ID: save it after a successful response.
There is no idempotency key. If a response is lost, list sessions before retrying
to avoid duplicate tasks. Processes run as the current OS user without elevation.
The token grants that user's command-execution authority; there is no multi-tenant
role system.

Tasks and terminals are independent of HTTP/SSH request contexts. Limits are 32
active processes and 256 retained sessions. Each session retains its **latest
16 MiB** of output in a bounded circular log (plus a 24-byte header), overwriting
the oldest bytes while continuing to capture live output. Legacy prefix `.log`
files remain readable; new sessions use `.ring`. A `start_offset` greater than
the requested offset signals expired history; reset the UTF-8 decoder at that
gap. Output retention does not block the child. `output_bytes` and `output_truncated` report the total and truncation.
Output is written to files and synced on normal completion; metadata is updated
with a same-directory temporary file, Sync and Rename. The last output bytes may
be lost on sudden power failure. Delete completed sessions to reclaim space.
Refresh all sessions when an event cursor is too old. Logs record IDs, kinds and
states, not command contents or authentication values.

Client disconnection does not stop a process. **Host/OS restart does not recover
live processes**: unfinished records become interrupted, and persisted PIDs are
not blindly signalled. Stopping Host terminates managed process trees. On Unix,
malicious or daemonizing programs that escape their process group/session fall
outside the normal tree-termination guarantee.

## MCP and integrations

MCP uses the official [`modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk) v1.8.0. ACP history connections use the community [`coder/acp-go-sdk`](https://github.com/coder/acp-go-sdk) v0.13.5 for request matching, cancellation and ordered replay. Host retains tool authorization, process ownership and event adaptation.

`nimote-host mcp` serves stdio MCP with the `2025-11-25` initialization lifecycle;
newer clients must allow negotiation to that version. Stdout contains JSON-RPC
only; errors go to stderr. The default tools are host_capabilities, host_sessions
and host_output. Setting `NIMOTE_HOST_MCP_WRITE=1` locally adds host_start and
host_cancel. Writes still use Host RPC authentication, quotas, path and process
checks. MCP does not start the daemon, automatically open network endpoints or
accept arbitrary URLs.

References: [MCP lifecycle](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2025-11-25/basic/lifecycle.mdx),
[MCP tools](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2025-11-25/server/tools.mdx),
[ConPTY lifecycle](https://learn.microsoft.com/en-us/windows/console/creating-a-pseudoconsole-session),
and [Job Object startup attributes](https://devblogs.microsoft.com/oldnewthing/20230209-00/?p=107812).

SFTP belongs to the client. Host does not implement SFTP, automatic cross-host
file transfers or system push notifications. Optional structured Codex approvals
are described below.

## Build and acceptance

```sh
./scripts/build-host.sh linux amd64
./scripts/build-host.sh darwin arm64
./scripts/build-host.sh windows amd64
go test ./...
```

CI configures three native platforms, including PTY/ConPTY tests, and six
cross-build targets. Committed CI configuration is not evidence of completed
runner execution. Native macOS/Windows services and real mobile-device handoff
still require acceptance. Build support and native runtime acceptance are
tracked separately. See [maintenance](maintenance.md) for distribution upgrades.

## Persistent SSH RPC

When `features.rpc_stream` is true, `nimote-host rpc-stream` supports one persistent SSH exec channel without a PTY. Frames are UTF-8 JSON lines: greeting `{"stream_protocol":1}`, request `{"id":1,"method":"GET","path":"/v1/status"}` (optional `body`), response `{"id":1,"result":...}` or `{"id":1,"error":"..."}`. Up to eight requests may run concurrently; match responses by ID rather than order. Authentication stays on the host. Never replay mutations after an ambiguous failure. Closing the bridge detaches without canceling daemon-owned sessions. Output waits and input requests can proceed concurrently; older hosts retain one-shot RPC compatibility.


### Structured Codex / OpenCode / Claude sessions and upgrade compatibility

The design follows the protocol boundaries used by [Zed External Agents](https://zed.dev/docs/ai/external-agents)
and [JetBrains ACP](https://www.jetbrains.com/help/ai-assistant/acp.html). ACP adapters
negotiate versions and capabilities. This Codex adapter uses the official
[app-server stable surface](https://learn.chatgpt.com/docs/app-server), without
experimental API opt-in, TUI scraping, or private interfaces. OpenCode uses official
[opencode acp](https://opencode.ai/docs/cli/acp/) with explicit ACP v1 negotiation.

Linux/macOS/Windows 10 1809+ clients check codex_structured, codex_structured_auto and agent_events_v1,
For OpenCode, use opencode_structured / opencode_structured_auto and agent: opencode.
Clients request `{kind: "agent", agent: "codex", interaction: "auto", directory: "/workspace"}`.
Host completes initialize and thread/start (ACP: session/new) before accepting a user task. Failed,
malformed or timed-out (all agents: 45 seconds) startup stops and removes the probe session,
then starts a native terminal. The result reports the selected interaction and
compatibility_fallback reason. Probe waits never hold the manager lock. Each new
session probes the installed binary; no CLI-version allowlist is hardcoded.

Explicit structured mode is available to integrations requiring it. Omitting
interaction, terminal mode, old Host installations and terminal resume
remain compatible. Auto/structured modes do not accept CLI args. Clients provide
an explicit terminal option and an always-available terminal entry from structured
sessions. A running turn is never automatically replayed, canceled or migrated.
The manual terminal entry creates an independent session.

`POST /v1/sessions/{id}/agent-message` accepts prompt, interrupt, approval and a
required request_id (up to 128 bytes). Prompts carry text; approvals carry approval_id
and decision: accept|decline. Request IDs are deduplicated for the running session;
after 4096 unique operations, new prompts require a new session. Approvals and
interrupts for the active turn remain available and their IDs are retained for
deduplication. Input pipe failure closes the adapter and terminates its managed
process. Ended-session snapshots report neither ready, busy nor pending approvals.
Interrupt stops a turn;
cancel ends the process. Raw input and resize are unavailable.

The existing byte-cursor output API returns base64 UTF-8 JSONL records with type/data:
ready, user, text_delta, message_completed, tool, turn_started, turn_completed,
approval, approval_resolved, turn_rejected, error. Text uses id/text; tools use
id/kind/title/status/output/changes. Changes always contain path/diff entries.
Approvals use id/kind/command/cwd/reason/changes. Provider method names and tagged
unions do not cross the adapter boundary. Unknown optional events and fields are
ignored; unsupported interactive requests get an explicit error, never approval.
The v1 event contract supports additive extensions; breaking changes require a new
capability version while preserving v1 clients.

`GET /v1/sessions/{id}/agent-state` supplies cursor, ready, busy and pending approvals.
Apply snapshots only when the replay cursor equals their cursor. Transport still
uses output long polling over the existing persistent RPC stream.

`agent_history_v1` and provider-specific `codex_history_v1`, `opencode_history_v1`, and
`claude_history_v1` capabilities are advertised when each native adapter is available.
Host uses Codex app-server thread APIs and OpenCode/Claude ACP session APIs to list,
read and resume native history; it does not inspect provider-private files. The opaque
`conversation_id` is distinct from a Host process session ID. Resume a selected
conversation with `resume: {"conversation_id":"..."}` in `POST /v1/sessions`; the new
Host session stores and returns the conversation ID. ACP loads use the original
workspace directory. A failed resume never falls back to a new conversation.

Detachment leaves the process and approvals running. Host restart marks unfinished
sessions interrupted; automatic thread restoration is not implemented. Command and
file-change approvals are supported. Output retains the existing 16 MiB window;
client history is bounded to 500 entries and about 2 Mi characters. Claude uses the maintained claude-agent-acp adapter when installed; otherwise it
retains terminal mode. Compatibility fixtures cover startup
failure/timeout, additive changes, unknown notifications, normalized events and
process cleanup; they do not guarantee interoperability with every future CLI.


ACP session/prompt completes only when its stopReason response arrives. Cancel is
sent as session/cancel with cancelled outcomes for pending permission requests;
new prompts wait for completion. Confirmation selects allow_once only, never
allow_always. Rejection selects reject_once or a cancelled outcome. Unsupported
client filesystem/terminal/elicitation calls receive explicit JSON-RPC errors.
Partial tool updates preserve descriptions and diff previews. Installed OpenCode
handshakes can be tested with NIMOTE_TEST_INSTALLED_OPENCODE=1, without model calls.

OpenCode 2.3.0 auto-mode startup has been smoke-tested against the official npm binary,
with isolated XDG directories and no model request.


### Claude adapter and native Windows pipes

Claude uses the maintained [claude-agent-acp](https://github.com/agentclientprotocol/claude-agent-acp)
via the same ACP v1 adapter. Features claude_structured / claude_structured_auto
are advertised only when that command is installed on PATH. Host never installs,
updates or invokes adapters through npx/latest. Prefer a pinned standalone release
with its bundled Claude CLI; npm package @agentclientprotocol/claude-agent-acp@0.106.0
requires Node 22+. Authentication and model configuration belong to the adapter;
Host does not automatically log in or replace credentials. Native claude remains
the terminal fallback.

Windows structured processes use separate redirected stdin, stdout and stderr
pipes, without ConPTY. CreateProcess Job List attributes atomically supervise the
whole process tree; only the three explicit standard handles are inherited.
Existing executable and batch-launcher quoting restrictions still apply. Native
Windows tests cover all three agents, .cmd wrappers, spaces in paths, large Unicode
and multiline payloads, stderr separation and shutdown. They run in the existing
host-native Windows CI job; local cross-compilation is not native runtime validation.

Standalone Claude adapter 0.106.0 passed real ACP session initialization on macOS,
without a model request. Cold starts receive a uniform 45-second initialization budget;
terminal fallback remains available, and every new session probes again.


### Cold-start deadlines

Feature agent_cold_start_wait advertises this fix. Only auto structured-agent creation gets the longer deadline chain: initialization
45 seconds, bridge HTTP client 60 seconds, server response deadline 65 seconds,
and App one-shot/persistent RPC at least 75 seconds. Ordinary I/O, status, connection
negotiation and other creates retain their short deadlines. Explicit protocol
rejections or process exits still fall back immediately; mutations are never replayed.
A real delayed subprocess/socket regression exceeds the legacy initialization and
HTTP deadlines, alongside virtual-clock tests for all agents and both transports.
Both Host and App must be updated for the complete deadline chain.
