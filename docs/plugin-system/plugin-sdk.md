# Plugin SDK — English / 简体中文

`github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk` is an independently
importable public Go package with no internal Host imports or third-party
requirements of its own. An external repository can `go mod init`, then
`go get github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk@<published-host-tag>`.
Use a local Go module `replace` during development before this change is released.
A separate SDK module/release cadence is a future packaging option, not a
requirement to import Host internals. Other languages can implement the public
[NDJSON contract](spec/v1/README.md).

Copy `examples/plugin-template/main.go` and its Manifest template. Implement
`pluginsdk.Handler`, declare schemas, version and permissions, and use
`pluginsdk.Serve(ctx, stdin, stdout, handler)`. Do not print logs to stdout. The
handler can emit numeric progress. Host serializes work within one plugin;
independent plugins run concurrently. SDK suppresses handler error details.
Use context deadlines for your own network operations and expect process termination.

From the Host source checkout, build the HTTP example on the target platform:

```sh
go build -o /tmp/http-inspector ./examples/http-inspector
go run ./tools/pluginpack -binary /tmp/http-inspector \
  -manifest examples/http-inspector/manifest.template.json \
  -out /tmp/http-inspector-bundle
```

Windows: use an absolute local temporary path and `.exe` for `-o`/`-binary`.
`pluginpack` writes the correct executable name, platform and executable digest,
then prints `manifest_sha256=...`. Output directory must be new. It describes the
running packaging platform; cross-built binaries must be packaged on their target
platform or have their Manifest platform edited/reviewed before installation.
Nothing in this build step changes the running Host.

After reviewing the two files, send these JSON envelopes via SSH to `nimotehost rpc`
(or the equivalent existing local RPC command). Replace paths and digests yourself:

```json
{"method":"POST","path":"/v1/extensions","body":{"protocol":"1.0","operation":"plugins.install","source":"/absolute/http-inspector-bundle","sha256":"MANIFEST_SHA256","confirmed":true}}
{"method":"POST","path":"/v1/extensions","body":{"protocol":"1.0","operation":"plugins.enable","id":"http-inspector","grants":["process.same-user","network.public"],"confirmed":true}}
{"method":"POST","path":"/v1/extensions","body":{"protocol":"1.0","operation":"capabilities.list"}}
{"method":"POST","path":"/v1/extensions","body":{"protocol":"1.0","operation":"capabilities.subscribe"}}
```

Wait for ready. Use the returned epoch and a fresh cryptographically random
64-hex request ID to invoke:

```json
{"method":"POST","path":"/v1/extensions","body":{"protocol":"1.0","operation":"capabilities.invoke","id":"http-inspector/request","epoch":"CURRENT_EPOCH","request_id":"64_LOWERCASE_HEX_DIGITS","confirmed":true,"input":{"method":"GET","url":"https://example.com","timeout_ms":5000}}}
```

Poll `capabilities.result` with the returned invocation ID. Cancel with
`capabilities.cancel`; subscribe using epoch and cursor. Debug failed plugins by
running their binary manually against NDJSON fixtures in a disposable account.
Host records sanitized metadata, not arbitrary child stderr. Never debug with
production credentials. To publish, run tests, build one bundle per platform,
verify the Manifest against the public schema, distribute it and the Manifest
digest through a trusted channel, document privileges and tag your source.
There are no install scripts and no Host-specific provider switch to edit.

简体中文：示例、模板和打包工具均可直接使用。第三方仓库只依赖公开 SDK 或公开
NDJSON 协议。安装、启用、调用均走既有认证通道；发布前需要来源审查、安全测试及
按目标平台构建，Host 不自动下载或信任社区二进制。
