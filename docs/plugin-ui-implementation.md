# Plugin UI implementation record

Both accessible repositories implement the additive Schema UI 1.0 changes. This record describes local work on 2026-11-14, not a new deployment or physical-device acceptance. The plugin manager, Executor, SSH and native core components were reused. Web/MCP Apps remain evaluation; this is not a complete three-layer platform.

## Host changes

- `pkg/pluginsdk/plugin_ui.schema.json` is the single machine-readable authority. `plugin_ui.go` reuses `jsonschema-go` for structure, namespace, action and budget checks.
- `Manifest.Validate` checks UI metadata through existing installation/enable/discovery paths. No new execution or event endpoint exists. Legacy `ui.layout/result` remain compatible; the new 32 KiB limit applies to contract-bearing UI only.
- `examples/system-monitor/manifest.template.json` exercises six tabs, nested sampling forms, collapsible details, responsive tables and linked selection, with live snapshots and simulated stress data.
- SDK positive/negative fixtures, budget/compatibility tests and schema-dependency documentation were added.

## Client changes

- `extensions/plugin_ui/contract.dart`: immutable parsing, bounded trees, RFC 6901 pointers and unknown-node policy.
- `runtime.dart`: in-memory page state, declarative argument mapping and same-plugin dispatch through `HostCapabilityExecutor`. Existing approval callbacks are reused; leaving during approval refuses dispatch. Disconnect, epoch changes and signature changes do not replay work.
- `renderer.dart`: precompiled shared controls for column/responsive/group/tabs/text/detail/form/button/table. Tabs preserve mounted state; tables provide search, numeric/text sorting, 20-row pagination, selection and details. Parent rebuilds retain form/runtime state.
- `structured_input.dart` and the optional `HostCapabilityForm.structured` flag: nested objects, dynamic optional fields, editable arrays and submit-time validation. Open dictionaries and over-budget input use JSON. Legacy forms default to `structured=false`.
- `HostPluginView`: existing plugin center/sidebar integration, legacy fallback, and unchanged task cancellation/recovery/error/progress UI.
- SHA256 pins cover schema, positive/negative fixtures and template. `check_plugin_ui_contract.py` compares complete bytes against a neighboring Host checkout; ordinary Flutter tests validate local pins.
- `host_plugins_acceptance.py --plugin-ui` covers discovery and the runtime approval/invocation chain against a real Host process.

## Recorded checks

| Check on 2026-11-14 | Result |
| --- | --- |
| `flutter test test/extensions test/host test/ui/ui_component_governance_test.dart` | 567 passed; 16 existing conditional skips, not counted as passes |
| Final runtime/renderer/nested-form/legacy-form/UI governance regressions | 32 passed; invalid nested JSON tested separately |
| `dart run tool/check_ui_components.dart` | 783 source files, zero violations |
| `flutter analyze` for changed Dart implementations/tests | No findings |
| `python3 tool/check_plugin_ui_contract.py --host ../nimotehost` | Pins match Host bytes |
| `go test ./pkg/pluginsdk ./internal/plugins ./internal/host` | Passed; Host approximately 71 seconds |
| `go test -race ./pkg/pluginsdk ./internal/plugins ./examples/system-monitor ./tools/pluginpack` | Passed |
| `go vet ./pkg/pluginsdk ./internal/plugins ./internal/host` | Passed |
| Real Host/RPC integration and Inspector/packer tests | Passed |
| `python3 tool/host_plugins_acceptance.py --host-repo ../nimotehost --plugin-ui` | Real Host/plugin processes passed; public HTTP disabled |
| `git diff --check` in both repositories | Passed |

Coverage includes positive/negative fixtures, cyclic objects, depth/state budgets, invalid and cross-plugin actions, approval rejection, leaving during approval, asynchronous results, deduplication, progress/cancel, uncertain-result recovery, epoch invalidation, legacy/unknown-node fallback, actual page approval, parent rebuilds, nested validation/array editing/disabled state and keyboard Tab. Widget tests exercise 390×844 phones, 844×390 landscape, 1024×1366 tablets, light/dark themes, 1×/2× text, and table filtering/pagination/sorting/selection.

Integration starts a temporary Go Host fixture and installs a real packaged Inspector through encoded RPC without changing production services. It checks discovery, no invocation after rejected approval, declared arguments, SSRF refusal, cancel, no replay after disconnect/restart, uninstall, and existing tasks/events/ACP. Successful result binding uses deterministic output; public-network availability is not a pass condition. This is not mobile-device or real-SSH-service acceptance.

## Limits and follow-up

- MCP Apps/isolated WebView are disabled. Resource provenance, isolation, CSP, instance checks, permissions and lifecycle tests must precede browser integration; see [architecture](plugin-ui-architecture.md).
- Only bounded snapshots and client pagination exist. No generic remote pagination, infinite stream, persistent/shared state or arbitrary event-driven actions; extensions must be versioned and retain the Executor.
- Visual forms support fixed properties and homogeneous arrays. Open dictionaries, complex unions/combinations and excess budgets use JSON. Validation occurs on submit, not necessarily every keystroke; full native JSON Schema editing is not claimed.
- Arbitrary native-view registration is unavailable. Existing terminal/editor/debugger entrypoints remain; unknown native/web nodes safely fall back.
- Same-account execution is not an OS sandbox; UI adds no downloaded Dart/native-code path.
- Physical devices, IME/platform WebView, store review, real SSH network disruption and full Local/AI/Terminal/IDE end-to-end behavior were not validated. Existing tests do not imply zero application regressions.
- At this implementation stage the repositories were not separately published. Pins identify content and the preimplementation baseline; actual releases must fix the intended revision and synchronize lock files and shared fixtures.

Each repository owns its implementation. The four UI documents record the same delivery, while only Host owns the JSON schema. Removing `ui.contract` restores legacy forms without changing grants, execution or task recovery.
