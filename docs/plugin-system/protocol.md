# Host Capability Protocol 1.0 — English / 简体中文

Normative, language-independent public contract: [spec/v1](spec/v1/README.md).
The spec directory can be copied/versioned/published independently of Host Go
internals. This change includes repository publication artifacts; it does not
create an external release, package-registry entry or a NimoteCode change.

Use the existing authenticated SSH bridge:

```json
{"method":"POST","path":"/v1/extensions","body":{"protocol":"1.0","operation":"capabilities.list"}}
```

`nimotehost rpc` accepts that envelope on stdin. `rpc-stream` uses its existing
correlation IDs. Loopback HTTP still requires `Nimote-Host-Protocol: 1`, an owner
Bearer credential and JSON Content-Type; clients should use SSH rather than
extracting credentials or exposing loopback publicly. Browser origins are denied.

| Operation | Additional fields | Result |
| --- | --- | --- |
| `capabilities.list` | none | entries including `id`, namespace `plugin`, kind, version, schemas, UI and state |
| `capabilities.get` | `id` = `plugin/capability` | entry |
| `capabilities.invoke` | `id`, `input`, `epoch`, unique 64-lowercase-hex `request_id`; `confirmed` for sensitive work | asynchronous invocation with `id`, capability, state |
| `capabilities.result` | invocation `id` | running / succeeded / failed / canceled, output or structured error |
| `capabilities.cancel` | invocation `id`, or community ACP Session ID | cancellation acknowledgement; poll final state |
| `capabilities.subscribe` | `epoch`, `after` cursor | event page, current epoch/cursor, `resync_required` |
| `plugins.list` | none | manifests, granted permissions, runtime states |
| `plugins.install` / `plugins.update` | absolute local bundle `source`, reviewed Manifest `sha256`, `confirmed:true` | acknowledgement; package remains disabled |
| `plugins.enable` | plugin `id`, exact `grants`, `confirmed:true` | acknowledgement; asynchronously starting |
| `plugins.disable` | plugin `id` | revoke grants, cancel work and managed ACP sessions |
| `plugins.uninstall` | disabled plugin `id`, `confirmed:true` | unregister/remove owned package |

Success: `{"protocol":"1.0","result":...}`. Errors use HTTP status plus
`{"code":"...","error":"..."}`; authentication 401, authorization 403,
protocol mismatch 426 with `supported_protocols`, invalid operation/schema/package
400, epoch/request conflict 409, admission 429/503. Invocation failures use
`{"code":"plugin_error","message":"..."}`; raw child errors are never returned.
Plugin state is disabled/starting/ready/recovering/failed. Enable acknowledgement
is not proof of availability: poll until ready.

Subscriptions are bounded polling subscriptions, not a WebSocket/SSE stream.
Start with an empty epoch, obtain the current epoch and snapshot on resync, then
poll `after=cursor` with that epoch. Store the cursor only after applying the page.
Changed epoch, future cursor or expired history requires rediscovery and querying
known invocations. Metadata includes plugin state, invocation state and numeric
0..1 progress. No input/result/credentials are stored in this journal.

For safe transport retry, first obtain the epoch from subscribe. Invocation
`request_id` deduplicates byte-identical input/capability/confirmation in that
epoch. Changed content conflicts. Retired results are never re-executed for the
same key. Retry keys are bounded and never evicted within an epoch. A changed epoch
rejects the previous request; Host restart makes prior outcomes uncertain, so do
not automatically resubmit side effects with a new epoch/key. Recover known
results within the current process using `capabilities.result`.

ACP agent capability invocation accepts `{directory,request_id}` as `input`,
requires confirmation and returns an existing Host Session instead of a finite
plugin Invocation. Use established session Agent controls/events/approval routes;
`capabilities.cancel` also accepts that community session ID. The executable's
`--acp` mode must implement the declared ACP v1/v2 using existing SDKs. Custom
terminal fallback is deliberately unavailable. Existing Task references remain
available through normal Session start; no new Agent prompt/approval protocol exists.

MCP exposes `host_capability_query` for read operations and
`host_capability_invoke` only when the existing MCP write option is enabled.
Plugin management is not exposed to MCP.
Online catalog/search/download operations are additive HCP 1.0 operations documented
in [the catalog specification](catalog.md), gated by `plugin_catalog_v1`. Manifest tools/tasks/events map through
the same generic invoke/query operations. Event capabilities return schema-defined
snapshots/batches on invoke; arbitrary unsolicited child notifications are not
supported in v1.

简体中文：HCP 1.0 使用已有 SSH RPC 安全通道，独立于 Host transport protocol 1。
查询、调用、取消、进度及订阅均无需插件专属接口。断线不取消已接受调用；重连使用
epoch、request_id 和结果 ID，重启后不得自动重放可能有副作用的操作。
