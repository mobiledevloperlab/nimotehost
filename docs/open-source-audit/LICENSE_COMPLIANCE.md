# License, dependency and sensitive-information audit

Baseline/date, priorities and conclusions follow [AUDIT_REPORT.md](AUDIT_REPORT.md). Scope includes both Go module graphs, preserved upstream notices, embedded GUI/schema/translation resources, source export/build logic, current tracked files, local refs/reflogs and the ignored environment note. This is a repository-based compliance assessment, not proof of authorship or a review of NimoteCode's independent code/dependencies.

## Apache-2.0 suitability

**Verified:** root `LICENSE` contains Apache-2.0; `NOTICE:1–8` names the project/contributors and excludes other Nimote repositories and trademarks. Source files commonly carry SPDX identifiers. `CONTRIBUTING.md` states contribution licensing. `THIRD_PARTY_NOTICES.md` and the preserved `third_party/` notices are present. CLI builder checks resolved modules and copies license/provenance material (`tools/build/main.go:100–133,194–260`). There is no reason to replace these files just to announce open source.

Apache-2.0 allows proprietary use and distribution subject to its terms. A separate mobile client communicating through SSH/API is not required to publish source merely because the Host is Apache-licensed. Retain the license, relevant notices, modification notices and original attributions for distributed derivatives. The license does not grant general branding rights. This follows the repository's `LICENSE` sections 1, 2, 4 and 6 and the [official Apache text](https://www.apache.org/licenses/LICENSE-2.0).

**Further verification required:** if NimoteCode copies other third-party code or includes other libraries, those separate terms still apply. No NimoteCode source/dependency graph or contributor agreements were inspected. “Host is open” is not proof that every component of a separate commercial product is cleared.

No incompatible mandatory copyleft dependency was identified in the inspected Host runtime paths. This conclusion is narrower than “all packaging is compliant”: nested notices are concretely missing from the GUI collection logic, and one graph-only module offers a FreeType/GPL choice that must not be mislabeled as generic BSD.

## Existing notices and obligations

| Material | Verified current status | Minimum action / priority / existing-user effect |
| --- | --- | --- |
| Host LICENSE and copyright | Apache text, NOTICE and common SPDX headers exist (`LICENSE`, `NOTICE:1–8`, `internal/host/server.go:1–2`). | Retain; owner confirms rights under LIC-02/P1. No runtime effect. Per-file boilerplate uniformity is not itself a release blocker. |
| NOTICE | Existing project/third-party/brand scope statement. | Preserve in derivatives/bundles; add required upstream attributions when applicable, not a new blanket license for them. P1 as part of LIC-01; no client effect. |
| CLI third-party notices | All 16 resolved external-module records match root upstream license-family files byte-for-byte; websocket `LICENSE.txt` maps to preserved `LICENSE`. | Keep existing version guard and Go toolchain notices. Verified; no corrective change required for root texts. |
| GUI notices | `GUI-MODULES.json` and root notice copying exist (`tools/build/gui.go:109–156`). | LIC-01/P1: add an explicit nested-notice list for actually used code/fonts; check actual archives. No protocol effect. |
| System libraries and packaging tools | GUI builds use CGO/platform desktop libraries; build-only modules also appear in GUI graph. | REL-01/P1: inspect linked/bundled native components per OS; distinguish system prerequisites from bundled libraries. No requirement to license unshipped tools as Host code. |
| Agent executables / SSH / gh | Host invokes installed programs; root notices say Agent binaries and SSH are not bundled (`THIRD_PARTY_NOTICES.md`). | Keep external-tool license/service-term boundaries visible; inspect any future bundling separately. P2 guidance; existing features retained. |

## LIC-01: confirmed nested-license collection gap

**Conclusion:** Risk identified. **Priority:** P1 before GUI binary distribution. **Existing-user effect:** corrected notices only; do not change execution/protocol behavior.

Current behavior: `tools/build/gui.go:133–149` scans only immediate module-root files whose names start LICENSE/COPYING/NOTICE or equal PATENTS. It considers any such file sufficient. It does not walk nested license locations or follow a root notice that points to separate license texts.

Concrete evidence from locally resolved upstream modules:

| Component and actual use | Notice missed by root-only collection | Risk / minimum remedy |
| --- | --- | --- |
| Fyne `v2.8.2`, `theme/bundled-fonts.go:9–55` embeds Noto Sans, DejaVu Sans Mono Powerline and Inter Symbols; exported default resources are in `theme/font.go:17–58`. | `theme/font/LICENSE.txt`, `LICENSE_DejaVu-Powerline.txt`, `LICENSE_Inter.txt`; check `LICENSE_EmojiOneColor.txt` against enabled emoji asset/build tags. | Fonts have their own terms/copyrights, including OFL and DejaVu/Powerline terms; Fyne's BSD root alone is insufficient. Copy the actual font notices and attribution material into the GUI bundle. |
| `github.com/go-text/typesetting v0.3.5`, shaping used by Fyne painter and go-text/render. | `harfbuzz/LICENSE:1–34` contains its own MIT text and multiple upstream copyright notices. | Root BSD does not replace nested MIT attributions. Preserve the HarfBuzz notice with the included port. |
| `github.com/go-gl/glfw/v3.4/glfw v0.1.0-pre.2`, included in native macOS GUI dependency traversal. | `glfw/LICENSE.md` covers bundled native GLFW code, separate from root bindings. | Preserve native GLFW's own notice; inspect relevant C source headers for the shipped target. |
| `github.com/golang/freetype` graph dependency | Root `LICENSE:1–12` points to `licenses/ftl.txt` and `licenses/gpl.txt`; test fonts have separate terms. | Graph membership alone is not evidence it is linked: it was absent from this native macOS runtime import traversal. If ever distributed, select/document a valid license route and preserve its complete text; do not automatically assert GPL infection. |

Additional nested license files were found in testdata, fuzz, generated-data and utility subtrees of several modules. These do not automatically require redistribution in a binary that contains none of that material. Conversely, a module-root inventory is not a file-level clearance. A short reviewed inclusion map for the concrete fonts/native code above is the lowest-maintenance fix; avoid a general legal rules engine.

