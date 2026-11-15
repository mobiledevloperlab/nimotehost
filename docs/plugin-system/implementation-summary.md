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
   automatic updates and automatic sandbox provisioning are not implemented.
   Explicit metadata-catalog search and GitHub download/install are now supported.
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

## Desktop GUI and local deployment follow-up

Implemented a generic Plugins page in the existing Fyne GUI, a public-HCP desktop
facade, bundle review/confirmation, lifecycle controls, JSON capability input with
schema defaults, sensitive-call confirmation, result/progress display and cancel.
No Inspector-specific GUI logic was added. All six UI languages are updated.
GUI module race tests, desktop facade race tests and both static checks passed.
macOS arm64 deployment completed on 2026-11-14:

- Native CLI and GUI bundle built with `go run ./tools/build -gui -version 2.4.0-plugins-gui`.
- Old GUI exited and old Host stopped through authenticated `nimotehost stop`.
- Existing `setup` installed the verified full GUI distribution, retained credentials/
  session records and created a previous-bundle backup outside the repository.
- New launchd service runs the fixed per-user installation; GUI was opened from
  that installation and linked from `~/Applications/NimoteHost.app`.
- Installed CLI, app CLI and GUI hashes match the built bundle. Host status reports
  `2.4.0-plugins-gui`, unchanged Host identity and a new daemon instance.
- Live HCP feature discovery plus `plugins.list`, `capabilities.list` and
  `capabilities.subscribe` passed. No community plugin was automatically enabled.
- Final native GUI module race suite passed (including the added plugin page tests),
  desktop facade race suite passed, and both packages passed static checks.

简体中文：当前系统已停止旧版本并安装/启动新 Host 与 GUI，入口在用户 Applications。
安装前后 Host 身份保持不变，凭据及会话记录保留，程序包摘要及实时 HCP 接口验证通过。
新增 GUI 源码随本次功能提交保存，未推送远端。


## Local and metadata-only GitHub catalog installation

Implemented both local installation and directory-driven GitHub Release installation.
Developers register only data in `plugin-catalog/index.json`; their implementation
code and binary assets stay in their own repositories. The public SDK includes
catalog DTOs and the versioned catalog JSON Schema. GUI provides search, source
configuration, platform checks, exact Manifest review, download/install/update,
progress/cancel and recent-job recovery. Online install uses the existing local
installer and never automatically enables a downloaded package.

The publisher tool can generate native Release assets and a metadata-only catalog
record. The default starter directory is empty, with no fictional published plugin.
A separate GitHub registry can be configured. Publishing the new default index path
requires an explicit repository push; no GitHub upload, push or release is performed
by this work. Signing, private GitHub repositories, automatic updates, archive
extraction/install hooks and OS sandbox provisioning remain outside this version.

Catalog verification and local deployment (2026-11-14):

- Root `go test -mod=readonly -race ./...`: PASS; Host package 194.022s.
- Native GUI module race suite: PASS, 35.921s; plugin runtime/publisher and owner-ACL targeted suites: PASS.
- Root/GUI `go vet ./...`, module verification, schema/documentation checks and diff whitespace: PASS.
- Linux amd64 and Windows amd64 all-package/test compilation: PASS (`-exec=true`, no native execution claim).
- Native CLI/GUI distribution built and installed as `2.4.0-plugin-catalog` using existing verified setup.
- Host identity and records preserved; new daemon instance and launchd running state checked. Installed CLI/app CLI/GUI SHA256 values match the built distribution.
- Installed GUI reopened. Live `plugins.catalog.sources`, `plugins.install.jobs`, local plugin listing and the `plugin_catalog_v1` feature flag passed.
- Live search responded with zero matches and one unavailable source. The newly added default JSON index has not been pushed/published; a direct HTTPS check also failed with a connection reset on this machine. This is not claimed as a successful real-public-release installation; download/install functional/security coverage uses injected test transports.
- Real public GitHub download acceptance remains pending reachable networking plus catalog/release publication. No HTTP/SOCKS proxy environment was configured; no network/proxy settings were changed.
- No plugin executable was downloaded from a real developer repository or automatically enabled. No GitHub push/release was performed; the metadata-only starter index remains empty.
- Program-package rollback backup retained at the installer-reported per-user backup location. The GUI/catalog source changes are included in the local implementation commit; no remote push was performed.
