# Plugin UI 架构决策

采用最小侵入扩展：HCP 1.0 的 capability.ui.contract 承载独立 UI 1.0；不存在 contract 时继续旧表单。业务 Schema 和 UI 树解耦。Host 不依赖 Flutter 组件；客户端只注册预编译组件。

数据流：Manifest → SDK 验证 → HCP 发现 → 有预算 Contract 解析 → Renderer → 动作 ID → Runtime → 现有 Executor → 原审批与签名复检 → HCP invoke → 事件/结果 → 页面状态 → Renderer。页面关闭释放监听和状态，远端已提交任务留在既有任务中心，不自动取消或重放。

Schema 层支持 column/responsive/group/tabs/text/form/table/detail/button。布局使用 Flutter primitives；控件通过 nimote_ui.dart。未知组件不渲染子树；错误 contract 回退旧表单。页面状态仅内存，按 capability 签名及连接身份重建，无插件共享或持久化权限。执行状态和任务结果仍归 workspace Executor；本地 table 支持搜索、排序、分页、选择和详情绑定。

Web 层暂不启用。官方 MCP Apps 使用 ui:// 资源、text/html;profile=mcp-app、JSON-RPC 双向消息和沙箱 iframe；官方 ext-apps AppBridge 可复用，但 Flutter WebView 不直接提供 iframe 同等的来源隔离、CSP 和桥验证。适配需固定资源摘要、禁止文件/任意导航/权限请求、来源与实例 nonce 绑定、消息大小与速率限制、只准已声明 capability 经 Executor、销毁时关闭消息桥。不能将 browser 页面作为插件容器。

参考（2026-11-14 检索）：[MCP Apps 规范](https://github.com/modelcontextprotocol/ext-apps/blob/main/specification/2026-01-26/apps.mdx)、[官方 SDK](https://apps.extensions.modelcontextprotocol.io/api/documents/overview.html)。这是采用候选标准的评估，未实现完整 MCP Apps。

商店边界：[Apple 2.5.2 / 4.7](https://developer.apple.com/app-store/review/guidelines/) 对下载代码和非内置软件设要求，4.7.2 对暴露 native API 设限制；[Google Play 政策](https://support.google.com/googleplay/android-developer/answer/18265346) 对解释执行代码的例外不免除其他政策责任。原生 Schema 无下载可执行客户端代码；远程 Web 插件仍需逐项审核资源、数据与分发流程，当前不声称获商店许可。

Native 层保留终端、编辑器、调试器已有实现；新 native view 必须由客户端注册和授权，UI 1.0 未开放任意 native 名称或直接 OS 调用。

实际模块：Host pkg/pluginsdk/plugin_ui.go 校验 UI 元数据；规范 JSON 随 SDK embed；Manifest.Validate 对所有声明动作做命名空间/存在性验证。NimoteCode extensions/plugin_ui/{contract,runtime,renderer,structured_input}.dart 分别负责解析、状态/分派、组件注册/布局和有预算的嵌套编辑。HostCapabilityForm 的 structured 开关缺省 false，仅新 Runtime 启用。HostPluginView 复用原审批和敏感输入脱敏；Runtime 直接使用既有 Executor，宿主只提供审批回调。
