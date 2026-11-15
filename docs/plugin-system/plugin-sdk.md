# Plugin SDK — English / 简体中文

`github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk` is the public Go SDK.
External plugins import it without depending on Host internals. Other languages
can implement the [NDJSON contract](spec/v1/README.md).

Use `pluginsdk.Serve(ctx, stdin, stdout, handler)` and declare capabilities,
schemas, permissions and version in a Manifest. Stdout is reserved for the wire
protocol. Progress values are between 0 and 1. Host serializes calls within each
plugin and terminates the process on cancellation; handlers should also observe
context cancellation. Errors returned to Host are sanitized.

The runnable example is [System Monitor](../../examples/system-monitor/README.md).
It exercises UI Contract 1.0 with read-only system snapshots and simulated data.
The previous Echo and HTTP Inspector demos have been replaced by this example.

Build and package on the target platform:

```sh
go build -o /tmp/system-monitor ./examples/system-monitor
go run ./tools/pluginpack -binary /tmp/system-monitor \
  -manifest examples/system-monitor/manifest.template.json \
  -out /tmp/system-monitor-bundle
```

The output directory must be new. Windows uses an absolute temporary path and
`.exe` for the build output. The packager pins the executable hash, platform,
and entrypoint, then prints the Manifest SHA256. Cross-built binaries must be
packaged on their target platform. Templates containing zero hashes are not
installation bundles.

After reviewing the executable and Manifest, use the existing authenticated RPC:

```json
{"method":"POST","path":"/v1/extensions","body":{"protocol":"1.0","operation":"plugins.install","source":"/absolute/system-monitor-bundle","sha256":"MANIFEST_SHA256","confirmed":true}}
{"method":"POST","path":"/v1/extensions","body":{"protocol":"1.0","operation":"plugins.enable","id":"system-monitor","grants":["process.same-user"],"confirmed":true}}
{"method":"POST","path":"/v1/extensions","body":{"protocol":"1.0","operation":"capabilities.list"}}
{"method":"POST","path":"/v1/extensions","body":{"protocol":"1.0","operation":"capabilities.subscribe"}}
```

Wait for ready, then invoke `system-monitor/snapshot` with input `{}`, the current
epoch, and a fresh random 64-hex `request_id`. Query `capabilities.result` with the
invocation ID; use `capabilities.cancel` to cancel. The UI uses the same execution
and authorization path. No automatic calls or timer refresh are defined by
[UI Contract 1.0](../plugin-ui-protocol.md).

For distribution, build and verify each supported platform and follow the
[metadata-only catalog workflow](catalog.md). The local demo does not publish a
catalog entry or change the running Host during packaging.

简体中文：复制系统监测示例，通过公开 SDK 实现能力。打包后可在 GUI 手动选择
`manifest.json` 安装并启用。插件以当前用户权限运行，需授予 `process.same-user`；
系统监测示例不需要网络访问授权，不执行写入系统配置或结束进程的操作。