The [OFL's official text](https://openfontlicense.org/open-font-license-official-text/) permits font embedding under its conditions, including keeping the applicable license/copyright. OFL on a bundled font does not require publishing a separate application's code. Keep individual upstream terms intact rather than relabeling every resource Apache-2.0.

## LIC-02: ownership, brand and artwork

**Conclusion:** Further verification required. **Priority:** P1 for right-to-publish confirmation; a detailed brand guide is P2. **Existing-user effect:** none for official builds; a fork may need distinct presentation.

Evidence: `cmd/nimotehost-gui/branding.go:11–16` embeds icons; `assets/nimotehost.png`, `assets/nimotehost-rounded.png` and `cmd/nimotehost-gui/assets/*` contain artwork/derivatives. The asset README describes generated artwork (`cmd/nimotehost-gui/assets/README.md:1–10`) but supplies no author/source-rights record. The root NOTICE explicitly withholds Nimote trademark rights. Git identity normalization does not establish who originally authored or owns code.

Minimum action: owner records authority to publish original code, documents, generated source artwork and derived icons; identify third-party inputs if any. State whether existing artwork files are covered by the source-tree copyright license, and distinguish this from permission to present a fork as an official Nimote product. Provide simple “based on NimoteHost”/fork naming guidance and avoid imposing non-open restrictions on the Apache-licensed code itself. No evidence currently proves infringement; do not label the art unlicensed solely because provenance records are missing.

## Sensitive information and Git history

**SEC-01 — Risk identified, P1.** The local ignored `env.md` contains:

| Location | Type only | Verified publication scope / risk |
| --- | --- | --- |
| `env.md:2–5` | Private network address, SSH login information and password | Untracked and ignored by `.gitignore:20`; not in export top-level allowlist. Real credential validity unknown. |
| `env.md:9–12` | Another private address and login/password information | Same scope; internal-address disclosure and credential reuse risk if manually copied to a release or support log. |
| `env.md:16` | API-key-shaped value | Same scope; no provider authentication or validity test performed. |

No actual values are reproduced here. Minimum action is owner-side verification and rotation of any real credentials, keeping private notes outside publication material and checking the final Git/source/binary artifact. Existing-user effect: only affected credentials may need updating. An ignored local file is not proof of historical public exposure. This audit did not remove, rotate, test or transmit these credentials.

Current tracked-file and history checks were performed using an in-memory Python scan of Git blob contents, with location/type-only output. Private-key block markers, AWS access-key IDs, GitHub token families, common long `sk-` provider-key formats and credential-bearing HTTP URLs were checked; generic key/password assignment, private addresses and local-user-path patterns were also triaged in tracked current content. This is a scoped pattern scan, not an entropy scanner or a guarantee of no secrets.

| Check | Verified result | Limit |
| --- | --- | --- |
| Local refs | 36 commits; 877 unique reachable file contents. | Remote-only branches, old clones and hosting caches were not fetched or inspected. |
| Reflogs | Six entries, one commit outside normal refs; including reflogs added no unique blobs to the 877 scanned contents. | Historical objects pruned before this audit cannot be reconstructed here. |
| Private-key/provider patterns | No matches in the 877 scanned blobs. | Short/custom/encoded/opaque credentials and image metadata may evade these patterns. |
| URL/path/IP candidates | URL credentials were test fixtures in `github_tasks_test.go:18`, `opencode_attachment_test.go:286`, `plugins/catalog_test.go:213–219`; path candidates are synthetic service tests (`hostservice/host_test.go:112,122`); IP entries are network denylist/test inputs. Historical removed HTTP-inspector files also produced fixture matches. | Identifying test fixtures does not mean real credentials elsewhere are impossible. No fixture value is repeated here. |
| Current generic secret assignments | Candidates in test fixtures plus exporter regex; no additional established live credential. | No external validity check. |
| Commit messages / identities | No checked credential-pattern candidate in messages; one normalized author/committer identity variant. | Normalized authorship is not an ownership record. |
| Object integrity | `git fsck --no-reflogs --unreachable` exited 0; one unreachable commit and two trees, no additional unreachable blob. | No Git history/object rewrite or pruning was performed. |
| Source export | Explicit top-level allowlist excludes `env.md`/Git/build outputs; symlinks, common secret suffixes and selected credential formats are rejected (`scripts/export-source.sh:8–30`). | It recursively includes files under allowed directories, including untracked ones. Its regex is narrower than this audit (e.g. not generic provider/API keys). Use it as defense in depth, not a final secret audit. |

`OPEN_SOURCE_REVIEW.md` is an earlier review record, not this audit's test output; its earlier counts and cleanup statements are not independently inherited as proof. Its duplicated Chinese prose was removed under the user's additional document-language instruction, retaining the English account. A final publication check should inspect the exact artifact/ref intended for public access and verify the remote separately without mixing old private history.

## Dependency verification and known-vulnerability checks

`go list -mod=readonly -m -json all` resolved 16 external modules for CLI and 77 for the nested GUI module. The GUI graph includes those CLI modules; there are 77 unique external module paths in the union. The one GUI replacement points to the local Host module, not an external copied library. `go mod verify` passed for the root; GUI verification also passed. Both root and GUI `go mod verify` passed. Root preserved licenses and module version records matched.

Root/GUI vet, root race tests and GUI race tests with `-tags ci` passed on Go 1.27.4/macOS arm64. `govulncheck v1.9.0 -json ./...` completed for both modules. CLI had no finding. GUI had **50 finding records covering 30 advisory IDs**, including six symbol-level traces covering four TIFF advisories in `golang.org/x/image v0.24.0`; the remaining IDs were module/package-level matches without a vulnerable-function trace. JSON-mode exit 0 is not a clean verdict. See [the vulnerability triage](SECURITY_PRIVACY.md#known-vulnerable-desktop-dependencies-rel-03) for the distinction between static reachability and attacker-controlled input.

These scanner findings concern security, not an Apache-2.0 license conflict. REL-03/P1 requires dependency triage before desktop release: update compatible affected versions with GUI regression checks, or record a concrete input non-exposure decision and residual risk. The [Go vulnerability documentation](https://go.dev/doc/security/vuln/) explains call-aware analysis. Repeat on the actual release toolchain and materially different native targets; this local result cannot clear all platforms or the minimum Go version.

## Resolved-module inventory

The following is the complete external-module graph inventory from this audit, not a claim that every row is linked into every binary. `CLI+GUI` means present in both graphs; `GUI` means only the desktop graph. Root-license classification is based on cached upstream text, with nested exceptions above. BSD variants marked for clause review should retain their exact original text, not a replacement template. Build/test-only and platform-specific entries are deliberately retained to avoid hiding compliance scope.

Versions below are the audited local graph, not recommendations to upgrade. Actual release artifacts must carry the module and toolchain inventory produced by their own build.

| Module | Resolved version | Graph | Root license / exception | Root notice files |
| --- | --- | --- | --- | --- |
| `cloud.google.com/go/compute/metadata` | `v0.3.0` | CLI+GUI | Apache-2.0 | `LICENSE` |
| `fyne.io/fyne/v2` | `v2.8.2` | GUI | BSD-3-Clause | `LICENSE` |
| `fyne.io/systray` | `v1.12.3-0.20260810170012-af4e8e793ec4` | GUI | Apache-2.0 | `LICENSE` |
| `github.com/BurntSushi/toml` | `v1.6.0` | GUI | MIT | `COPYING` |
| `github.com/FyshOS/fancyfs` | `v0.0.1` | GUI | BSD-3-Clause | `LICENSE` |
| `github.com/akavel/rsrc` | `v0.10.2` | GUI | MIT | `LICENSE.txt` |
| `github.com/anthonynsimon/bild` | `v0.14.0` | GUI | MIT | `LICENSE` |
| `github.com/clipperhouse/uax29/v2` | `v2.2.0` | GUI | MIT | `LICENSE` |
| `github.com/coder/acp-go-sdk` | `v0.13.5` | CLI+GUI | Apache-2.0 | `LICENSE` |
| `github.com/coder/websocket` | `v1.8.14` | CLI+GUI | ISC | `LICENSE.txt` |
| `github.com/cpuguy83/go-md2man/v2` | `v2.0.7` | GUI | MIT | `LICENSE.md` |
| `github.com/creack/pty` | `v1.1.24` | CLI+GUI | MIT | `LICENSE` |
| `github.com/davecgh/go-spew` | `v1.1.1` | GUI | ISC | `LICENSE` |
| `github.com/fogleman/gg` | `v1.3.0` | GUI | MIT | `LICENSE.md` |
| `github.com/fredbi/uri` | `v1.1.1` | GUI | MIT | `LICENSE.md` |
| `github.com/fsnotify/fsnotify` | `v1.9.0` | GUI | BSD-3-Clause | `LICENSE` |
| `github.com/fyne-io/gl-js` | `v0.2.1-0.20260315212741-029c47fd27e8` | GUI | BSD-3-Clause | `LICENSE` |
| `github.com/fyne-io/glfw-js` | `v0.4.0` | GUI | MIT | `LICENSE` |
| `github.com/fyne-io/image` | `v0.1.1` | GUI | BSD-3-Clause | `LICENSE` |
| `github.com/fyne-io/oksvg` | `v0.2.1-0.20260918172519-a9af55fa95d9` | GUI | BSD-3-Clause | `LICENSE` |
| `github.com/go-gl/gl` | `v0.0.0-20260331235117-4566fea9a276` | GUI | MIT | `LICENSE` |
| `github.com/go-gl/glfw/v3.4/glfw` | `v0.1.0-pre.2` | GUI | BSD-3-Clause | `LICENSE` |
| `github.com/go-ole/go-ole` | `v1.3.0` | GUI | MIT | `LICENSE` |
| `github.com/go-text/render` | `v0.2.1` | GUI | BSD-3-Clause | `LICENSE` |
| `github.com/go-text/typesetting` | `v0.3.5` | GUI | BSD-3-Clause | `LICENSE` |
| `github.com/go-text/typesetting-utils` | `v0.0.0-20260419141703-4ffe8874dabc` | GUI | BSD-3-Clause | `LICENSE` |
| `github.com/godbus/dbus/v5` | `v5.2.2` | GUI | BSD (check original clauses) | `LICENSE` |
| `github.com/golang-jwt/jwt/v5` | `v5.3.1` | CLI+GUI | MIT | `LICENSE` |
| `github.com/golang/freetype` | `v0.0.0-20170609003504-e2365dfdc4a0` | GUI | FreeType License OR GPL-2.0-or-later (graph only) | `LICENSE` |
| `github.com/google/go-cmp` | `v0.7.0` | CLI+GUI | BSD-3-Clause | `LICENSE` |
| `github.com/google/jsonschema-go` | `v0.4.3` | CLI+GUI | MIT | `LICENSE` |
| `github.com/hack-pad/go-indexeddb` | `v0.3.2` | GUI | Apache-2.0 | `LICENSE` |
| `github.com/hack-pad/safejs` | `v0.1.0` | GUI | Apache-2.0 | `LICENSE` |
| `github.com/inconshreveable/mousetrap` | `v1.0.0` | GUI | Apache-2.0 | `LICENSE` |
| `github.com/jackmordaunt/icns/v2` | `v2.2.7` | GUI | MIT | `LICENSE` |
| `github.com/jeandeaual/go-locale` | `v0.0.0-20250612000132-0ef82f21eade` | GUI | MIT | `LICENSE` |
| `github.com/josephspurrier/goversioninfo` | `v1.7.0` | GUI | MIT | `LICENSE` |
| `github.com/jsummers/gobmp` | `v0.0.0-20230614200233-a9de23ed2e25` | GUI | MIT | `COPYING.txt` |
| `github.com/kr/text` | `v0.2.0` | GUI | MIT | `License` |
| `github.com/lucor/goinfo` | `v0.9.0` | GUI | BSD-3-Clause | `LICENSE` |
| `github.com/mattn/go-runewidth` | `v0.0.24` | GUI | MIT | `LICENSE` |
| `github.com/mcuadros/go-version` | `v0.0.0-20190830083331-035f6764e8d2` | GUI | MIT | `LICENSE` |
| `github.com/modelcontextprotocol/go-sdk` | `v1.8.0` | CLI+GUI | Apache-2.0 / MIT transition; docs CC-BY-4.0 | `LICENSE` |
| `github.com/natefinch/atomic` | `v1.0.1` | GUI | MIT | `LICENSE` |
| `github.com/nfnt/resize` | `v0.0.0-20180221191011-83c6a9932646` | GUI | ISC | `LICENSE` |
| `github.com/nicksnyder/go-i18n/v2` | `v2.5.1` | GUI | MIT | `LICENSE` |
| `github.com/niemeyer/pretty` | `v0.0.0-20200227124842-a10e7caefd8e` | GUI | MIT | `License` |
| `github.com/pmezard/go-difflib` | `v1.0.0` | GUI | BSD (check original clauses) | `LICENSE` |
| `github.com/russross/blackfriday/v2` | `v2.1.0` | GUI | BSD (check original clauses) | `LICENSE.txt` |
| `github.com/rymdport/portal` | `v0.4.2` | GUI | Apache-2.0 | `LICENSE` |
| `github.com/segmentio/asm` | `v1.1.3` | CLI+GUI | MIT | `LICENSE` |
| `github.com/segmentio/encoding` | `v0.5.4` | CLI+GUI | MIT | `LICENSE` |
| `github.com/spf13/cobra` | `v0.0.5` | GUI | Apache-2.0 | `LICENSE.txt` |
| `github.com/spf13/pflag` | `v1.0.3` | GUI | BSD-3-Clause | `LICENSE` |
| `github.com/srwiley/oksvg` | `v0.0.0-20221011165216-be6e8873101c` | GUI | BSD-3-Clause | `LICENSE` |
| `github.com/srwiley/rasterx` | `v0.0.0-20220730225603-2ab79fcdd4ef` | GUI | BSD-3-Clause | `LICENSE` |
| `github.com/stretchr/objx` | `v0.5.2` | GUI | MIT | `LICENSE` |
| `github.com/stretchr/testify` | `v1.11.1` | GUI | MIT | `LICENSE` |
| `github.com/urfave/cli/v2` | `v2.27.7` | GUI | MIT | `LICENSE` |
| `github.com/xrash/smetrics` | `v0.0.0-20240521201337-686a1a2994c1` | GUI | MIT | `LICENSE` |
| `github.com/yosida95/uritemplate/v3` | `v3.0.2` | CLI+GUI | BSD-3-Clause | `LICENSE` |
| `github.com/yuin/goldmark` | `v1.8.2` | GUI | MIT | `LICENSE` |
| `golang.org/x/crypto` | `v0.33.0` | GUI | BSD-3-Clause | `LICENSE`, `PATENTS` |
| `golang.org/x/image` | `v0.24.0` | GUI | BSD-3-Clause | `LICENSE`, `PATENTS` |
| `golang.org/x/mobile` | `v0.0.0-20231127183840-76ac6878050a` | GUI | BSD-3-Clause | `LICENSE`, `PATENTS` |
| `golang.org/x/mod` | `v0.23.0` | GUI | BSD-3-Clause | `LICENSE`, `PATENTS` |
| `golang.org/x/net` | `v0.35.0` | GUI | BSD-3-Clause | `LICENSE`, `PATENTS` |
| `golang.org/x/oauth2` | `v0.35.0` | CLI+GUI | BSD-3-Clause | `LICENSE` |
| `golang.org/x/sync` | `v0.20.0` | CLI+GUI | BSD-3-Clause | `LICENSE`, `PATENTS` |
| `golang.org/x/sys` | `v0.47.0` | CLI+GUI | BSD-3-Clause | `LICENSE`, `PATENTS` |
| `golang.org/x/term` | `v0.29.0` | GUI | BSD-3-Clause | `LICENSE`, `PATENTS` |
| `golang.org/x/text` | `v0.22.0` | GUI | BSD-3-Clause | `LICENSE`, `PATENTS` |
| `golang.org/x/time` | `v0.15.0` | CLI+GUI | BSD-3-Clause | `LICENSE`, `PATENTS` |
| `golang.org/x/tools` | `v0.42.0` | CLI+GUI | BSD-3-Clause | `LICENSE`, `PATENTS` |
| `golang.org/x/tools/go/vcs` | `v0.1.0-deprecated` | GUI | BSD-3-Clause | `LICENSE` |
| `gopkg.in/check.v1` | `v1.0.0-20200227125254-8fa46927fb4f` | GUI | BSD (check original clauses) | `LICENSE` |
| `gopkg.in/yaml.v3` | `v3.0.1` | GUI | Apache-2.0 | `LICENSE`, `NOTICE` |

Go runtime/toolchain notices are a separate required component, preserved under `third_party/go/` and replaced with the actual build toolchain texts by `tools/build/main.go:254–260`. Original Host schema/translation files are embedded at `pkg/pluginsdk/plugin_ui.go:13–16` and `cmd/nimotehost-gui/locale.go:16–17`; their rights depend on LIC-02, rather than a new third-party dependency inferred from the file format.

## Desktop vulnerability scan inventory (REL-03)

This table preserves every advisory ID with a finding record in the completed native desktop scan. Module/package-only matches are not established exploitable vulnerabilities. Fixed versions are the scanner's advisory-specific versions, not a tested upgrade plan. Evidence and minimum remediation are in [SECURITY_PRIVACY.md](SECURITY_PRIVACY.md#known-vulnerable-desktop-dependencies-rel-03); priority P1 for release triage, no protocol effect, GUI regression checks required for any update.

| Advisory | Current module/version | Highest observed evidence | Advisory fixed version |
| --- | --- | --- | --- |
| [GO-2025-3503](https://pkg.go.dev/vuln/GO-2025-3503) | `golang.org/x/net v0.35.0` | Module/package match; no vulnerable-symbol call trace | `v0.36.0` |
| [GO-2025-3595](https://pkg.go.dev/vuln/GO-2025-3595) | `golang.org/x/net v0.35.0` | Module/package match; no vulnerable-symbol call trace | `v0.38.0` |
| [GO-2026-4440](https://pkg.go.dev/vuln/GO-2026-4440) | `golang.org/x/net v0.35.0` | Module/package match; no vulnerable-symbol call trace | `v0.45.0` |
| [GO-2026-4441](https://pkg.go.dev/vuln/GO-2026-4441) | `golang.org/x/net v0.35.0` | Module/package match; no vulnerable-symbol call trace | `v0.45.0` |
| [GO-2026-4815](https://pkg.go.dev/vuln/GO-2026-4815) | `golang.org/x/image v0.24.0` | Static vulnerable-symbol call path; hostile input unverified | `v0.38.0` |
| [GO-2026-4918](https://pkg.go.dev/vuln/GO-2026-4918) | `golang.org/x/net v0.35.0` | Module/package match; no vulnerable-symbol call trace | `v0.53.0` |
| [GO-2026-4961](https://pkg.go.dev/vuln/GO-2026-4961) | `golang.org/x/image v0.24.0` | Module/package match; no vulnerable-symbol call trace | `v0.42.0` |
| [GO-2026-4962](https://pkg.go.dev/vuln/GO-2026-4962) | `golang.org/x/image v0.24.0` | Module/package match; no vulnerable-symbol call trace | `v0.39.0` |
| [GO-2026-5025](https://pkg.go.dev/vuln/GO-2026-5025) | `golang.org/x/net v0.35.0` | Module/package match; no vulnerable-symbol call trace | `v0.55.0` |
| [GO-2026-5026](https://pkg.go.dev/vuln/GO-2026-5026) | `golang.org/x/net v0.35.0` | Module/package match; no vulnerable-symbol call trace | `v0.55.0` |
| [GO-2026-5027](https://pkg.go.dev/vuln/GO-2026-5027) | `golang.org/x/net v0.35.0` | Module/package match; no vulnerable-symbol call trace | `v0.55.0` |
| [GO-2026-5028](https://pkg.go.dev/vuln/GO-2026-5028) | `golang.org/x/net v0.35.0` | Module/package match; no vulnerable-symbol call trace | `v0.55.0` |
| [GO-2026-5029](https://pkg.go.dev/vuln/GO-2026-5029) | `golang.org/x/net v0.35.0` | Module/package match; no vulnerable-symbol call trace | `v0.55.0` |
| [GO-2026-5030](https://pkg.go.dev/vuln/GO-2026-5030) | `golang.org/x/net v0.35.0` | Module/package match; no vulnerable-symbol call trace | `v0.55.0` |
| [GO-2026-5031](https://pkg.go.dev/vuln/GO-2026-5031) | `golang.org/x/image v0.24.0` | Module/package match; no vulnerable-symbol call trace | `v0.41.0` |
| [GO-2026-5032](https://pkg.go.dev/vuln/GO-2026-5032) | `golang.org/x/image v0.24.0` | Static vulnerable-symbol call path; hostile input unverified | `v0.41.0` |
| [GO-2026-5061](https://pkg.go.dev/vuln/GO-2026-5061) | `golang.org/x/image v0.24.0` | Module/package match; no vulnerable-symbol call trace | `v0.43.0` |
| [GO-2026-5062](https://pkg.go.dev/vuln/GO-2026-5062) | `golang.org/x/image v0.24.0` | Static vulnerable-symbol call path; hostile input unverified | `v0.43.0` |
| [GO-2026-5066](https://pkg.go.dev/vuln/GO-2026-5066) | `golang.org/x/image v0.24.0` | Static vulnerable-symbol call path; hostile input unverified | `v0.43.0` |
| [GO-2026-5942](https://pkg.go.dev/vuln/GO-2026-5942) | `golang.org/x/net v0.35.0` | Module/package match; no vulnerable-symbol call trace | `v0.56.0` |
| [GO-2026-5970](https://pkg.go.dev/vuln/GO-2026-5970) | `golang.org/x/text v0.22.0` | Module/package match; no vulnerable-symbol call trace | `v0.39.0` |
| [GO-2026-6222](https://pkg.go.dev/vuln/GO-2026-6222) | `golang.org/x/image v0.24.0` | Module/package match; no vulnerable-symbol call trace | `v0.45.0` |
| [GO-2026-6603](https://pkg.go.dev/vuln/GO-2026-6603) | `golang.org/x/net v0.35.0` | Module/package match; no vulnerable-symbol call trace | `v0.60.0` |
| [GO-2026-6610](https://pkg.go.dev/vuln/GO-2026-6610) | `golang.org/x/net v0.35.0` | Module/package match; no vulnerable-symbol call trace | `v0.60.0` |
| [GO-2026-6611](https://pkg.go.dev/vuln/GO-2026-6611) | `golang.org/x/net v0.35.0` | Module/package match; no vulnerable-symbol call trace | `v0.60.0` |
| [GO-2026-6612](https://pkg.go.dev/vuln/GO-2026-6612) | `golang.org/x/net v0.35.0` | Module/package match; no vulnerable-symbol call trace | `v0.60.0` |
| [GO-2026-6617](https://pkg.go.dev/vuln/GO-2026-6617) | `golang.org/x/net v0.35.0` | Module/package match; no vulnerable-symbol call trace | `v0.60.0` |
| [GO-2026-6629](https://pkg.go.dev/vuln/GO-2026-6629) | `golang.org/x/text v0.22.0` | Module/package match; no vulnerable-symbol call trace | `v0.41.0` |
| [GO-2026-6860](https://pkg.go.dev/vuln/GO-2026-6860) | `golang.org/x/net v0.35.0` | Module/package match; no vulnerable-symbol call trace | `v0.62.0` |
| [GO-2026-6866](https://pkg.go.dev/vuln/GO-2026-6866) | `golang.org/x/net v0.35.0` | Module/package match; no vulnerable-symbol call trace | `v0.62.0` |
