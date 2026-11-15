# Manifest Schema — English / 简体中文

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

Installation accepts only a local bundle directory and an independently reviewed
SHA256 of its exact manifest bytes. The binary is copied and hashed again before
publication. Publisher/source are provenance claims, not cryptographic identity.
This release has no signature authority, plugin marketplace or automatic download.

简体中文：Manifest 与协议作为公开 JSON 文件版本化提供。安装前审查来源并固定
Manifest SHA256；启用时明确批准全部声明权限。更新必须先禁用，并重新批准权限。
