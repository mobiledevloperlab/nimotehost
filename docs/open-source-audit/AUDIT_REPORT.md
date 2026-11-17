# NimoteHost open-source readiness audit

Audit date: 2026-11-17. Source baseline: `42d3cd1` (36 locally reachable commits). Scope: this Host repository, its two Go modules, local module-cache license texts, tracked resources, local Git refs/reflogs, and the ignored local environment note identified below. No NimoteCode source, other repository, production state, remote Git history, live user session, or external account was inspected.

The implementation is substantially ready for public review, but the evidence does **not** yet support an unconditional production release. There is a concrete desktop license-packaging gap, a local credential hygiene issue, known affected desktop dependency versions with static TIFF-decoder call paths, and outstanding rights, client-compatibility and native-platform acceptance checks. No unauthenticated remote execution vulnerability was established by this audit. That is a bounded finding, not proof that no vulnerability exists.

## Reading the conclusions

- **Verified**: directly supported by inspected code or a recorded check. A verified behavior may still be an intentional trust boundary.
- **Risk identified**: code or local material establishes a specific limitation; exploitability and impact are stated separately.
- **Further verification required**: evidence is unavailable or incomplete. This is not a vulnerability verdict.
- **P0**: confirmed immediate exposure or authorization bypass requiring a release stop. No finding currently meets this evidentiary threshold.
- **P1**: resolve or explicitly close the relevant release gate before shipping the affected distribution.
- **P2**: bounded improvement; do not block basic source publication or introduce a large framework for it.

Evidence locations refer to baseline code lines unless explicitly marked as edited documentation. Sensitive values are intentionally omitted throughout these reports.

## What the code actually implements

| Area | Verified behavior and evidence | Limit |
| --- | --- | --- |
| Transport | Local IPv4 HTTP only; `Config.Address()` fixes `127.0.0.1`; authenticated CLI RPC can run inside an existing SSH exec channel (`internal/host/config.go:18–56`, `rpc_client.go:21–90`). | Host does not implement or configure SSH server authentication, host-key checking, SFTP or firewall policy. |
| Authorization | Owner bearer token, independent device credentials, per-request revocation and route/session checks (`server.go:167–233`, `devices.go:257–316`). | An unrestricted SSH login or same-account executable can read the owner token. Device ACLs cannot confine that authority. |
| Execution | Task argv, terminal/PTY, structured Codex app-server and ACP adapters; supervised lifecycle (`manager.go:194–274,355–411`, `agent_adapter.go:14–54`). | Workspace is a working directory, not a filesystem sandbox. Agent approval behavior also depends on the installed provider. |
| Persistence | Owner-private state, atomic snapshots, output rings, activity/events and mutation receipts (`storage.go:37–138`, `output.go:19–68`, `idempotency.go:73–145`). | Local data is readable by the account and is not application-encrypted. Daemon restart does not resurrect an OS process. |
| Extensibility | Public `pkg/pluginsdk`, HCP `1.0`, namespaced capabilities, process plugins and ACP community adapters (`pkg/pluginsdk/protocol.go:17–113`, `internal/host/plugins.go:206–245`). | Plugin processes have account privileges. A dynamic Go plugin system is unnecessary. |
| Client discovery | Host protocol `1`, feature map, version/min_protocol, runtime enforcement fields; separate stream/HCP/UI versions (`protocol.go:15–48`, `resource_capabilities.go:13–28`). | This is capability advertisement and exact major-version checking, not a general version-range negotiation engine. |
| Distribution | Separate CLI/GUI modules, source export allowlist, module/version checks, SHA256 bundles, native/cross-build CI (`tools/build/main.go:100–133`, `scripts/export-source.sh:8–39`, `.github/workflows/ci.yml`). | Workflow definitions do not prove successful remote runs or device acceptance. |

No official-build attestation or vendor-only license check was found in the Host connection path. Self-built binaries can preserve the existing wire contracts. NimoteCode's actual connection/discovery policy remains unverified.

