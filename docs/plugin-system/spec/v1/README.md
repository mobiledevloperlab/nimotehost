# Nimote Host Capability Protocol 1.0

Public, versioned specification, Apache-2.0.

This directory plus [protocol.md](../../protocol.md) defines HCP **1.0** without a
Go dependency. Freeze this version's field meanings when publishing releases.
Additive future fields require tolerant response readers; unsupported request
fields are rejected by this Host. A breaking change requires a new directory,
protocol value, client negotiation and separately maintained compatibility tests.
Host transport remains protocol 1; no legacy endpoint changes semantics.

- [Request schema](request.schema.json): logical operations over authenticated
  `POST /v1/extensions` inside the existing SSH RPC envelope.
- [Response schema](response.schema.json): successful envelopes and errors.
- [Catalog schema](catalog.schema.json): metadata-only GitHub listings, optional `plugin_catalog_v1` extension.
- [Manifest schema](manifest.schema.json): discovery metadata and executable policy.
- [Plugin wire schema](plugin-wire.schema.json): private stdin/stdout transport;
  language-independent, so plugins need not be written in Go.

## Stdio plugin wire

UTF-8 NDJSON, one object per line, at most 512 KiB per frame. Host sends:

```json
{"id":"initialize","method":"initialize","protocol":"1.0"}
```

Plugin must answer before any invocation:

```json
{"id":"initialize","protocol":"1.0","result":{}}
```

Host then sends `{"id":"ping","method":"ping"}` for health checking, or
`{"id":"opaque-id","method":"invoke","capability":"local-name","input":{}}`.
Plugin returns exactly the same string `id` with either `result` matching the
manifest output schema or `error:{code,message}`. An invocation can first emit
up to 100 progress-only frames `{"id":"opaque-id","progress":0.5}`.
All other output is a protocol error. SDK stdout is reserved for protocol frames;
raw stderr is discarded by Host. No Host token or automatic credential environment
is provided. Correlation is per process and only one operation runs at a time.

Cancellation/timeout closes the process transport and terminates the supervised
tree. Plugins must tolerate termination at any point. There is no rollback promise
and no automatic invocation replay. The Host can restart the process for later
requests. An application-level cancel message is unnecessary in this serial v1
transport; `capabilities.cancel` is the public cancellation operation.

Capability identifiers are `manifest.id/capability.name`, each local component
matching `[a-z][a-z0-9-]{0,62}`. Plugin/capability versions use `MAJOR.MINOR.PATCH`.
Protocol version matching is exact for 1.0: unsupported peers fail negotiation.
Input/output schemas are JSON Schema objects resolved locally; `$ref` and `$dynamicRef` are prohibited (inline schemas only; depth <=32 and <=4,096 schema nodes). Implementations must bound schema/package size
and avoid rendering untrusted UI descriptors as executable content.

A generic client renders forms from `input_schema`, escapes and presents
`output_schema` results, displays title/description and optional UI hints, and
requires visible confirmation before setting `confirmed:true` for a sensitive
operation or granting package permissions. `ui.layout` and `ui.result` are advisory
strings, never scripts, HTML or native command names. Unknown kinds or unsupported
UI hints must fall back to JSON display or safe refusal.

`agent` with `acp_version` 1/2 opts into existing ACP session semantics; all other
agent/tool/task/event capabilities use finite invoke semantics. An event plugin
can return a cursor/snapshot batch defined by its output schema. Host registry
and progress subscriptions are the independent standard HCP event journal.
