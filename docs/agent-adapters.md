# Agent adapter development

**English** | [简体中文](agent-adapters.zh-CN.md)

Host runs installed Agent CLIs as the current user. Adapters belong inside the
Host module; clients use documented RPC and capability fields. An adapter must
not require the Nimote control plane, download a CLI automatically, or embed
credentials. Propose new integrations in an issue before implementation.

## Current integration points

| Area | Files and responsibility |
| --- | --- |
| Request validation and command resolution | `internal/host/manager.go`: allowed Agent names, workspace and argument checks, lifecycle integration |
| Startup and fallback | `internal/host/agent_compatibility.go`: structured handshake, deadlines, cancellation and terminal fallback |
| Agent transport | `internal/host/agent_rpc.go`: shared SDK connection, bounded dispatch, approval replies and cleanup |
| Structured conversation | `internal/host/agent.go`: shared state and snapshots; `internal/host/codex_agent.go`: Codex app-server handling; `internal/host/acp_agent.go`: ACP handling for OpenCode and Claude |
| Platform support | Platform-specific terminal and structured-process files in `internal/host/`; preserve build constraints and process cleanup |
| Public discovery | `internal/host/protocol.go`: request/session fields, tools and feature flags |
| RPC operations | `internal/host/server.go`: validation, authentication boundary and semantic operations |
| Native history/resume | Provider-specific history implementation where supported; do not advertise another provider's history semantics |

The implementation currently uses explicit provider branches, not a public Go
plugin API. Search for all existing provider names before adding one. Update
`resolveCommand`, `supportsStructuredAgent`, `configureStructuredCommand` and
`newStructuredAgent` as applicable, plus discovery and protocol documentation.
For an ACP provider, reuse ACP handling only when its actual handshake and event
semantics match. Keep provider-specific parsing and command choices separate
from session storage and service installation. Propose an interface refactor
with concrete use cases before changing the shared lifecycle.

## Required behavior

- Launch the installed executable directly with explicit argv. Preserve workspace
  validation and authentication. Never pass prompts through a shell command.
- Distinguish terminal, structured and auto modes. Probe readiness before accepting
  a user turn; a failed probe can fall back only after its process is cleaned up.
  Never replay a dispatched prompt or mutation in another adapter.
- Cancellation and shutdown must terminate owned processes and pipes, release
  waiting readers and preserve the session's final state. Avoid holding manager
  locks during slow handshakes or network/process I/O.
- Capabilities describe implemented support; executable discovery and a successful
  runtime handshake are separate checks. Do not advertise unified approvals,
  history or resume unless their full semantics are implemented and documented.
- Preserve bounded output, replay offsets and durable event ordering. A failed
  resume must not silently start a new conversation. Handle unknown upstream
  messages without weakening request validation or executing unapproved actions.

## Validation and PR evidence

Use fake protocol peers and temporary executables, following
`acp_agent_unix_test.go`, `codex_agent_unix_test.go`,
`agent_start_deadline_unix_test.go` and their platform-independent counterparts.
Cover successful startup, missing executable, incompatible/malformed protocol,
startup timeout, cancellation during probing, process exit, concurrent output,
and cleanup. Exercise terminal fallback and prove prompts are not replayed.
Add provider-specific approval, history/resume and reconnect checks if supported.

Run the contribution checks and affected native-platform CI. Record the exact
CLI/adapter versions used for any real Agent smoke test and distinguish fake-peer
coverage from real authentication/model tests. Keep tokens and private output out
of fixtures. Update both protocol guides (`host.en.md` and `host.md`), capability
and installation notes, and third-party notices when introducing dependencies.
