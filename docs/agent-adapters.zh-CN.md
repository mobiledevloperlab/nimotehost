# Agent 适配器开发

[English](agent-adapters.md) | **简体中文**

Host 以当前用户身份运行已安装的 Agent CLI。适配器属于 Host 模块，客户端通过文档化的
RPC 和能力字段接入。适配器不应依赖 Nimote 控制面、自动下载 CLI 或内置凭据。
新增集成前先通过 issue 讨论方案。

## 当前接入位置

| 范围 | 文件与职责 |
| --- | --- |
| 请求校验与命令解析 | `internal/host/manager.go`：Agent 名称白名单、工作目录、参数校验和生命周期集成 |
| 启动与回退 | `internal/host/agent_compatibility.go`：结构化握手、超时、取消和终端回退 |
| 结构化会话 | `internal/host/codex_agent.go`：共享状态和 Codex app-server；`internal/host/acp_agent.go`：OpenCode、Claude 的 ACP 处理 |
| 平台支持 | `internal/host/` 中平台专属的终端和结构化进程文件；保留构建约束和进程清理 |
| 能力发现 | `internal/host/protocol.go`：请求、会话字段、工具与能力标记 |
| RPC 操作 | `internal/host/server.go`：校验、认证边界和语义操作 |
| 原生历史与恢复 | 由支持该能力的提供方单独实现，不套用其他提供方的历史语义 |

当前实现采用显式提供方分支，没有公开的 Go 插件 API。添加适配器前搜索现有提供方名称，
按需更新 `resolveCommand`、`supportsStructuredAgent`、`configureStructuredCommand`、
`newStructuredAgent`，以及能力发现和协议文档。ACP 提供方只有在实际握手和事件语义一致
时才复用 ACP 实现。提供方特有解析和命令选择应与会话存储、服务安装分开；调整共享
生命周期前，先以具体用例提出接口重构方案。

## 必须保持的行为

- 使用明确 argv 直接启动已安装的程序，保留工作目录校验和认证，不通过 shell 拼接提示词。
- 区分 terminal、structured 和 auto 模式。在接受用户回合前探测就绪；探测失败后，必须
  清理原进程才能回退。已派发的提示词或写操作不能在另一适配器自动重放。
- 取消和关闭须终止自有进程及管道、唤醒等待读取并保存最终状态。慢握手和进程/网络 I/O
  不应持有 manager 锁。
- 能力标记描述已实现的支持；发现可执行文件与运行时握手成功是两个检查。统一审批、历史
  和恢复须实现完整语义并记录后才能声明。
- 保持输出上限、重放偏移和持久事件顺序。恢复失败不能静默新建会话。处理未知上游消息时
  不放宽请求校验，也不执行未经批准的操作。

## 验证与 PR 证据

参照 `acp_agent_unix_test.go`、`codex_agent_unix_test.go`、
`agent_start_deadline_unix_test.go` 和跨平台测试，使用假协议对端与临时可执行文件。
覆盖成功启动、程序缺失、不兼容/畸形协议、启动超时、探测期间取消、进程退出、并发输出
和清理；验证终端回退及提示词不重放。支持审批、历史/恢复或重连时增加相应语义覆盖。

执行贡献检查和受影响平台的原生 CI。真实 Agent 冒烟验证应记录准确 CLI/适配器版本，
区分假对端覆盖和真实认证/模型测试。样本不含 token 或私有输出。同步更新协议双语文档
`host.en.md`、`host.md`、能力说明及安装说明；新增依赖时更新第三方声明。
