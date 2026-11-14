# Host hardening tests / 加固测试记录

English / 简体中文 · 2026-11-14

## Environment and interpretation / 环境及结论边界

Actual native environment: macOS Darwin 25.2.0 arm64, Go 1.27.4;
module minimum remains Go 1.26.5. No module versions or dependencies changed.
Linux/Windows results below mean compilation, not native execution. No real
SSH server configuration, installed user-service registration, mobile device or
model request was performed. Existing integration fixtures exercise real child
processes and the SDK, not production Agent/model acceptance.

Final native root regression: **354 passed tests/subtests, four explicit existing
environment skips, zero failures** (`go test -json ./...`). The count includes
nested subtests and helper tests, not 354 independent end-to-end scenarios.
The native race regression passed for every root package, with no reported race.

## Commands and actual results / 命令及实际结果

| Executed / 已执行 | Result / 实际结果 |
| --- | --- |
| `go test ./...` and final `go test -json ./...` | PASS; six tested root packages, tools/install has no test files; final 354 pass / 4 skip |
| `go test -race ./...` | PASS on final production source; no race report |
| `go vet ./...` | PASS; no diagnostics |
| `make check` | PASS; five Python checks, go mod verify, readonly race regression and vet |
| `python3 -m unittest discover -s test -p 'test_*.py'` | PASS; 5 checks, also executed through make/clean-source |
| `go mod verify` | PASS; all modules verified |
| `go test -mod=readonly -race -tags ci ./...` in desktop module | PASS, 33.373s; headless Fyne driver, no UI code modified |
| `go vet ./...` and `go vet -tags ci ./...` in desktop module | PASS |
| `go test ./internal/host -run '^$' -fuzz '^FuzzObserverBoundary$' -fuzztime=5s -parallel=4` | PASS; 312,606 executions, four workers, no failing corpus |
| `go run ./tools/build` and six explicit OS/architecture builds | PASS; complete bundles/archives/manifests/checksums |
| `GOOS=<target> GOARCH=<arch> CGO_ENABLED=0 go test -mod=readonly -c -o <temporary-directory> ./...` for all six targets | PASS; compiles test code without pretending to execute target binaries |
| `sh scripts/check-clean-build.sh` | PASS on final production source; fresh module/build caches, readonly race/vet and native bundle |
| `./scripts/export-source.sh` | PASS; standalone source archive produced without installing services |
| `git diff --check`, `gofmt -l` on changed Go trees | PASS; no whitespace errors or unformatted Go files |

Six production/test compilation targets: darwin/amd64, darwin/arm64,
linux/amd64, linux/arm64, windows/amd64, windows/arm64. Each target build used
the repository tools/build pipeline; test executables went to temporary directories.
Only darwin/arm64 actually ran the tests. GUI production packaging/signing and
Linux/Windows desktop environments were not tested this round.

Desktop linking emitted the existing Apple linker warning about duplicate `-lobjc`;
the build/test returned exit 0. No check was disabled to hide that diagnostic.
There is no separate root linter configuration beyond gofmt/vet/repository checks.

## Regression coverage / 用例与异常场景

| File / 文件 | Cases and assertion / 覆盖与断言 |
| --- | --- |
| internal/host/hardening_devices_test.go | Both scoped roles; filtered session lists and events; output/activity/Agent-state cross-session denial; write/history/Host-management denial; safe cancel/decline and keyed retries; malformed scopes/roles; owner/device receipt separation; restart deduplication; concurrent reads and durable revocation/corruption fail closed; maximum 256-session record authenticates within 32 KiB, 257-session and oversized-file rejection |
| cmd/nimotehost/devices_test.go | Actual CLI scoped-device creation/list and owner-role rejection; existing forced-command non-RPC rejection remains |
| internal/host/hardening_approvals_test.go | Foreign/malformed/stale thread/turn and idle/canceled requests; unoffered native accepts; 32 concurrent approval attempts produce one queued response; queued accept becomes decline/cancel at native turn completion; Core message retry hashes reject changed action/content across all three providers |
| internal/host/hardening_resources_test.go | Defaults and policy parsing; CPU/RSS unsupported rejection; twelve concurrent starts admit exactly two; cancellation releases capacity; impossible disk floor rejects before launch; real one-second runtime monitor; injected low-disk/output errors stop child and persist failure; receipt reload keeps resource classification; HTTP/helper backpressure; read pressure preserves mutation reserve; uncertain outcomes have distinct code without a capacity-retry hint |
| internal/host/hardening_process_unix_test.go | Real shell starts a descendant writing a heartbeat file; after root/session completion the file stops growing; same process-group scope only; resource-failed Agent startup keeps one failed structured session and never launches a terminal fallback |
| internal/host/hardening_transport_test.go | Output blocked in a closeable Writer releases on cancellation; duplicate concurrent stream IDs rejected; corrupt/oversized/reversed/overflow event journal fails closed; future output cursor preserves legacy empty-page behavior while signaling resync; stale legacy metadata uses actual .log boundary; repeated Close wakes readers and rejects writes |
| internal/host/hardening_adapter_test.go | Known protocol descriptors; missing and unknown providers; truthful unsupported quota capability flags; explicit fake executable version success/failure/timeout/cancel; capabilities polling does not execute --version |
| internal/desktop/manager_test.go | Existing real CLI daemon survives manager exit; restricted device bridge/revocation, duplicate start/auth failure/restart/config locks remain; compare stable status and random instance_id; explicit restart must change instance identity |
| Existing root regression | PTY/ConPTY platform-specific code, structured Agent SDK lifecycle, steering, approvals/policies, history/resume/truncation, durable receipts/retirement, output/activity/events, config, service definitions, install/rollback and archive/license boundaries remain covered |
| tools/build/docs_test.go and test/test_docs.py | Exact four-document allowlist; language/navigation, local links and complete RPC reference assertions retained |