## Finding register

Each row records current behavior, consequence, smallest proposed action, priority and existing-user impact. Domain reports expand the evidence and acceptance criteria.

| ID / conclusion | Evidence | Current behavior and risk/limit | Minimum action | Priority / effect on existing NimoteCode users |
| --- | --- | --- | --- | --- |
| LIC-01 / Risk identified | `tools/build/gui.go:133–149`; upstream Fyne `theme/bundled-fonts.go:9–55`; typesetting `harfbuzz/LICENSE:1–34` | Only module-root notices are copied; nested font, HarfBuzz and GLFW notices are missed. A desktop artifact cannot be assumed compliant. | Add an explicit small list of notices for actually distributed nested assets/code and verify archive contents. | P1; packaging only, no protocol change. |
| LIC-02 / Further verification required | `NOTICE:1–8`, `cmd/nimotehost-gui/assets/README.md:1–10`, `branding.go:11–16` | License/brand boundaries exist, but files cannot establish original-code or artwork ownership. | Owner records authority to license code/artwork; publish a short fork-brand policy. | P1 for rights confirmation; no runtime change. |
| SEC-01 / Risk identified | Local ignored `env.md:2–5,9–12,16`; `.gitignore:20`; `scripts/export-source.sh:8–16` | Environment note contains login information and an API-key-shaped value. It is untracked, excluded by export allowlist, and absent from the scanned reachable history. Validity/external exposure is unknown. | Owner verifies and rotates any real credentials; keep note outside release material; rescan final artifact. | P1; credential changes may require local reconnect/reconfiguration. No Host code change. |
| SEC-02 / Verified boundary; acceptance required | `manager.go:264–274,385–387`; `plugins.go:28–34,59–66`; `SECURITY.md` | Normal execution and plugins run as the Host account. Environment filtering/grants do not restrict file/network syscalls. Native Agent policy is retained. | Validate that shipping UX accurately discloses account authority and provider approval limits; keep default per-user installation. | P1 acceptance item, not an established ACL bypass; no default removal of functionality. |
| SEC-03 / Risk identified | `storage.go:96–138`; `cmd/nimotehost/main.go:75–149` | Owner secret is long-lived; there is no dedicated owner-token rotation command in the inspected CLI. | Document stopped-daemon rotation/reconnect; add a small serialized rotation command only if operationally needed. | P2; rotation invalidates old local credentials and needs reconnect. |
| SEC-04 / Risk identified | `protocol.go:93–100`; `activity.go:193–212`; `manager.go:748–774`; `request_retention.go:15–18,47–98` | Args/output/tool text persist; no universal redaction or expiry. Delete removes metadata before auxiliary files and can leave undiscoverable remnants after an I/O failure. Legacy receipts intentionally outlive sessions. | Document data classes/retention and backup limits; make orphan cleanup retryable without weakening receipt replay protection. | P2; no wire break; users should opt into any future retention policy. |
| SEC-05 / Verified limitation | `resources.go:16–32`; `resource_caps_darwin.go:6–9`; `resource_caps_linux.go:12–27`; `plugins/runtime.go:565,710` | Default CPU/RSS/process/runtime quotas are unset; macOS lacks hard quotas, Linux needs delegated cgroups. Plugin disk use is not a hard-quota boundary. | Keep honest enforcement metadata; give optional conservative settings and cleanup guidance, not blanket new defaults. | P2; finite defaults could interrupt existing long tasks, so avoid silently changing them. |
| COMP-01 / Further verification required | `protocol.go:22–48`; `pkg/pluginsdk/plugin_ui.go:18–33`; `docs/plugin-ui-audit.md:17–23` | Host has bounded extension contracts. Host repository descriptions of mobile fallback do not prove NimoteCode's parser/render behavior. | Run the smallest official/custom/old Host compatibility matrix with unknown fields, enums, UI components and hostile-size data. | P1; client fix only if checks fail; must precede claiming arbitrary extensions are mobile-safe. |
| COMP-02 / Risk identified | `server.go:188–190`; `protocol.go:43–44`; `plugins.go:97–99`; `request_retention.go:20–34` | Major protocol is exact-match; request unknown fields are rejected; old mutation IDs have different retention guarantees. | Document additive-response rules, feature gates, request compatibility and separate Host/HCP/UI versions. | P2; preserve protocol 1 and old ID semantics. |
| ARCH-01 / Risk identified | `manager.go:35–65`; `agent_adapter.go:44–54`; `plugins.go:198–245`; `external_sessions.go` | One large Host package owns many domain states; community adapter lookup uses a process-global runtime. | Keep adapters narrow; pass the existing runtime explicitly when naturally touching this area. No package-wide rewrite. | P2; internal-only change, preserve lifecycle semantics. |
| ARCH-02 / Verified limitation | `internal/plugins/runtime.go:688–762` | Restart failure count is cumulative and is not reset by healthy operations; a later third disconnection ends recovery. | Document the bounded budget, or reset it only after a deliberate healthy interval; retain no replay of side effects. | P2; availability improvement only. |
| REL-01 / Further verification required | `.github/workflows/ci.yml:11–250`; `internal/host/*_windows.go`, `execution_identity_linux.go:83–125` | CI declares eight CLI targets and six desktop targets; local macOS tests cannot validate Windows ACL/ConPTY, Linux cgroups or native GUI behavior. | Archive successful CI results plus minimal native installation, disconnect, revoke and shutdown checks. | P1 for claimed supported targets; no protocol change. |
| REL-02 / Risk identified | `.github/workflows/ci.yml:241–250`; `tools/build/archive.go:85–92`; `internal/desktop/releases.go:21–54` | Checksums detect byte changes, not publisher identity; Actions use movable major tags. GUI checks metadata on demand, not an automatic executable updater. | Pin release actions to reviewed commit SHAs; optionally add provenance/platform signing. Do not build an updater framework. | P2; trust/distribution only. |
| REL-03 / Risk identified; exploitability needs verification | `cmd/nimotehost-gui/go.mod:10,49–50`; GUI `branding.go:11–27`, `window.go:55–67`; scanner traces | Desktop scan has 30 advisory IDs / 50 finding records, with six symbol traces across four TIFF advisories. Observed application image inputs are embedded PNG resources; hostile TIFF reachability is not established. CLI scan has no finding. | Triage affected x/image/x/net/x/text versions; prefer a tested compatible dependency update, or record a justified non-exposure decision. Recheck release targets/toolchain. | P1 for desktop release; no wire change; dependency update needs GUI regression checks. |
| DOC-01 / Verified, corrected during this task | `README.md`, `SECURITY.md`, `THIRD_PARTY_NOTICES.md`, `OPEN_SOURCE_REVIEW.md`, `CONTRIBUTING.md`, `CHANGELOG.md` and five `.zh-CN.md` editions | English documents had Chinese prose; Chinese editions also had untranslated English paragraphs/headings. | Both document sets standardized to their own language; identifiers, commands, upstream license texts and translation resources preserved. | P2, completed; documentation only. |

