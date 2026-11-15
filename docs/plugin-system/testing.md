# Testing — English / 简体中文

Current implementation: HCP **1.0**, existing Host transport **1**.

Final verification: `go test -mod=readonly -race ./...` passed for all root-module
packages (Host package 190.223 seconds); `go vet ./...`, module integrity, public
schema compatibility and documentation checks passed. The standalone Inspector
was also built/packaged and passed real stdio negotiation and metadata-address
SSRF rejection.

Tests run on macOS arm64 in this workspace:

- Public SDK negotiation, initialization gate, unknown methods, error redaction and
  oversized frames.
- Runtime concurrent calls across two plugins, metadata-copy isolation, schema
  rejection, cancel/recovery, persisted grants, restart, disable/uninstall, explicit
  update, changed executable hashes, exact permission grants and bounded restart
  exhaustion. Invalid installed packages do not stop registry initialization.
- Epoch/cursor expiry and reconnect, receipt conflict/deduplication and refusal to
  replay retired results. Public Manifest templates validate against the published
  schema; all protocol schemas compile.
- A separately built executable imports only public SDKs. The real Host process
  supervisor installs/starts it, discovers its capabilities, calls it through the
  authenticated HTTP boundary, recovers from process exit, kills blocked work and
  starts a dynamically discovered ACP Agent with existing Session/Agent events.
  Tests reject owner/device/observer/MCP permission bypasses and environment leaks.
- HTTP Inspector tests GET/POST/PUT/DELETE, query encoding, headers/body, JSON/text,
  response status, timeout/cancel, request/response limits, redirects, sensitive
  headers and safe error messages. SSRF tests cover local/private/link-local/CGNAT,
  mapped/transition/reserved addresses, mixed DNS answers, and numeric dialing of
  checked addresses. Network tests use local fixtures and injected dialing;
  they do not send requests to public APIs or metadata services.

Commands:

```sh
go test -mod=readonly -race ./...
go vet ./...
GOOS=linux GOARCH=amd64 go test -exec=true ./...
GOOS=windows GOARCH=amd64 go test -exec=true ./...
```

`-exec=true` checks compilation and **does not execute** platform tests. The
existing `.github/workflows/ci.yml` native matrix already runs all new packages,
including race detection, on Ubuntu, macOS and Windows. Only macOS native results
have been collected here; Linux/Windows native CI acceptance remains pending.
Cross-compilation is not evidence for Windows file-sharing/Job behavior or Linux
cgroup runtime behavior. There is no independent security audit claim.

Remaining validation: native Windows/Linux plugin lifecycle/ACP tests under CI,
configured plugin quotas under delegated Linux cgroups/Windows Jobs, extended
hostile-peer fuzzing, Host SIGKILL plugin cleanup acceptance, and external
NimoteCode integration against the versioned spec. See implementation summary
for final command outcomes. Core existing process/SSH/approval tests remain in
the full suite; no production Host state or external client repository is modified.

简体中文：本机执行 macOS 原生测试和竞态检测；Linux/Windows 仅交叉编译，原生验收
仍须运行现有 CI。安全测试不访问公网或真实凭据；未宣称进行独立安全审计。

## Desktop follow-up

Native macOS GUI module `go test -mod=readonly -race ./...` and `go vet ./...`
passed, including existing window lifecycle/language/layout tests and new plugin
navigation, ready-state invocation gating and schema-default tests. The desktop
facade race tests verify authenticated HCP transport, exact manifest review hashes,
protocol mismatch rejection and invocation request IDs. GUI build/install evidence
is recorded in implementation-summary.md.


## GitHub catalog follow-up

Added automated coverage for metadata-only index/schema compatibility, case-insensitive
search, source failures, manifest review, independent Release asset download and
shared local/remote installation. Tests verify wrong Manifest/binary digests,
changed reviewed metadata, mismatched plugin IDs, missing platform assets, disabled
updates, source trust confirmation/persistence/revocation, cancellation and staging
cleanup, redirect/credential safety, bounded streaming downloads and owner-only RPC.
The release packer test generates both local bundles and remote assets, then verifies
the catalog → Manifest → executable SHA256 chain. GUI tests check that both local and
catalog entry points remain available; catalog strings are covered in all six languages.

Network tests inject transports and never publish a plugin, edit a remote GitHub
repository, or execute an unreviewed downloaded program. The default public index is
empty and needs repository publication before its new remote path can be read.
Actual public catalog/release publication and Linux/Windows native execution are
separate acceptance steps, not silently treated as completed by local tests.

Live post-install search returned a structured unavailable-source result. A separate
direct HTTPS probe to GitHub was reset by the current network, so public-network
download acceptance is explicitly pending; only controlled transport tests passed
for the full download flow. No proxy/system-network configuration was modified.
