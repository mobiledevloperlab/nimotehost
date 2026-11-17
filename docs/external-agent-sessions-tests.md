# External Agent session test record

Updated: 2026-11-17. Local macOS arm64, Go 1.27.4; module minimum is Go 1.26.5. Go checks used `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`; tests themselves install/download no Agents. They use temporary directories, test-created services and protocol fixtures, never existing user tasks.

## Claude discovery and real ACP increment

Claude Code 2.1.311 and open-source ACP adapter 0.107.3 were separately installed at the user's request. No external model requests or user conversation reads were performed. CLI tests used temporary CLAUDE_CONFIG_DIR; ACP tests used fake credentials and local HTTP.

| Check | Recorded result |
| --- | --- |
| Final full race regression | `NIMOTE_TEST_CLAUDE_DISCOVERY=1 NIMOTE_TEST_CLAUDE_ACP=1 NIMOTE_TEST_CODEX_CONTROL=1 go test -mod=readonly -race ./...` passed, exit 0; Host 226.410s, final production source. Conditional real-environment skips are not passes |
| Discovery/installed CLI/ACP/history targets | `NIMOTE_TEST_CLAUDE_DISCOVERY=1 NIMOTE_TEST_CLAUDE_ACP=1 go test -mod=readonly -race ./internal/host -run 'TestClaudeNativeDiscovery\|TestClaudeInstalledACPInitialization\|TestACPHistory\|TestExternalAdapters\|TestAttachedHistory' -count=1` passed, 25.970s |
| Real CLI | Isolated empty-config query passed on 2.1.311, proving command/JSON availability, not real active-session acceptance. Nonempty states use fixtures |
| Real ACP | 0.107.3 v2 initialize, empty list and structured new-session creation passed; native ID appeared through original Session updates, with no terminal fallback. No prompt was sent; real tools/approval/full history/context continuation remain pending |
| Failure/boundary coverage | Malformed/null/oversized output, ID/workspace checks, no fabricated empty IDs, duplicate identity, pagination/deletion, unknown states, no inferred liveness without PID, owner auth, redacted errors, canceled hanging queries and discovery fields rejected for live attach |
| ACP replay | Fixtures require `session/resume` with `replayFrom:{type:start}`; replay-end/control snapshots are not fabricated tool history. Existing cross-session/ACK/bounded-replay tests pass |
| Build/static/docs | macOS arm64 and Linux/Windows amd64 products, Windows amd64 Host test compilation, vet/docs/diff passed; Linux/Windows native execution unverified |
| GUI | `go test -mod=readonly -tags ci ./...` passed, 4.405s; test driver only, existing duplicate `-lobjc` warning remains |

Global npm installation exhausted disk and rolled back. An isolated Host directory omitting optional binaries succeeded and reused the installed CLI. The first real ACP assertion incorrectly treated the initial admission record as an asynchronously updated Session; it was fixed to read the existing updated-record API, then passed without changing Session behavior. Failures were not counted as passes.

## OpenCode v1 and Windows increment

No dependencies were added: existing OpenCode state machine, standard library and `x/sys/windows` were reused. This is not support for every execution mode of all three Agents.

| Check | Recorded result |
| --- | --- |
| Final full race regression | `NIMOTE_TEST_CODEX_CONTROL=1 go test -mod=readonly -race ./...` passed, exit 0; Host 211.027s, including final v1 false-ack refusal and real isolated Codex. Later changes only refined Windows fixture owner preparation/test records, with separate compilation/docs checks |
| v1/v2/Codex/adapters/endpoints | `go test -mod=readonly -race ./internal/host -run 'TestOpenCodeV1\|TestOpenCodeAttach\|TestCodexAttach\|TestExternalSession\|TestExternalAttachment' -count=1` passed, 17.807s |
| Complete v1 targets | `go test -mod=readonly -race ./internal/host -run '^TestOpenCodeV1' -count=1` passed, 6.834s: original ID/workspace, filtered global pending, duplicate approvals, explicit abort, prompt_async, uncertain writes not resent, reconnect, discovery/cursor expiry/deletion/auth, original tool correlation, >50 messages, cross-session parts, budgets, unknown states and native false acknowledgements |
| Windows security/transport compilation | `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -mod=readonly -c ... ./internal/host` passed. `TestCodexWindowsReadOnlyACL`/`TestCodexWindowsControlSocket` cover owner, null DACL, other principals' write/read/delete-child rights, inherited ACEs and real AF_UNIX WebSocket with unchanged ACL. Compile-only; configured native CI was not run remotely in this increment |
| Products/GUI | Three product targets passed; GUI `-tags ci` passed, 3.875s with existing duplicate `-lobjc` warning |
| Static/docs | Root vet, Windows amd64 Host vet and tools/build tests passed |
| Python | 19 passed, 2.143s |

