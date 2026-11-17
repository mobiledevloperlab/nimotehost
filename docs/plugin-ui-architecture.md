# Plugin UI architecture decisions

UI Contract 1.0 is an additive extension in HCP 1.0 `capability.ui.contract`. Business schemas and UI trees are separate; absent contracts retain the legacy form. Host does not depend on Flutter widgets, and clients register only precompiled components.

## Data flow and ownership

Manifest → SDK validation → HCP discovery → bounded contract parsing → renderer → action ID → runtime → existing Executor → approval and signature recheck → HCP invocation → events/results → page state → renderer.

Closing a page releases listeners and state. Accepted remote work stays in the existing task center and is neither canceled nor replayed automatically. State exists only in memory, is rebuilt for capability-signature or connection-identity changes, and has no cross-plugin sharing or persistence permission. Execution state and results remain owned by the workspace Executor. Tables support local search, sorting, pagination, selection and detail binding.

The schema supports column/responsive/group/tabs/text/form/table/detail/button. Layout uses Flutter primitives and existing `nimote_ui.dart` controls. Unknown components do not render their subtrees; invalid contracts fall back to legacy forms.

## Web and MCP Apps evaluation

Web hosting is not enabled. Official MCP Apps use `ui://` resources, `text/html;profile=mcp-app`, bidirectional JSON-RPC and sandboxed iframes. The official ext-apps AppBridge is a candidate for reuse, but Flutter WebView does not directly provide equivalent origin isolation, CSP or bridge verification. An adapter requires pinned resource hashes, blocked file/arbitrary navigation and permission requests, origin and per-instance nonce binding, message-size/rate limits, declared capabilities routed through the Executor, and bridge shutdown on disposal. The existing browser page is not a plugin container.

References reviewed on 2026-11-14: [MCP Apps specification](https://github.com/modelcontextprotocol/ext-apps/blob/main/specification/2026-01-26/apps.mdx), [official SDK](https://apps.extensions.modelcontextprotocol.io/api/documents/overview.html). This is standards evaluation, not complete MCP Apps support.

[Apple guidelines 2.5.2 and 4.7](https://developer.apple.com/app-store/review/guidelines/) address downloaded code and nonembedded software; 4.7.2 restricts exposing native APIs. [Google Play policy](https://support.google.com/googleplay/android-developer/answer/18265346) exceptions for interpreted code do not waive other obligations. Native schemas download no executable client code. Remote web plugins still require resource, data and distribution review; store acceptance is not established.

## Native components and modules

Existing terminal, editor and debugger implementations remain native. New native views require client registration and authorization; UI 1.0 exposes neither arbitrary native names nor direct OS calls.

Host `pkg/pluginsdk/plugin_ui.go` validates metadata, embeds the authoritative JSON schema, and checks every declared action's namespace and existence through `Manifest.Validate`. Client `extensions/plugin_ui/{contract,runtime,renderer,structured_input}.dart` provides parsing, state/dispatch, registered rendering and bounded nested editing. `HostCapabilityForm.structured` defaults to false and is enabled only by the new runtime. `HostPluginView` retains approval and sensitive-input redaction; runtime dispatches directly through the existing Executor, with the host supplying its original approval callback.
