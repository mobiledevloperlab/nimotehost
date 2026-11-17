# Customization, extensibility and NimoteCode compatibility

This report uses the baseline, evidence labels and finding IDs in [AUDIT_REPORT.md](AUDIT_REPORT.md). Only the Host repository was inspected. Statements in existing Host documents about Flutter/NimoteCode behavior are integration requirements or historical claims until independently tested against that client.

## Existing customization is already substantial

| Customization | Verified implementation | Actual limit / existing-user effect |
| --- | --- | --- |
| Modify, build and install Host | Standalone Go module; CLI `go build ./cmd/nimotehost`; distribution builder and source exporter; desktop is a separate nested module (`go.mod`, `tools/build/main.go`, `scripts/export-source.sh`, `cmd/nimotehost-gui/go.mod`). | Release builder rejects unexpected module replacements/version records (`tools/build/main.go:100–133`). A fork can use plain Go builds or deliberately update its own release records. Native GUI needs native desktop prerequisites. |
| Custom task or command | `StartRequest` carries task/terminal command, args, directory and optional task reference (`protocol.go:51–69,104–109`); `resolveCommand` and `exec.Command` implement it (`manager.go:194–274,355`). | Full OS-account execution for an owner; not a user-installed command registry or a scheduler. Existing mobile UX for arbitrary task creation must be verified. |
| Use/configure installed Agents | Built-in Codex, Claude ACP and OpenCode ACP descriptors (`agent_adapter.go:14–54`); provider-owned controls projected through `agent_controls.go`. | Binary discovery is via PATH/descriptor; version presence alone is not protocol compatibility. Adding a fourth native wire protocol is code work, not just a manifest edit. |
| Custom ACP Agent | Manifest declares namespaced `kind: agent`, ACP 1/2; enabled/ready community adapter starts fixed `--acp` path (`pkg/pluginsdk/protocol.go:84–102`, `internal/host/plugins.go:206–245`). | Reuses structured Agent lifecycle and approvals. Built-in installation/history enumeration remains provider-specific (`agent_adapter.go:75–95`, `agent_history_http.go:87–119`); arbitrary custom history/live attach is not automatic. |
| Custom tool/task capability | HCP process SDK, schema/manifest validation, invoke/result/cancel/events (`pkg/pluginsdk/protocol.go`, `internal/plugins/runtime.go:463–610`). | Manifest `kind` is metadata; generic invocation is not automatically equivalent to a retained Host Session. ACP invocation is deliberately special-cased (`plugins.go:144–170`). |
| Describe capability UI | Public UI schema, bounded component/action/state validator and shared vectors (`pkg/pluginsdk/plugin_ui.go:13–121`, `plugin_ui.schema.json`, `testdata/plugin_ui_vectors.json`). | A descriptor is not an executable Flutter extension. Contract validation on the official Host does not prove an arbitrary modified Host emits valid data. |
| Expose generic extension API | Owner-only `/v1/extensions`, `capabilities.list/get/invoke/result/cancel/subscribe`, namespaced IDs and protocol handshake (`server.go:218–225`, `plugins.go:69–195`). | Existing device roles cannot invoke it. Extending the endpoint must preserve owner checks and schemas. |
| Integrate through MCP | Query tools by default; explicit write-mode invocation through authenticated RPC (`mcp.go:23–54,102–126`). | MCP stdio is not a public API server, plugin UI host, or automatic NimoteCode feature. |
| Alter limits/service behavior | Existing config/resources and per-user service integration (`config.go`, `resources.go`, `internal/hostservice/service.go`). | Some limits depend on OS/kernel configuration. Unsupported hard constraints fail validation rather than silently providing a fake sandbox. |

The `examples/system-monitor` executable and `tools/pluginpack` provide an actual buildable example. `internal/host/plugins_test.go:24–206` exercises a separately built process through RPC and community ACP sessions. No new dynamic plugin loader is needed to meet the user's customization goals.

## Current protocol layers