## Architecture and maintenance assessment

The useful module boundary is already present: CLI/desktop management → authenticated local API → Manager/Agent lifecycle → platform process backend. Plugin protocol/validation is split into `pkg/pluginsdk` and `internal/plugins`; native service integration and installation transactions have separate packages. GUI dependencies do not enter CLI builds. Reuse these boundaries.

Agent implementation cost is dominated by native protocol churn, approval semantics, history projections, terminal fallback and attachment ownership, not by selecting a binary. `agent_compatibility.go:23–26,61–156` makes a live handshake before accepting work, avoids replaying running turns through another adapter and refuses terminal fallback under enforced constraints. `agent_rpc.go:23–49` delegates JSON-RPC correlation to the existing SDK. These are valuable safeguards to preserve.

Concurrency protections include connection/request/per-principal capacity, single-daemon locks, bounded RPC worker pools, bounded output, mutation receipts and explicit process ownership. `Manager.Close()` joins session/background work (`manager.go:792–821`); plugin close cancels and joins runtime workers (`internal/plugins/runtime.go:632–644`). This review did not establish a specific new deadlock or race. Platform tests and fault injection remain necessary evidence for these guarantees.

Potential simplifications should be incremental. Community adapters need launch, version and protocol configuration, not a universal Agent abstraction spanning all provider history/control models. Keep the existing catalog optional and freeze scope; discovery/download/UI/runtime already create substantial test surface. No new dynamic loader, plugin marketplace, scripting DSL, dependency resolver, arbitrary remote UI, enterprise integration layer or general scheduler is justified by the stated goals. No dependency or suspected dead code should be removed solely because it is indirect or test-only; the resolved graphs include build/test and platform-specific packages.

