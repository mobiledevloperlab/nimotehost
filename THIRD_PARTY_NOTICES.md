# Third-party notices

**English** | [简体中文](THIRD_PARTY_NOTICES.zh-CN.md)

NimoteHost code and documentation are licensed under Apache-2.0. Third-party
components retain their own copyrights and licenses; their original license
texts are preserved in `third_party/`, and copied to `licenses/` in binary
bundles. No third-party source code is vendored here.

| Component | Version | License | Source | License text |
| --- | --- | --- | --- | --- |
| creack/pty | v1.1.24 | MIT | https://github.com/creack/pty | [LICENSE](third_party/pty/LICENSE) |
| golang.org/x/sys | v0.47.0 | BSD-3-Clause, additional patent grant | https://go.googlesource.com/sys | [LICENSE](third_party/x-sys/LICENSE), [PATENTS](third_party/x-sys/PATENTS) |
| Go runtime/toolchain | build's Go version (minimum 1.26.5) | BSD-3-Clause, additional patent grant | https://go.dev | [LICENSE](third_party/go/LICENSE), [PATENTS](third_party/go/PATENTS) |

`third_party/dependencies.json` records the reviewed dependency versions. The
build rejects module changes or replacements without an updated license record.
`MODULES.json` and `BUILD.json` in each binary bundle identify its actual module
versions, toolchain and build settings. These are build provenance records,
not a claim of an SPDX/CycloneDX SBOM or a full file-level license audit.

Agent executables and system SSH are not bundled by Host. Their respective
licenses and service terms continue to apply to the user's installations.

## Protocol SDK dependencies

MCP uses the official Go SDK. Agent sessions and history queries reuse the generic
JSON-RPC connection from Coder’s community ACP Go SDK, with a small Codex wire adapter.
The MCP SDK license describes its Apache-2.0 / MIT transition; preserve the complete text.
Transitive modules, including build/test dependencies, are recorded below and in `third_party/dependencies.json`.

| Module | Version | License files |
| --- | --- | --- |
| cloud.google.com/go/compute/metadata | v0.3.0 | [LICENSE](third_party/cloud.google.com-go-compute-metadata/LICENSE) |
| github.com/coder/acp-go-sdk | v0.13.5 | [LICENSE](third_party/coder-acp-go-sdk/LICENSE) |
| github.com/golang-jwt/jwt/v5 | v5.3.1 | [LICENSE](third_party/golang-jwt-jwt-v5/LICENSE) |
| github.com/google/go-cmp | v0.7.0 | [LICENSE](third_party/google-go-cmp/LICENSE) |
| github.com/google/jsonschema-go | v0.4.3 | [LICENSE](third_party/google-jsonschema-go/LICENSE) |
| github.com/modelcontextprotocol/go-sdk | v1.8.0 | [LICENSE](third_party/modelcontextprotocol-go-sdk/LICENSE) |
| github.com/segmentio/asm | v1.1.3 | [LICENSE](third_party/segmentio-asm/LICENSE) |
| github.com/segmentio/encoding | v0.5.4 | [LICENSE](third_party/segmentio-encoding/LICENSE) |
| github.com/yosida95/uritemplate/v3 | v3.0.2 | [LICENSE](third_party/yosida95-uritemplate-v3/LICENSE) |
| golang.org/x/oauth2 | v0.35.0 | [LICENSE](third_party/x-oauth2/LICENSE) |
| golang.org/x/sync | v0.20.0 | [LICENSE](third_party/x-sync/LICENSE), [PATENTS](third_party/x-sync/PATENTS) |
| golang.org/x/time | v0.15.0 | [LICENSE](third_party/x-time/LICENSE), [PATENTS](third_party/x-time/PATENTS) |
| golang.org/x/tools | v0.42.0 | [LICENSE](third_party/x-tools/LICENSE), [PATENTS](third_party/x-tools/PATENTS) |

## Desktop dependencies / 桌面依赖

Fyne and its transitive dependencies are isolated in
`cmd/nimotehost-gui/go.mod` / `go.sum`; the CLI dependency manifest is unchanged.
Desktop distributions carry `GUI-MODULES.json` and upstream LICENSE/COPYING/NOTICE
files in `licenses-gui/`, in addition to the CLI licenses. The builder fails if a
resolved desktop module has no root license file. Fyne uses the BSD 3-Clause
license; consult each bundled notice for its dependencies. These libraries are not
linked into the CLI. Fyne 及其依赖仅用于桌面版，随桌面包附完整上游许可证。
