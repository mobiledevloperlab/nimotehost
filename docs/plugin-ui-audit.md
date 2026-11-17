# Plugin UI audit and decisions (2026-11-14)

## Review scope

Reviewed NimoteCode `lib/host/host_capability_{models,client,schema,executor}.dart`, `host_plugin_workspace.dart`, `lib/extensions/{host_plugin_view,host_capability_form,plugin_runtime,capability_gateway,plugin_manifest,plugin_table_data}.dart`, related host/extension/UI tests, and NimoteHost `pkg/pluginsdk`, `internal/plugins/{runtime,schema,catalog}.go`, `internal/host/plugins.go`, protocol specifications, the historical HTTP Inspector and tests. The two Host paths resolved to the same tree; the preimplementation baseline was `a6f28bde0973c2050277b432c6a19d1f7779fcf2`, and both working trees were initially clean.

## Preserved implementation

Host keeps default-disabled installation, fixed entrypoints, integrity checks, exact grants, process supervision, serial queues, timeout/cancellation, bounded restart and event journals. HCP 1.0 remains separate from transport protocol 1 over authenticated SSH RPC, with list/get/invoke/result/cancel/subscribe and epoch/request-ID replay protection. The SDK publishes input/output JSON Schemas and optional raw UI metadata.

`HostCapabilityClient` retains discovery, protocol checks, cursors and scope invalidation. `HostCapabilityExecutor` owns confirmation, postapproval signature rechecks, input/output validation, concurrency limits, result recovery and uncertain outcomes. `HostPluginWorkspace` isolates resources by SSH identity and suspends polling with application lifecycle. `HostPluginView` integrates navigation and task results. No new execution bypass is introduced.

The separate local panel system retains trusted panel identity checks in `CapabilityGateway`, `PluginStorage`, `PanelHost/ExtensionRegistry`, read pools, workflows and background jobs. Its identifiers, grants and result types differ from HCP; it is not merged or migrated.

## Limited extension

Legacy `ui.layout/ui.result` remain hints. Existing forms render top-level scalars and use JSON for nested objects/lists; result previews are limited to 100 rows/12 columns without bindings. Versioned `ui.contract` adds a bounded component tree, page memory state, JSON Pointer bindings and declared actions, while preserving legacy fallback and shared controls. No rewrite is required.

## Security constraints

`process.same-user` discloses risk rather than providing an OS sandbox. Malicious plugins retain account privileges, and hashes are not publisher signatures. Client schema support is explicitly limited and forbids references; it does not implement all JSON Schema features. UI has no expressions, remote resolution or implicit invocation; actions cannot cross plugin namespaces. Unknown components suppress their entire subtree. Sensitive input is not persisted, and leaving a page never replays accepted work.

The browser WebView has navigation/tunnels/lifecycle but lacks isolated plugin resources and a restricted RPC bridge, so it cannot serve directly as a safe plugin container. Host MCP query/invoke support is not MCP Apps hosting. Existing high-performance terminal/editor components remain client-owned; no Dart or native libraries are downloaded.

## Decision sequence

Publish the Host-owned schema and shared fixtures first, synchronize immutable client pins, then implement runtime/navigation and hostile-input/lifecycle tests. Web/MCP Apps remain evaluation until isolation is verifiable. Existing protocol needs no migration: removing `ui.contract` restores legacy presentation without changing execution or grants. Richer nested forms and remote pagination require versioned optional extensions; unimplemented capabilities must remain identified as such.
