# Plugin UI Contract 1.0

## English summary / 简体中文正文见下文

The authoritative definition is pkg/pluginsdk/plugin_ui.schema.json. HCP 1.0 discovery carries optional ui.contract metadata with an exact 1.0 version, initial state, declared same-plugin actions and a precompiled component tree. JSON Pointer bindings connect state, tables and details. Unsupported contracts fall back to legacy forms; unknown components do not execute their subtrees. There are no new execution endpoints or automatic invokes.

The existing Executor retains schema checks, approval, signature and epoch verification, cancellation and result recovery. Reconnects never replay accepted work. UI metadata, JSON, component trees, forms and state have explicit budgets detailed below. Tables use bounded snapshots and client-side pagination. Web/native containers are not registered in UI 1.0. Schema changes must originate in the Host and synchronize immutable hashes and shared fixtures with the client.

## 简体中文

规范所有者：**NimoteHost/pkg/pluginsdk/plugin_ui.schema.json**（JSON Schema 2020-12）。本文件说明其语义，不是第二套独立定义。NimoteCode 的 test/host/fixtures/plugin-ui-1.0 是固定字节的测试镜像，lock.json 固定 SHA256 和 Host 基线；禁止在客户端独立修改。UI 与 HCP 的版本互不替代。

## 发现与兼容

继续 POST /v1/extensions，protocol:"1.0"，capabilities.list/get 返回既有 capability 描述。ui.layout、ui.result 保持提示语义，新增可选 ui.contract。contract.version 必须精确匹配 "1.0"；Host SDK 在 Manifest 校验时拒绝不支持的版本。旧 Host/客户端仍可发布/读取 ui 对象；旧客户端显示不兼容提示并使用原有输入 Schema，不加载可执行 UI。新客户端遇到缺失/非法/不支持 contract 亦回退原表单。未知组件显示不兼容占位且不遍历执行子树，仍校验其结构与 Action 引用。

没有新执行 endpoint 或第二条授权路径，也没有隐式 onLoad/定时 invoke。新功能通过可选字段或新 Contract 版本发布；破坏性变更必须新版本和相应固定制品、正反契约语料。回滚为删去 ui.contract，保留 input_schema/output_schema 和 legacy UI 提示；更新已安装插件仍遵守禁用、摘要审阅及重新授权。

## 字段与语义

| 对象 / 字段 | 含义 |
| --- | --- |
| contract.version | 固定 "1.0" |
| contract.state | 页面初始 JSON 对象，最多 64 个命名槽；只存内存 |
| contract.actions | 最多 32 个命名动作；目标必须属于同一 Manifest 且已声明 |
| contract.root | 组件树根节点 |
| action.capability | 完整 plugin/name，不接受事件提供的任意 ID |
| action.result | 成功结果写入 state 的槽名；不改写失败/取消的旧数据 |
| action.input | 参数名 → {pointer: RFC 6901 JSON Pointer}；button 从 {state: 页面状态} 读取；不存在的路径为 null，仍须通过目标输入 Schema |
| node.type | 预编译组件名；未知名称安全占位 |
| node.id | 可选逻辑标识，当前 Widget 身份按树路径管理 |
| node.label / text | 有界纯文本，不解析 HTML/Markdown/脚本 |
| node.source | 从 {state: 页面状态} 读取的 JSON Pointer |
| node.visible | Pointer 值严格等于 true 时显示，不支持表达式 |
| node.action | form/button 的必填动作名 |
| node.selection | table 的可选 state 槽；选择一行写入完整不可变行对象 |
| node.columns | table 的 1..12 个行对象属性名 |
| node.children | 最多 64 个子节点 |

状态、动作、selection 和 result 槽名均为 ^[a-zA-Z][a-zA-Z0-9_-]{0,63}$。组件属性禁止未知字段，未来控件名允许未知以支持回退。节点不携带权限、可执行表达式、网络 URL 数据源、Flutter 对象或 OS 句柄。

支持 column（纵排）、responsive（宽度至少 600 且文字缩放不超过 1.5 时双列，否则纵排）、group（折叠）、tabs（保留已挂载子页状态）、text、detail（有界可选择 JSON 文本）、form、button、table。form 使用 action 目标的 input_schema，用户提交的完整值替代 action.input，不与隐藏默认参数合并。表单输入只存在控件状态，不自动写入共享 state。可选字段动态启用；嵌套对象/数组使用有预算的预编译编辑器；开放字典、深层和复杂组合保留 JSON 编辑及完整 Schema 校验。