| Layer | Version/check in real code | Consequence |
| --- | --- | --- |
| Host API | `Protocol = 1`; HTTP `Nimote-Host-Protocol` must equal it (`protocol.go:15`, `server.go:188–190`). | Breaking major differences return 426; no automatic version downgrade. |
| Host software release | Separate `Version` set during build (`protocol.go:17–18`). | A fork can retain compatible wire behavior independently of release naming. Do not conflate product semver with wire versions. |
| Capabilities | `protocol`, `min_protocol`, `features`, `agents`, runtime `enforcement` (`protocol.go:22–48`, `resource_capabilities.go:13–28`). | Existing discovery is sufficient for most additive features. `min_protocol` equals protocol today; it is not evidence of multiple supported majors. |
| SSH RPC stream | Greeting `stream_protocol: 1`; bounded JSON frames and correlated request IDs (`rpc_stream.go:52–107`). | Independently versioned transport behavior must remain stable. |
| HCP | SDK `Protocol = "1.0"`; Host validates before dispatch and advertises supported protocol on error (`pkg/pluginsdk/protocol.go:17`, `plugins.go:97–99`). | An enabled plugin's schema/capability versions are additional contracts, not a reason to increment Host major. |
| Plugin UI | `ui.contract` validation; legacy hints accepted when the field is absent (`plugin_ui.go:18–33`). | Unknown UI contract versions are rejected by this publisher. Client fallback remains independently required. |
| Native Agent/MCP | ACP descriptor versions 1/2; Codex-specific adapter; MCP SDK supports `2025-11-25` (`agent_adapter.go:44–54`, `mcp.go:61–65`). | These versions are not interchangeable; native-provider upgrades need their own smoke checks. |

**COMP-02 — P2, risk identified.** Response feature maps are additive, but requests use `DisallowUnknownFields` (`server.go:33–42`); a new client cannot send a new request field to an old Host merely because JSON is extensible. Expiring mutation keys also require `mutation_expiry_v1` (`request_retention.go:20–34`). Minimum action: document feature-gated request fields, optional response additions, stable types/enums and the different version spaces in one compatibility table. Existing-user effect: preserve protocol 1, legacy request behavior, receipt semantics and explicit errors. Do not add a full negotiation framework now.

## Official, forked and old Host compatibility

Verified Host-side fact: the bearer/protocol path does not check a publisher signature, vendor license or official build identity (`server.go:173–190`). Self-compilation does not inherently break these mechanisms. Host IDs identify an installation; executable paths/discovery and state ownership must remain consistent (`storage.go:17–23,84–138`, `discovery.go:9–20`). A fork that changes state layout, CLI output, executable discovery, fixed endpoint or required fields can break clients even while advertising protocol 1.

**COMP-01 — P1, further verification required.** NimoteCode may have its own minimum version checks, provider enums, discovery commands or rendering assumptions. They cannot be confirmed here. `docs/plugin-ui-audit.md` describes mobile behavior, but contains no auditable client implementation in this repository. Minimum action is a small compatibility fixture suite shared as data, not publication of NimoteCode source. It should cover:

1. Official current Host, oldest explicitly supported Host, and a self-built Host with unchanged core protocol plus one extra namespaced capability.
2. Missing optional fields, unknown feature names, unknown Agent/capability kinds, unknown status values, null/wrong types and absent UI metadata.
3. Oversized/deep responses, invalid UTF-8/JSON, huge arrays, terminal/control sequences and invalid IDs/cursors. Impose client limits before allocation/rendering.
4. 401/403/404/426/429/503, stream interruption, replay/resync, stale epoch, request uncertainty and retry of the same mutation ID.
5. Unsupported UI contract/components/actions: show bounded text/schema/raw-data fallback or hide unsupported actions, without arbitrary execution or crash.
6. Owner/device distinction and a real human decision before sensitive invocation/enable. A `confirmed` boolean supplied by owner code is not a human-consent proof.

Existing-user effect: primarily validation and accurate supported-version documentation; a NimoteCode update is necessary only if current handling fails or a new presentation/control is introduced. Host-side validation cannot force a deliberately modified Host to be trustworthy. The phone must treat every Host response as untrusted input.

## Which additions can avoid a NimoteCode update?

| Addition | Host-only possibility | When a client update is required |
| --- | --- | --- |
| New local script/task using existing command+argv/output | Yes at protocol level. | If the existing UI cannot submit the task, select it or display its output. Client implementation unverified. |
| New capability using existing HCP input/output/UI vocabulary | Yes if the deployed client already implements generic HCP discovery/invocation and supported UI fallback. | If HCP support is absent, provider names are hardcoded, or UI requires new components. Current mobile implementation unverified. |
| New ACP 1/2 binary using existing message/approval projection | Yes for Host execution through the current adapter. | Agent selection/labels may be hardcoded; history/live attachment requires separate provider-specific support. |
| New server-side algorithm behind unchanged operations | Generally yes, preserving semantics, resource/error contracts and receipt behavior. | If behavior introduces a new interactive decision or changes a required field/meaning. |
| New feature flag / optional metadata | Can be advertised now. | To make the phone actively use the new feature; unknown flags alone do not create functionality. |
| New native protocol, approval action, public transport, UI widget or breaking core field | Not transparently usable by an old client. | Explicit client implementation/feature gate and, for breaking core semantics, a coordinated protocol version. |

