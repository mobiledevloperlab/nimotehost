# Plugin UI 实施记录

状态：两个可访问仓库已分别实施 Schema UI 1.0 增量升级；未发布或部署。先完成审计和架构决策再修改代码，未重写插件管理器、Executor、SSH 或原生核心组件。Web/MCP Apps 仍处评估阶段，不能将本次交付称为完整三层平台或真机验收。

## NimoteHost 的实际修改

- pkg/pluginsdk/plugin_ui.schema.json 为唯一机器可读规范；plugin_ui.go 复用已存在的 jsonschema-go，检查结构、命名空间、动作声明和资源预算。
- Manifest.Validate 对 UI 元数据做附加验证，沿用安装/启用/发现路径自动发布 ui.contract，不创建数据执行或事件 endpoint。旧 ui.layout/result 保持兼容，新的 32 KiB 预算只约束带 contract 的 UI。
- examples/http-inspector/manifest.ui.template.json 为可运行 Inspector 的可选复杂界面：表单、标签页、折叠、响应式表格和详情联动。原模板不变，不重新开发业务能力。
- SDK 正反契约语料、预算与兼容测试；SDK 文档说明新增的 Schema 校验依赖。

## NimoteCode 的实际修改

- extensions/plugin_ui/contract.dart：不可变解析、有预算树、RFC 6901 Pointer 和未知节点策略。
- runtime.dart：页面内存 State Store、声明式参数映射、同插件动作分派，直接复用 HostCapabilityExecutor。宿主只提供原有审批回调。审批结束前页面退出会拒绝派发，断线/新 epoch/签名变化不会重放。
- renderer.dart：预编译组件注册集合，复用 Nimote 共享控件；支持 column/responsive/group/tabs/text/detail/form/button/table。标签页保留挂载状态；表格含搜索、数值/文本排序、20 行分页、选择和详情绑定。父级重建不清空表单与运行时状态。
- structured_input.dart 与 HostCapabilityForm 的可选 structured 开关：嵌套对象、动态可选字段、可编辑数组，统一提交验证；超出可视预算或开放字典使用 JSON 输入。旧表单缺省 structured=false。
- HostPluginView 将新版界面接入既有插件中心/侧栏；不兼容版本回退旧表单；任务取消/恢复/错误/进度仍由原任务列表提供。
- SHA256 固定规范、正反语料和模板；check_plugin_ui_contract.py 可比较相邻 Host 仓库完整字节；常规 Flutter 测试自动校验固定摘要。
- host_plugins_acceptance.py 新增 --plugin-ui，真实 Host 集成测试覆盖发现 UI Contract 和 Runtime 审批/调用链。

## 测试与证据

2026-11-14 本地执行：

| 检查 | 结果 |
| --- | --- |
| flutter test test/extensions test/host test/ui/ui_component_governance_test.dart | 567 通过、16 按原有条件跳过；跳过项不算通过 |
| 最终针对新 Runtime、renderer、嵌套表单、原 Host 表单、UI governance 的回归 | 32 通过；另补充非法嵌套 JSON 测试独立执行 |
| dart run tool/check_ui_components.dart | 783 源文件，0 违规 |
| flutter analyze 本次所有修改的 Dart 实现及测试 | 无问题 |
| python3 tool/check_plugin_ui_contract.py --host ../nimotehost | pins 和 Host 字节一致 |
| go test ./pkg/pluginsdk ./internal/plugins ./internal/host | 通过（Host 完整包约 71 秒） |
| go test -race ./pkg/pluginsdk ./internal/plugins ./examples/http-inspector ./tools/pluginpack | 全部通过 |
| go vet ./pkg/pluginsdk ./internal/plugins ./internal/host | 通过 |
| Host 实际进程与 RPC 集成、Inspector/打包工具测试 | 通过 |
| python3 tool/host_plugins_acceptance.py --host-repo ../nimotehost --plugin-ui | 真实 Host + 插件进程通过；未启用 public-http |
| 两仓库 git diff --check | 通过 |

覆盖：契约正反语料、循环对象、深度和状态预算、非法 Action、跨插件动作、审批拒绝不派发、审批期间退出、异步结果、并发去重、进度、取消、未知结果恢复、epoch 失效、旧 UI/未知组件降级、实际页面审批、父级重建、嵌套输入验证/数组增删/禁用与键盘 Tab。手机 390×844、横屏 844×390、平板 1024×1366，Light/Dark，1×/2×文字，表格筛选/分页/排序/选择均有 Widget 测试。

真实集成在临时目录启动 Go Host fixture，经编码 RPC 安装真实打包 Inspector；不操作运行中的生产服务。检验 UI 发现、拒绝审批无调用、声明式参数映射、Host SSRF 拒绝、取消、断线/重启不重放、卸载，以及原任务/事件/ACP 会话。结果绑定成功路径由确定性模拟输出覆盖；未将外网服务可用性列入通过条件。该测试不等同于移动设备或真实 SSH 服务验收。

## 已知限制与后续任务

- MCP Apps/隔离 WebView 未启用；评估、官方来源及商店边界见架构文档。后续必须先建立资源来源限制、隔离桥、CSP、实例校验、权限和生命周期测试，不能直接接现有浏览器。
- 当前为有界快照与客户端分页，无通用远程分页/无限流、插件共享或持久 State；没有任意插件事件驱动的自动 Action。后续通过版本化可选扩展加入，不绕开 Executor。
- 嵌套可视表单支持固定 properties 和同构数组；开放字典、复杂组合/union 或超过控件预算仍使用 JSON。校验在提交时完成，未承诺完整 JSON Schema 的原生可视编辑或每键实时校验。
- Native view 任意注册未开放；原终端/编辑器/调试器保留原生入口。未知 native/web 节点安全占位。
- Host 同账户二进制不具 OS 沙箱，原有安全债务未被 UI 升级消除。未新增下载 Dart/原生代码路径。
- 未验证真机、IME/平台 WebView、iOS App Store 或 Google Play 审核；未做真实远程 SSH 网络抖动和全部 Local/AI/Terminal/IDE 端到端测试。相关既有扩展/Host 测试通过不能等价为全应用零回归。
- 两仓库尚未分别提交/发布，规范 pin 以内容摘要和实施前 Host 基线标识；正式发布时固定目标 release/revision，并同步锁与共同契约语料。

两仓库分别维护实现；四份根 docs 文档记录同一次审计与交付，但 JSON Schema 只在 Host 维护。回滚移除 ui.contract；旧表单、权限、执行与任务恢复路径继续可用。