## Validation record

Environment: macOS/arm64, Go `1.27.4`; minimum declared Go version is `1.26.5`. Checks use read-only module mode where applicable. No real SSH service installation, provider/model request, GitHub mutation, user session start, production token rotation or history rewrite was performed. Opt-in `NIMOTE_TEST*` variables were absent.

| Check | Result |
| --- | --- |
| Root/desktop `go list -mod=readonly -m -json all` | Passed; 16 external root modules and 77 external desktop-graph modules (root dependencies overlap). Desktop uses one authorized local Host replacement. |
| Root notices versus cached dependency originals | All 16 records' root LICENSE/COPYING/NOTICE/PATENTS texts matched; no mismatch. This does not cover nested licenses. |
| `go mod verify` and `go vet ./...` in both modules | Passed. |
| `go test -mod=readonly -race ./...` (root) | Passed; Host package completed in 212.851 seconds. Live opt-in and platform-specific checks are not implied. |
| `go test -mod=readonly -race -tags ci ./...` (desktop) | Passed; linker emitted duplicate `-lobjc` warning. Headless test tag is not a native GUI acceptance run. |
| Python unittest discovery / final document checks | 19 tests passed initially; final link validation passed after document edits. `git diff --check` passed. |
| Local refs/reflogs and credential-pattern scan | 877 unique blob contents; no private-key/provider-token pattern match. Generic URL/path/IP hits were tests or network denylist entries. See license report for limits. |
| `govulncheck v1.9.0`, root and native desktop, JSON output | Root: no finding. Desktop: 30 advisory IDs, 50 records; six symbol traces cover four TIFF advisories. JSON-mode exit 0 alone does not mean clean. See SECURITY_PRIVACY.md for input-reachability triage. |

No newly added implementation tests were needed for an audit and language-only documentation correction. Eleven existing root documents were corrected (six English and five Chinese). Existing tests were used as evidence, with skipped/live/platform checks kept distinct from executed checks.

## Release decision and remaining work

Six acceptance groups are defined in [ROADMAP.md](ROADMAP.md): G1 nested notices, G2 rights/brand confirmation, G3 credentials/final-source scan, G4 client compatibility/security UX, G5 supported-platform acceptance, and G6 dependency vulnerability triage. These are six work groups, **not six confirmed security defects**. Source publication and binary release have different gates; GUI-specific packaging checks need not prevent a properly cleared CLI-only release.

Apache-2.0 does not require an independent communicating NimoteCode client to publish its source. Redistribution still requires retaining applicable notices, and other copied/linked code needs its own review. The license also withholds trademark permission beyond limited descriptive uses. These conclusions follow the [Apache License sections 1, 2, 4 and 6](https://www.apache.org/licenses/LICENSE-2.0); the Host repository cannot establish NimoteCode's separate dependency or ownership situation.

The domain reports are [license/compliance](LICENSE_COMPLIANCE.md), [security/privacy](SECURITY_PRIVACY.md), and [extensibility/client compatibility](EXTENSIBILITY.md). No implementation, configuration, dependency or license file was changed by this audit.
