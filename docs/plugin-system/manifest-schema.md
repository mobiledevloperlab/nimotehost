# Manifest Schema

The normative [JSON Schema](spec/v1/manifest.schema.json) is HCP 1.0. Build a
platform-specific bundle using [the SDK guide](plugin-sdk.md). A bundle contains
`manifest.json` and a regular executable named `plugin` or `plugin.exe`.

Required metadata: protocol, namespace `id`, semantic version, publisher, source,
entrypoint, executable SHA256, supported `GOOS/GOARCH` platforms, permissions and
1..64 capability descriptors. Each descriptor includes name, kind, version, title,
input/output JSON Schemas; description, UI object, sensitivity and ACP version are
optional. Capability names must be unique within the plugin. Runtime validation
also checks the actual platform, compiled schemas, all schema references, duplicate
permissions, source file types and both reviewed-manifest/executable digests.
Unknown manifest fields are rejected.

`process.same-user` is mandatory because this release executes with Host's OS
account. `network.public` documents intended networking and is required by the
Inspector example. These are disclosed privileges/owner grants, **not** network
or filesystem sandbox rules. See [security](security.md). Undeclared permission
names fail closed; there is no wildcard permission or credential inheritance.

The local installation operation accepts a bundle directory and an independently reviewed
SHA256 of its exact manifest bytes. The binary is copied and hashed again before
publication. Publisher/source are provenance claims, not cryptographic identity.
The catalog supports explicit GitHub Release download/install using a separate
[metadata index schema](spec/v1/catalog.schema.json). There is no signing authority
or automatic update. Developers submit metadata only; executable code stays in
their own repository.