表格最多 1000 行、12 列，每页 20 行；只渲染当前页。搜索跨声明列，数字按数值排序，其余按字符串排序；筛选/排序重置页码。选择行可绑定详情。当前分页是客户端分页，Host 数据源通过普通 capability 返回有界快照；协议不虚构远程 total/cursor 语义。

## 交互示例

```json
{
  "version":"1.0",
  "state":{"snapshot":null,"selected":null},
  "actions":{"sample":{"capability":"system-monitor/snapshot","result":"snapshot"}},
  "root":{"type":"responsive","children":[
    {"type":"button","label":"采集本机快照","action":"sample"},
    {"type":"table","source":"/state/snapshot/processes","columns":["pid","name","cpu"],"selection":"selected"},
    {"type":"detail","source":"/state/selected"}
  ]}
}
```

完整独立示例：`examples/system-monitor/manifest.template.json`。系统监测工作台包含六个标签页、嵌套采样表单、折叠详情、响应式表格和行选择联动。本机模式读取只读快照，模拟模式提供 500 行进程和 60 条趋势样本。须使用 pluginpack 写入目标平台及真实摘要后安装。

## 执行、事件与安全

Runtime 在 dispatch 时验证声明、scope epoch 和 capability 签名；Executor 验证输入输出 Schema、当前状态、并发、用户审批、审批后的 get 签名和 epoch。Host 再检查已有鉴权、grants 与敏感确认。按钮不可调用未声明、其他插件或 ACP Session；ACP 保留原生会话入口。未授权也不能通过 UI 状态提升权限。

Host 现有 subscribe 是游标轮询。初次 epoch 空获取快照；失效游标重新发现；进度 0..1、结果终态和取消继续由 Executor/任务列表展示。客户端不把任意插件事件当 UI Action、不引入事件触发调用或可执行订阅脚本。断线变 unknown，只查询结果而不重放；Host 重启新 epoch 不自动再次 invoke。页面退出清理监听与 UI 状态，未完成审批返回 false；已受理任务仍归 workspace Executor 管理，取消/恢复由现有任务 UI 提供。

限制：UI 元数据 32 KiB；通用 JSON 4096 节点/24 深度；组件树 128 节点/12 深度；每次 state 写入复用 512 KiB/16384 值节点/32 深度限制并限制 64 个槽。表单嵌套可视编辑最多 128 个控件预算、6 层、32 个数组项，超限回退 JSON。原始 HCP transport 仍最多 2 MiB 响应，SDK 帧 512 KiB；Schema 4096 节点/32 深度且插件引用均拒绝。规范自身的本地递归 $ref 是受信任编译资产，不解析来自插件的远程引用。Host 既有每调用最多 100 个进度帧及有限日志控制事件风暴；无 UI 自发调用循环。

错误：ui_invalid（结构/属性/Action 引用）、ui_version（客户端版本）、ui_binding（客户端 Pointer）、ui_forbidden（越命名空间/未声明）、ui_limit（预算）。Host SDK 将 UI 版本/Pointer 格式归为 ui_invalid；安装接口仍使用既有 HCP 错误封装，不新增传输错误码。执行错误继续使用 HostCapabilityError，不把 raw child 错误回传或自动重试。

Web UI 与 Native Views 不在 UI 1.0 注册集合内；未知 web/native 节点只占位。评估与未来隔离要求见 plugin-ui-architecture.md。

## 版本同步与契约测试

修改顺序：Host 更新规范、SDK、共同正反样例、示例模板和测试；以新版本/新摘要发布不可变制品。客户端从选定 Host revision 获取字节，同步 lock.json 并运行两端相同语料。不能只修改 lock 来掩盖解析差异。

客户端：`python3 tool/check_plugin_ui_contract.py --host ../nimotehost` 比较三份制品的完整字节与固定摘要；常规 Flutter 测试亦验证本地 pins，CI 不依赖相邻仓库。Go 测试验证同一语料并覆盖 legacy/深度/超大输入。当前新增规范尚未外部发布，lock 的 base_commit 记录实施前基线，具体内容以 SHA256 为准。仓库分别提交/发布，不建立共享源码路径依赖。
