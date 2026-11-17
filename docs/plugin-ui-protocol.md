# Plugin UI Contract 1.0

The authoritative schema is **`NimoteHost/pkg/pluginsdk/plugin_ui.schema.json`** (JSON Schema 2020-12). This document explains its semantics rather than defining a second schema. Client `test/host/fixtures/plugin-ui-1.0` contains byte-pinned mirrors; `lock.json` fixes SHA256 and the Host baseline. Do not edit client copies independently. UI and HCP versions are separate.

## Discovery and compatibility

Existing `POST /v1/extensions`, `protocol:"1.0"`, and `capabilities.list/get` return capability metadata. `ui.layout` and `ui.result` remain hints; optional `ui.contract.version` must exactly equal `"1.0"`. Host rejects unsupported versions during Manifest validation. Older peers can retain the UI object and use legacy input schemas without loading executable UI. Missing, invalid or incompatible contracts fall back to legacy forms. Unknown components display an unsupported placeholder without traversing/executing children; structure and action references are still checked.

There is no new execution endpoint, authorization path, implicit on-load invocation or timer. Compatible extensions use optional fields; incompatible changes require a new contract version, pinned artifacts and shared positive/negative fixtures. Removing `ui.contract` preserves input/output schemas and legacy hints; package updates still require disable, hash review and new grants.

## Fields

| Field | Meaning |
| --- | --- |
| `contract.version` | Exactly `"1.0"` |
| `contract.state` | Initial page JSON object; at most 64 named slots, memory only |
| `contract.actions` | At most 32 named actions targeting declared capabilities in the same Manifest |
| `contract.root` | Component-tree root |
| `action.capability` | Full `plugin/name`; never an arbitrary event-provided ID |
| `action.result` | State slot for successful results; failures/cancel do not overwrite prior data |
| `action.input` | Argument names mapped to `{pointer: RFC 6901 JSON Pointer}`; buttons read `{state: pageState}`, missing paths yield null, and target input validation still applies |
| `node.type` | Precompiled component name; unknown names safely fall back |
| `node.id` | Optional logical identifier; widget identity currently follows tree paths |
| `node.label/text` | Bounded plain text, not HTML/Markdown/scripts |
| `node.source` | JSON Pointer into `{state: pageState}` |
| `node.visible` | Visible only when the pointer resolves exactly to true; no expressions |
| `node.action` | Required action name for forms/buttons |
| `node.selection` | Optional table state slot receiving the complete immutable selected row |
| `node.columns` | 1–12 row property names |
| `node.children` | At most 64 children |

State/action/selection/result names match `^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`. Unknown component properties are rejected; unknown future component names are allowed for fallback. Nodes contain no grants, executable expressions, network data-source URLs, Flutter objects or OS handles.

Components: column; responsive (two columns at width ≥600 and text scale ≤1.5, otherwise vertical); collapsible group; tabs preserving mounted state; text; detail with bounded selectable JSON; form; button; table. Forms use the target action's input schema. The submitted complete value replaces `action.input`, without merging hidden defaults. Form values stay in widget state rather than shared state. Optional fields can be enabled dynamically; bounded precompiled editors handle nested objects/arrays, while open dictionaries, deep or complex combinations use JSON editing with complete schema validation.

Tables contain at most 1000 rows/12 columns, render only the current 20-row page, search declared columns, sort numbers numerically and other values as strings, and reset pagination after filtering/sorting. Selection binds details. Pagination is client-side over bounded capability snapshots; no remote total/cursor semantics are implied.

## Example

```json
{
  "version":"1.0",
  "state":{"snapshot":null,"selected":null},
  "actions":{"sample":{"capability":"system-monitor/snapshot","result":"snapshot"}},
  "root":{"type":"responsive","children":[
    {"type":"button","label":"Collect local snapshot","action":"sample"},
    {"type":"table","source":"/state/snapshot/processes","columns":["pid","name","cpu"],"selection":"selected"},
    {"type":"detail","source":"/state/selected"}
  ]}
}
```

The complete example is `examples/system-monitor/manifest.template.json`: six tabs, nested sampling, collapsible details, responsive tables and linked selection. Live mode reads snapshots; demo mode supplies 500 process rows and 60 trend samples. Use pluginpack to pin the actual platform and hashes before installation.

## Execution, events and security

Runtime validates declarations, scope epoch and capability signature at dispatch. Executor validates input/output schemas, current state, concurrency, approval, postapproval `get` signature and epoch. Host checks authentication, grants and sensitive confirmation again. Buttons cannot call undeclared capabilities, other plugins or ACP Sessions; ACP retains native session entrypoints. UI state cannot elevate privileges.

Existing subscriptions poll cursors: an empty epoch obtains a snapshot, invalid cursors require rediscovery, and progress (0..1), terminal results and cancellation remain in the Executor/task UI. Arbitrary events are not UI actions; there are no event-triggered calls or executable subscription scripts. Disconnect makes outcomes unknown and only queries results. New Host epochs never replay invocation. Page disposal releases listeners/state and rejects pending approval; accepted work stays with the workspace Executor and existing cancel/recovery controls.

Budgets: 32 KiB UI metadata; generic JSON 4096 nodes/24 depth; component tree 128 nodes/12 depth; state writes reuse 512 KiB/16384 value nodes/32 depth and at most 64 slots. Visual forms allow 128 controls, six levels and 32 array items before JSON fallback. HCP responses remain ≤2 MiB, SDK frames ≤512 KiB, schemas ≤4096 nodes/32 depth, and plugin references are forbidden. Trusted recursive `$ref` inside the compiled authoritative schema does not enable remote plugin references. Existing 100-progress-frame and bounded journal limits constrain event storms; UI has no self-invoking loops.

Error categories: `ui_invalid` for structure/properties/actions, `ui_version` for client compatibility, `ui_binding` for client pointers, `ui_forbidden` for namespaces/declarations, `ui_limit` for budgets. Host SDK classifies UI-version/pointer-format failures as `ui_invalid`; installation retains the existing HCP error envelope without new transport codes. Execution uses `HostCapabilityError`, never raw child errors or automatic retries.

Web/native containers are not registered in UI 1.0; unknown nodes show placeholders. See [architecture](plugin-ui-architecture.md) for evaluation and future isolation requirements.

## Version synchronization and tests

Update Host schema, SDK, shared positive/negative fixtures, example and tests first, then publish immutable artifacts with a new version/hash. Clients obtain bytes from the selected Host revision, synchronize `lock.json` and run identical fixtures. Editing only the lock cannot conceal parser differences.

`python3 tool/check_plugin_ui_contract.py --host ../nimotehost` compares all three artifacts and hashes. Ordinary Flutter tests verify local pins without a neighboring repository in CI; Go tests use the same corpus and cover legacy/depth/oversized inputs. Before external publication, the lock baseline identifies the preimplementation revision and SHA256 identifies actual content. Repositories publish separately, without shared source-path dependencies.
