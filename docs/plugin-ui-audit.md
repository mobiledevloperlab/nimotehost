# 插件 UI 审计与决策（2026-11-14）

## English summary / 简体中文正文见下文

The audit preserves Host installation, grants, process supervision, bounded events and authenticated HCP RPC, together with the client's discovery, approval, signature verification and task recovery. The separate local panel system is not merged. UI Contract 1.0 adds bounded declarative components and page state while retaining legacy forms as fallback; it introduces no execution bypass.

Same-user plugin processes retain account privileges, hashes are not publisher signatures, and client schema support is a limited subset. UI actions cannot cross plugin namespaces, invoke implicitly or replay on reconnect. WebView isolation and MCP Apps hosting remain unimplemented. Publish the Host-owned schema and shared fixtures before synchronizing client pins and testing lifecycle and hostile inputs.

## 简体中文

审阅范围：NimoteCode lib/host/host_capability_{models,client,schema,executor}.dart、host_plugin_workspace.dart，lib/extensions/{host_plugin_view,host_capability_form,plugin_runtime,capability_gateway,plugin_manifest,plugin_table_data}.dart，相关 test/host、test/extensions、UI 目录；NimoteHost pkg/pluginsdk、internal/plugins/{runtime,schema,catalog}.go、internal/host/plugins.go、协议规范、HTTP Inspector 示例与测试。两个 Host 路径 realpath 相同，基线 a6f28bde0973c2050277b432c6a19d1f7779fcf2。实施前两个仓库工作区干净。

## 已实现 / 保留

Host 默认禁用、固定入口、摘要校验、确切 grants、进程监督、串行队列、超时、取消、重启恢复和有限事件日志。HCP 1.0 独立于传输版本 1，通过鉴权 SSH RPC，提供 list/get/invoke/result/cancel/subscribe；epoch 和 request_id 防重放。SDK 发布输入输出 JSON Schema、可选 ui RawMessage。

客户端 HostCapabilityClient 管理发现、协议检查、事件游标和范围失效；HostCapabilityExecutor 负责确认、审批后重新读取签名、输入输出校验、并发容量、结果恢复和未知结果处置。HostPluginWorkspace 按 SSH 身份隔离资源，轮询随应用生命周期停止。HostPluginView 已接入插件导航和任务结果。以上保留，禁止新建执行旁路。

另有本地自定义插件面板系统：CapabilityGateway 按受信任 panel 身份检查权限、PluginStorage 持久化、PanelHost/ExtensionRegistry 贡献、read pool、workflow 和 background jobs。其标识、权限和结果类型与 HCP 不同；此次不合并或迁移。

## 核心限制 / 局部扩展

HCP ui.layout/ui.result 只是提示，现有表单只对顶层标量生成原生控件，嵌套对象/列表使用 JSON 输入。结果表格只是 100 行 / 12 列的预览，无绑定/联动。新增独立版本的 ui.contract，保持原始表单作为缺省与不兼容回退。增量引入有预算的组件树、页面内存状态、JSON Pointer 绑定及声明式动作；复用共享 Nimote 控件。无重写理由。

## 安全债务

Host process.same-user 是风险披露而非 OS 沙箱，恶意插件仍有账户权限；摘要不是发行者签名。客户端 Schema 是明确受限子集，引用禁止，不能声称完整 JSON Schema 支持。UI 必须无表达式、无远程解析、无隐式 invoke，动作不能跨插件；未知组件整个子树降级。敏感输入不持久化，退出不重放已受理操作。

Browser WebView 具备导航/隧道/生命周期而没有插件资源隔离和受限 RPC 桥，不能直接复用为安全插件容器。MCP 已有 Host query/invoke 接入，但不是 MCP Apps 容器。终端和编辑器已有高性能实现，保持客户端所有权，不下载 Dart/原生库。

## 决策顺序

先发布 Host 单一规范和示例，再用固定摘要同步客户端契约；实现 Schema Runtime 和导航接入、恶意输入及生命周期测试。Web/MCP Apps 仅评估，待可验证的隔离容器再启用。旧协议无需迁移；移除 ui.contract 即回退旧表单，执行和 grants 保持原样。后续丰富嵌套表单和远程分页应通过可选版本能力扩展，不把未实现功能宣称完成。
