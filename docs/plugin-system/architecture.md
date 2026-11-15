# Plugin architecture — English / 简体中文

NimoteHost now has an additive Host-side capability platform. Protocol **HCP 1.0**
is independent of the existing Host transport protocol **1**. A generic client
implements discovery, JSON Schema forms/results, confirmation, invocation and
cursor polling once; new standard plugins require no provider-specific client or
Host changes. Existing clients must first implement this generic protocol: an
unmodified client that never supported capability discovery cannot magically gain
new UI. The normative public files are in [spec/v1](spec/v1/README.md), distributed
with this repository, without importing internal Go packages.

## Existing implementation review

| Existing component | Finding and reuse |
| --- | --- |
| SSH | Host does not implement another SSH server. OpenSSH exec `rpc`, `rpc-encoded`, `rpc-stream` bridge to authenticated loopback HTTP. Existing frame limits, multiplexing and admission apply. |
| Authentication / ACL | Bearer owner token and private per-device records are validated in `Server.ServeHTTP`; origin/protocol checks remain. New extensions are owner-only; observer/operator/controller grants do not authorize plugins. |
| Agent Adapter / ACP | CLI descriptors and `agentAdapter` already configure maintained Codex / ACP adapters. Manifest ACP descriptors extend lookup and reuse the official ACP SDK, Session Manager, approvals, prompts, output and Task references. No ACP implementation was duplicated. |
| MCP | Official MCP Go SDK already serves tools over stdio. Two generic HCP bridge tools extend its existing read/write gate. There is no second MCP stack or automatic remote MCP server importer. |
| Sessions / Tasks | Existing durable Session tasks and native Agent approvals remain authoritative. Finite plugin work has a separate bounded invocation table, not a replacement task scheduler. |
| Events | Existing session revisions/output replay remain unchanged. Plugin events use an independent bounded epoch/cursor journal to avoid mixing incompatible retention semantics. |
| Docker / GitHub | Existing task/workflow integrations remain unchanged. No equivalent general public HTTP inspector was found in reviewed Host sources. |
| Platforms | Existing Unix guardian/process groups and Windows Job backend supervise plugin processes. Configured native quotas are passed through. No Go `plugin` package is used. |

## Boundaries and execution

- `pkg/pluginsdk`: public wire types and stdio server; standard library only.
- `internal/plugins`: concurrent registry, schema validation, installation,
  persisted enable/grant records, invocation receipts/events and process lifecycle.
- `internal/host/plugins.go`: existing transport/ACL, process supervisor and ACP
  adaptation. It contains no HTTP Inspector logic.
- `examples/system-monitor`: read-only system snapshots, complex declarative UI and contract tests.
- `tools/pluginpack`: reproducible platform-specific bundle creation.

Each enabled plugin owns a serial work queue and independent supervisor worker.
Different plugins execute concurrently. Initialization negotiates HCP 1.0;
health checks run every five seconds, with a two-second deadline. Startup allows
three seconds after process creation. Failure causes at most three launch attempts
per enable cycle, with one/two-second backoff. No invocation is retried. Explicit
recovery from exhausted attempts requires disable/enable. Invalid installed
packages are rejected without stopping core Host startup.

Limits: 16 installed plugins, 64 capabilities per plugin, eight queued requests
per plugin, 32 retained/active invocations globally, 30 seconds per invocation,
512 KiB wire frames, 60 KiB manifest, 64 MiB executable, 256 metadata events,
and 4,096 retry receipt keys per daemon epoch. Completed results can be retired;
active work is never evicted. Caller data is deep-copied across registry boundaries.

Package records persist; results/events/receipts do not survive Host restart.
Persisted enabled packages restart after integrity/grant validation. An invalid
package is not executed or deleted automatically. Updates require disable and
clear all prior grants. A crash during a two-directory update may leave a private
`.previous-*` directory for manual recovery; unknown staging/backup directories
are ignored. This is fail-closed installation, not a transactional package database.

简体中文：复用现有 SSH、认证、进程监管、ACP 和 MCP；新增能力通过统一协议发现。
独立进程与原有会话管理并存，不替换现有任务系统。标准插件可自动接入已支持 HCP
的通用客户端；未实现 HCP 的旧客户端继续使用原接口。

## Desktop GUI

The native desktop now includes a generic Plugins page: review/install local bundles,
show manifests and grants, enable/disable/update/uninstall, discover capabilities,
edit JSON input with required-field defaults from the input schema, confirm sensitive
work, and monitor/cancel invocations with result/progress polling. It uses the public
HCP contract through the existing authenticated desktop facade; no plugin-specific
GUI logic is added. Settings retains its original navigation index. All six existing
languages include plugin controls. Complex schemas use a JSON editor rather than
a complete visual JSON Schema form renderer.

简体中文：新增通用插件页面、清单审查、权限确认、能力调用及结果/进度/取消入口；
界面继续使用现有六种语言，复杂输入采用通用 JSON 编辑器。