OS names identify source build targets, not native acceptance. Both OpenCode versions used fixtures; real OpenCode was not installed.

## Earlier baseline

| Check | Recorded result |
| --- | --- |
| Two attach transports, targeted race | Passed, 10.845s; added owner/out-of-order approval targets passed, 12.412s |
| Real Codex 0.169.0 | `NIMOTE_TEST_CODEX_CONTROL=1 go test -mod=readonly -race -v ./internal/host -run '^TestCodexControlInstalledCLI$'` passed, 5.563s. Isolated CODEX_HOME and fixed local-model responses; attached during a real desktop turn, original work finished after Host closed, reconnected and continued with the same ID/context in model requests. No external model calls |
| Final full race baseline | Same opt-in full race command passed, exit 0; Host 208.418s, including final adapter entrypoints, invalid IDs/notifications, approval races, external policy limits and real Codex context continuation. Other conditional skips are not environment evidence |
| GUI | Final `-tags ci` passed, 4.361s; unit-test driver with existing linker warning |
| Vet | Final source passed; a sandbox cache-read restriction was separately rechecked with cache access |
| Linux/Windows amd64 test compilation | Passed; three final product builds also passed, without native-runtime claims |
| Products | macOS arm64 native plus Linux/Windows amd64 cross `go build -mod=readonly ./...` passed; cross-build CGO disabled |
| Python/integrity/licenses | 19 Python tests, module verify and diff passed; all 16 resolved dependencies match notices; original WebSocket license retained |

## Automated scenarios

| Scenario | Evidence |
| --- | --- |
| Missing CLI/incompatible version/protocol | Adapter/list/ID checks, ACP major rejection, failed attach never creates a replacement session; existing control endpoints do not require CLI installation |
| Original process/session | Codex Unix WebSocket fixture and real CLI; v1/v2 HTTP fixtures accept original session paths only, no create/fork |
| Approval recovery/multiple clients | Original pending IDs, only one of 16 concurrent approves succeeds, desktop-resolved requests reject later phone replies, early resolution never recreates approval, pending before resume-turn snapshots remains correlated |
| Unsupported native requests | Remain desktop-owned until native resolution; no MethodNotFound that fails original work |
| Policy consistency | External connections remain confirm-only, cannot enable auto later; Host-launched policy regressions retain compatibility |
| Upstream interruption | OpenCode disconnected→snapshot recovery; unchanged messages do not duplicate events |
| Lost write acknowledgement | Repeated uncertain request is not resent; changed content under the same ID conflicts |
| Host close/restart | Detach sends no interrupt/decline, preserves busy/pending, reconnects original native ID with a new Host record |
| Deletion/cross-session/auth | 404/auth failure, foreign pending/replay refused, owner-only discovery/attach, no passwords returned |
| Endpoint safety | Public/DNS/credential/path/query/privileged-port/redirect rejection; real Codex aliases resolved read-only |
| Existing behavior | Full SSH/RPC/terminal/task/approval/output/event/idempotency/recovery/install regressions; conditional native skips excluded from claims |

An early sandbox run could not listen locally; later runs used an environment permitting temporary listeners. Actual Codex testing exposed CLI-proxy/control-socket handshake mismatch, replaced with official WebSocket. Official socket aliases and empty-thread/no-rollout differences were fixed using read-only alias checks and a real local test turn. Documentation inventory/API checks were updated after an initial failure, then final regressions passed. None of those failures counted as success.

## Unverified and unsupported

- Real OpenCode v1/v2 installation; official-contract fixtures only. v1 full reads fail above 2000 messages or response budget.
- Claude empty-config native discovery and ACP initialization passed; nonempty states use fixtures. Real models/history/approvals remain pending. Official terminal attach is separate from unsupported Host structured takeover.
- Real Codex acceptance used fixed local responses, not external models, every security approval/tool or all multiwriter cases.
- Native Windows/Linux, minimum Go and other architectures. Windows ownership/control-socket security and native tests are implemented but only cross-compiled/static-checked.
- Phones/tablets/background/network interruption/real SSH/login/reboot/long load. No machine reboot was performed.

No absolute zero-bug claim. Architecture/API/capability details are in [implementation](external-agent-sessions.md).
