# Implementation summary — English / 简体中文

Delivered in this source tree: additive HCP **1.0** plugin platform over existing
Host transport **1**. Review [architecture](architecture.md), [public protocol](protocol.md),
[Manifest](manifest-schema.md), [SDK/install/publish guide](plugin-sdk.md),
[security boundary](security.md) and [testing](testing.md).

Implemented:

- Concurrent namespaced registry with Agent/Tool/Task/Event descriptors, schemas,
  UI hints, versions, lifecycle state, discovery and change/progress notifications.
- Independent supervised processes, manifest/payload hash validation, platform
  compatibility checks, default-disabled install, enable/revoke, update/uninstall,
  health checks, bounded backoff/restart attempts, timeout/cancel, bounded queues,
  receipts/results and sanitized audit metadata. Existing native quota settings
  are reused rather than inventing another backend.
- Public language-independent schemas/specification, standard-library Go SDK,
  community starter, bundle packaging tool, generic MCP query/invoke mapping and
  dynamic ACP v1/v2 descriptors using existing sessions/approvals/events.
- Independent HTTP API Inspector; no Inspector-specific Host code. Functional,
  schema compatibility, concurrency, authorization, cancellation/crash recovery
  and SSRF tests are included.
- Both contribution guides now link the public development/security requirements.

Validation (2026-11-14, macOS arm64):

| Check | Result |
| --- | --- |
| Full `go test -mod=readonly -race ./...` | PASS; Host package 190.223s, all root-module packages passed. |
| Plugin runtime / Inspector / real subprocess / ACP targeted race suite | PASS |
| Public SDK negotiation, frame bounds and credential redaction | PASS |
| `go vet ./...`, `go mod verify`, `git diff --check` | PASS |
| Public Manifest/schema compatibility and documentation links | PASS |
| Linux amd64 `go test -exec=true ./...` | PASS, compile only |
| Windows amd64 `go test -exec=true ./...` | PASS, compile only |
| Inspector binary build, bundle/hash generation, real NDJSON negotiation and SSRF rejection | PASS |
| Linux/Windows native execution | Pending existing CI matrix; not run here. |


Known limits / follow-up:

1. This is not an OS sandbox: same-account plugins can access that account's
   files and sockets. Declared permissions are reviewed grants, not kernel
   filesystem/network restrictions. Signatures, trusted publisher authorities,
   downloads/marketplace and automatic sandbox provisioning are not implemented.
2. Subscriptions poll a 256-event in-memory journal. Results/receipt keys do not
   survive daemon restart. Epoch changes explicitly reject automatic retries;
   uncertain side effects need user review. No durable background job scheduler,
   result export or server-push child Event stream is claimed.
3. Generic event capabilities provide schema-defined snapshot/batch results;
   the core does not interpret plugin-specific event payloads. ACP capabilities
   use the existing Session wire contract. MCP is a generic bridge, not a new
   dynamic remote MCP server manager.
4. Schema support requires inline JSON Schema objects (no `$ref`/`$dynamicRef`),
   bounded depth/size. Optional UI hints are declarative data. Exact HCP 1.0
   matching is implemented; new incompatible versions need a separate specification.
5. New compatible plugins require no provider-specific Host/client edits once
   the client implements the generic contract. This work does not modify NimoteCode
   or claim that an older client without HCP already renders new plugins.
6. Linux/Windows native execution and independent client acceptance still need CI
   or the relevant environment. External package-registry/release publication
   is not performed; public versioned artifacts are committed-ready in the repo.
7. Installation has no hooks and fails closed; interrupted update directory swaps
   may require manual recovery of a private `.previous-*` package. Raw child
   stderr is discarded to avoid credential leaks; structured state/errors are
   available for diagnosis. No guarantee is made that arbitrary returned content
   itself contains no secrets.

简体中文：已交付可扩展源码、公开协议/SDK、独立插件及自动化测试；现有功能沿用原实现。
尚未实现同账户恶意代码沙箱、签名市场、跨重启调用恢复或 NimoteCode 仓库改造。
Linux/Windows 原生测试与外部发布明确保留为后续验收，不以交叉编译替代。