These are protocol assessments, not claims that current NimoteCode already implements each generic client path.

## Minimum viable extension approach

Keep the existing path: a trusted local executable plus manifest → explicit owner review/hash and enable → namespaced capability → current generic RPC/MCP/optional supported UI. For ACP, reuse the existing adapter and approval/session ownership. For a simple task, a shell script invoked by existing argv execution is usually enough; do not require a plugin package for every command.

No changes to core ACLs are needed for a new owner-only capability. If restricted-device capability access is later requested, explicitly define a narrow role and server checks before adding it. Do not grant capability execution merely because a device can list metadata. No new public endpoint or user-programmable authorization language is necessary.

## Top five worthwhile additions

All five proposals improve the current architecture rather than replacing it. Each references an existing limitation and records its smallest implementation and user effect.

| Rank / finding | Evidence and present limitation | Smallest addition | Priority / existing-user effect |
| --- | --- | --- | --- |
| 1 / COMP-01 | Host contracts/vectors exist; actual mobile tolerance of a fork is unverified (`pkg/pluginsdk/testdata/plugin_ui_vectors.json`, `docs/plugin-ui-audit.md`). | Add a tiny protocol conformance fixture set and record official/old/fork client results, including malformed input. | P1; protects existing clients; no new runtime subsystem. |
| 2 / COMP-02 | Version checks/features exist but rules span several files (`protocol.go:15–48`, `server.go:33–42,188–190`). | Publish a supported-version/feature/request-field table and client fallback rules. | P2; documentation and tests, no forced upgrade. |
| 3 / ARCH-01 | ACP extension is implemented, while built-in discovery/history stays fixed (`plugins.go:206–245`, `agent_adapter.go:75–95`). | Provide one minimal community ACP template and a checklist of startup, cancel, approval, detach and history limitations, reusing current test helpers. | P2; additive; no claim of universal custom history. |
| 4 / COMP-02, SEC-02 | HCP/manifest/schema/UI contracts already exist (`pkg/pluginsdk/protocol.go`, `plugin_ui.go`), but an extension author can mistake metadata grants for isolation. | A short “add one capability” recipe using the system-monitor example, with input/output limits, namespacing, trust warning and supported generic UI subset. | P2; author documentation, existing behavior retained. |
| 5 / SEC-04 | Commands/args/output are already supported but not a reusable reviewed-task catalog (`protocol.go:51–69`, `manager.go:194–274`). | Start with documented script+argv recipes and optional small declarative task examples; add a task alias file only after repeated demand. | P2; no scheduler/DSL; preserve arbitrary owner tasks and avoid secret-bearing argv examples. |

## Maintenance costs to control

**ARCH-01 — P2, risk identified.** `internal/host` combines sessions, terminal I/O, Agent approvals, history, worktrees, GitHub workflows and persistence; `plugins.go:198–245` also stores the community runtime globally. This makes tests and future multi-Host embedding harder. Minimum action: continue using small adapters and shared lifecycle helpers; replace the global with an explicit resolver only when a concrete caller needs it. Existing-user effect: none intended. Exporting all Manager internals as an SDK would freeze the wrong boundary.

**ARCH-02 — P2, verified limitation.** Plugin supervisor's `failures` counter increments after every connection ends and is never reset by healthy work (`runtime.go:688–762`). Three lifetime failures can stop automatic recovery. Minimum action: document the budget or reset after a bounded healthy interval, while preserving explicit enable and no replay of side effects. Existing-user effect: availability behavior only; do not restart forever or conceal repeated faults.

Native provider integration will continue to need maintenance even with ACP: capability/approval differences, control discovery, pagination, session resume and external attachment are provider-specific. Preserve installed-version probing and live handshakes (`agent_adapter.go:99–159`, `agent_compatibility.go:23–26,61–156`). Existing `.github/workflows/agent-compatibility.yml` and `scripts/agent_compatibility.py` monitor upstream changes without a model call; their existence is useful, but successful remote monitoring is not verified here.

Avoid dynamic Go plugins, a general cross-provider RPC passthrough, automatic Agent installation, automatic capability execution on page render, downloaded native UI code, a marketplace/dependency solver, a workflow DSL, or a universal sandbox abstraction. Each creates ongoing security and compatibility obligations beyond the intended lightweight Host. Retaining the current bounded process extension mechanism is sufficient.
