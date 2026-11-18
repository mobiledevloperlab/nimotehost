# Host client API

NimoteHost is primarily the backend for NimoteCode's Host workspace. It also runs
without NimoteCode and accepts third-party clients through the same interfaces.
This document describes that boundary; the [endpoint reference](completed.md)
contains the implemented routes and domain contracts.

## Choose a transport

| Interface | Intended use | Authentication |
| --- | --- | --- |
| `nimotehost rpc` | One JSON request on stdin; successful JSON on stdout, errors on stderr with nonzero exit | Bridge reads the credential on the host |
| `nimotehost rpc-stream` | JSONL requests multiplexed on one SSH exec channel without a PTY | Bridge reads the credential on the host |
| Loopback HTTP `/v1/*` | Same-machine clients that manage the local owner/device credential | Bearer token plus `Nimote-Host-Protocol: 1` |
| `nimotehost mcp` | Compatible MCP tools, read-only by default | Bridge reads the credential on the host |

Configure and authenticate SSH separately. Do not expose the loopback HTTP API
through a public proxy. An ordinary SSH login has that OS account's authority;
scoped devices require the documented forced command and independent credentials.
See [Security](../SECURITY.md). MCP writes require `NIMOTEHOST_MCP_WRITE=1` and
MCP exposes only its declared tools, not every HTTP endpoint.

`rpc-encoded` is a legacy one-shot fallback. Base64 is not encryption; request
contents in argv can appear in process listings. Use stdin/stream transport for
secrets. Host commands must not emit banners or logs to protocol stdout.

## Negotiate before calling optional features

1. Call `GET /v1/capabilities`. Accept protocol 1, minimum protocol 1, a nonempty
   `host_id` and a `features` object. Release versions are not protocol versions.
2. Enable optional operations and request fields only when the documented known
   feature is true. Missing features are false; ignore unfamiliar response fields.
3. When `rpc_stream` is true, an SSH client can open `nimotehost rpc-stream`.
   Its first line is `{"stream_protocol":1}`. Stream protocol, Host protocol and
   plugin HCP versions are independent.
4. Recheck capabilities and Host identity after reconnecting. Do not assume a
   replacement server has the same features or mutation receipts.

The request body is JSON, not a JSON-encoded string:

```json
{"id":1,"method":"GET","path":"/v1/sessions"}
```

A stream success is `{"id":1,"result":...}`. Request IDs are positive integers,
unique among in-flight requests. Responses can arrive out of order; correlate
by ID. Limit concurrency to eight. Request JSON is limited to 1 MiB; keep the
complete response line within 2 MiB excluding newline. Strict domain request
handlers reject unknown fields. See the [compatibility table](host-hardening.md#client-compatibility).

## Structured errors and old clients

HTTP errors retain their existing `{"code":"permission_denied","error":"..."}`
body and status. New bridges advertise `rpc_error_details_v1` and add the following
field to stream errors, without changing the legacy `error` string:

```json
{"id":1,"error":"Host RPC 403: ...","error_detail":{"status":403,"code":"permission_denied","message":"device permission denied"}}
```

Prefer `error_detail.status` and `error_detail.code` for decisions. Treat `message`
as diagnostic text. Ignore unknown fields/codes. When details are absent, retain
legacy handling; do not assume an old host supports structured errors. One-shot
`rpc` retains its stdout/stderr contract; structured bridge errors are available
through `rpc-stream` and the Go client below.

| HTTP status | Stable code |
| --- | --- |
| 400 | `invalid_request` |
| 401 | `unauthenticated` |
| 403 | `permission_denied` |
| 404 | `not_found` |
| 405 | `method_not_allowed` |
| 409 | `conflict` |
| 413 | `request_too_large` |
| 415 | `unsupported_media_type` |
| 426 | `incompatible_protocol` |
| 429 | `resource_exhausted` |
| 500 | `internal` |
| 503 | `unavailable` |

Bridge failures have no HTTP status (zero in the SDK). Codes include
`host_stopped` (connection refused, independent of the configured port),
`transport_error`, `deadline_exceeded`, `cancelled` and `invalid_response`.
A missing HTTP status never proves a mutation did not run. An application rejection
need not close a healthy stream; malformed framing or transport failure does.

## Go client and runnable example

Import `github.com/mobiledevloperlab/nimotehost/pkg/hostclient`. It uses only the
standard library and does not import Host internals or desktop dependencies.
It provides request/result JSON encoding, capability checks, typed errors and an
optional command transport. It is a small client foundation, not typed wrappers
for every domain endpoint. `pkg/pluginsdk` serves a separate plugin purpose.

From this source checkout, with an already running Host:

```sh
# Local Host; nimotehost must be on PATH.
go run ./examples/host-client
# Remote Host; use an existing trusted SSH configuration.
go run ./examples/host-client -ssh user@host
```

The example probes capabilities and lists sessions. Output can include private
session metadata; do not publish it. It does not install services or start tasks.

```go
client := hostclient.Client{
    Transport: hostclient.CommandTransport("nimotehost", "rpc-stream"),
}
caps, err := client.Capabilities(ctx)
// Handle err before using caps; test caps.Supports("feature") for optional calls.
var sessions json.RawMessage
err = client.Do(ctx, "GET", "/v1/sessions", nil, &sessions)
// errors.As(err, &rejection) obtains *hostclient.Error with Status and Code.
```

The command transport requires a Host that implements `rpc-stream` protocol 1.
For older Hosts without that bridge, provide a custom one-shot transport.
The command transport starts a bridge per call; it favors a simple integration
over continuous streaming. Its default upper deadline is 90 seconds, shortened
by caller context. It bounds response frames and sends request data through stdin.
It does not invoke a local shell, configure SSH, start Host, or retry requests.
The SSH command/destination and remote executable remain trusted configuration.
Provide a custom `hostclient.Transport` to reuse a multiplexed SSH channel.

## Mutation and lifecycle rules

A timeout or disconnection may happen after a write executed. Do not automatically
replay it. If the operation advertises idempotency, retain the same request key,
body and Host identity for an explicitly chosen retry; never generate a new key
for an uncertain operation. `mutation_expiry_v1` governs versioned keys and their
seven-day window. See [retention and compatibility](host-hardening.md#client-compatibility).

Output offsets are bytes, not characters. Honor resync signals rather than
assuming retained output begins at zero. Client disconnect detaches observation;
Host owns processes. Host/OS restart does not resurrect them. Authentication and
capability negotiation do not provide OS isolation or a multi-tenant sandbox.