Disk/output fault tests inject failures; they do not fill the developer's disk or
simulate kernel OOM. The runtime test executes an actual child and waits for the
Manager monitor. Cross-platform compilation does not replace native syscall tests.
No test was deleted, and assertions were not weakened to suppress a failure.

## Failures found and fixes / 发现并修复的问题

1. The first documentation regression rejected newly requested hardening docs
   because the repository explicitly allowed only completed.md/pending.md. Update
   the exact expected set to four files; retain all content/link/protocol checks.
2. Initial future-output-cursor rejection contradicted existing
   TestOutputAfterEndIsEmpty. Preserve its exact legacy behavior; expose additive
   resync_required instead. Add a regression for old .log records with no byte count.
3. Adapter consolidation changed Claude's established missing-adapter diagnostic.
   Restore the original actionable wording; existing fallback assertions pass.
4. New dynamic free-disk status made the old byte-for-byte status comparison
   unsuitable for proving daemon identity. Preserve comparison of all stable
   fields, add a random per-daemon instance_id and verify explicit restart changes
   it; do not infer replacement from unrelated free-space changes.
5. Final review moved resource-monitor creation under first process admission;
   closing an unused/recovered-only Manager needs no worker, and start/close are
   serialized. Final root/race tests run against this implementation.

6. Added a resource-failure guard to auto startup: an enforced timeout/disk/output
   failure must not be mistaken for protocol incompatibility and launch a terminal.
   Its new test exposed that cancel(false) relabeled a settling resource failure
   as canceled. Kill the owned handle without that state mutation and preserve
   the failed record. The unchanged assertions now pass.
7. Under concurrent clean/race builds, the one-second fixture could expire before
   its script created the launch marker. Synchronize on actual fixture startup
   before injecting the resource deadline; retain all no-fallback/failed-history
   assertions. The regular runtime test still exercises the real one-second
   monitor. The focused fallback test passed 20 consecutive race-enabled runs.

8. Boundary review found that 256 valid session IDs exceed the old 16 KiB
   private-file budget. Keep credential/resource files at 16 KiB, allow device
   records up to 32 KiB, and enforce a bounded read even if a file grows after
   stat. The new maximum-scope test verifies authentication at 256 IDs and refusal
   of 257 IDs/oversized files. It and the fallback regression passed three repeated
   race-enabled runs together.

## Explicit skips and remaining validation / 明确跳过与未验收

The final root JSON report lists these four existing environment-gated skips:

- hostservice: TestLinuxDefinitionWithSystemdParser (no native systemd parser on macOS).
- host: TestInstalledOpenCodeHandshake (explicit installed-provider opt-in absent).
- host: TestInstalledClaudeAdapterStartup (explicit installed-provider opt-in absent).
- host: TestInstalledCodexHandshake (explicit installed-provider opt-in absent).

No test was newly skipped to bypass a failing implementation. POSIX fixture code
in the new tests is also explicitly non-native on Windows; only test compilation
was performed there. Native Windows ConPTY/jobs/free-disk, Linux process/resource
behavior, actual forced-command SSH isolation/revocation, service login/logout,
SIGKILL/OS restart, long-duration multi-session saturation and real Agent model
approval/resume remain release acceptance work. Go 1.26.5 runtime is not separately
verified on this workstation. Self-review is not independent security review.

Full execution-capable device sandboxing, hard CPU/RSS quotas and Unix crash/escaped
process containment remain unimplemented; applications must not use these test
results to claim those guarantees or formal release readiness. See
[remaining work](pending.md) and [architecture/rollback](host-hardening.md).


## Final file scope / 最终修改范围

Production changes are confined to internal/host (device ACL, approvals/request
hashes, resource monitor/platform probes, lifecycle/output/events, adapter registry,
capabilities and RPC), cmd/nimotehost (scoped-device and explicit Agent diagnostics
commands/help). No UI or NimoteCode source changed; no service registration or
running installation was modified.

Regression-only changes outside those paths: internal/desktop/manager_test.go and
tools/build/docs_test.go. README.md/README.zh-CN.md point to the four maintained
state/audit/test documents. Generated distribution outputs remain in ignored dist/.

All final checks returned exit 0. Full raw local outputs were inspected; the table
above records their actual conclusions, including skips and platform limitations.
