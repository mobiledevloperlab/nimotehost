# 外部 Agent 原会话发现与连续操作

English / 简体中文 · 更新：2026-11-17

Host 可安全实现的接入路径已落地；真实平台与手机验收仍有缺口。本文按具体传输说明能力，不将历史读取、原 ID 恢复和现有进程控制混为一谈。

## 审计与架构

| 范围 | 复用与增量 |
| --- | --- |
| Manager 与会话 | 继续使用原 `StartRequest`、`Session`、`resumeMu`、`resumeExisting` 和资源准入；没有第二套会话系统。 |
| Adapter | `externalSessionAdapter` 复用原 Codex/ACP 列表和历史实现，可选 `externalLiveSessionAdapter.AttachSession` 负责已有连接，不在 Manager 按 Provider 平行创建系统。`ExternalAttachment` 描述显式连接；凭据与可持久化的 `ExternalConnection` 分开。 |
| Codex | 原启动模式仍为 app-server stdio；新增通过 AF_UNIX socket WebSocket 连接已经运行的同一个 app-server。Windows 复用 Go 标准库与已有 `x/sys/windows`。SDK 继续处理 JSON-RPC 相关性、顺序与原生审批。 |
| OpenCode | 原 ACP 路径不变；支持 v1 HTTP 和 v2 `/api` HTTP/SSE，两种协议共用状态机、审批与事件投影。SSE 触发权威快照校准，定时快照补齐掉线期间的持久消息与待审批。 |
| Claude | 新增官方 `claude agents --json` 原生发现，不依赖 ACP；有界进程执行与取消直接复用 `runEvidenceProcessBounded`。既有 ACP v2 恢复不变，历史读取补上明确的 `replayFrom`，过滤回放结束和控制快照。 |
| Session Host | 两个外部连接句柄实现原 `processHandle` 生命周期，但不持有外部进程。`Kill`、`Close` 只关闭连接；只有显式 `AgentMessage.interrupt` 才请求原生中断。 |
| 审批与同步 | 复用原 `AgentMessage`、审批卡片、`AgentSnapshot`、输出字节游标、activity 序号、事件 revision 和持久请求回执。 |
| 持久化 | 原原子写入、请求预留和回执保持有效。Session 新增可选 `external` 连接身份，不保存密码；Host 重启后原记录 interrupted，客户端显式重新连接原 native ID。 |
| SSH 与权限 | 原 loopback HTTP、SSH RPC/流和设备 ACL 不变。外部发现、建立连接仅允许 owner；限会话设备仍需原角色授权。 |
| 自动化任务 | 接管不会自动执行额外的验证任务，也不修改原 Agent 的审批、sandbox、工作区或模型配置。 |

参考项目 [sessions.mjs](https://github.com/oxmumu3/ag-mobile-bridge/blob/main/lib/sessions.mjs) 和 [acp-hub.mjs](https://github.com/oxmumu3/ag-mobile-bridge/blob/main/lib/acp-hub.mjs) 的长期 worker、请求关联和重连思路；没有移植私有数据库猜测、历史注入式接力或公网隧道。

协议依据：[Codex app-server](https://github.com/openai/codex/tree/main/codex-rs/app-server)、[OpenCode v2 API](https://opencode.ai/docs/api)、[OpenCode v2 公开协议](https://github.com/anomalyco/opencode/blob/dev/packages/protocol/openapi.json)、[ACP 会话协议](https://github.com/agentclientprotocol/agent-client-protocol/blob/main/docs/protocol/v1/session-setup.mdx)、[Claude Remote Control](https://code.claude.com/docs/en/remote-control)。上游主分支文档不等于每个已安装版本都兼容。

v1 增量按官方 `v1.19.6` 的[会话接口](https://github.com/anomalyco/opencode/blob/v1.19.6/packages/opencode/src/server/routes/instance/httpapi/groups/session.ts)、[审批接口](https://github.com/anomalyco/opencode/blob/v1.19.6/packages/opencode/src/server/routes/instance/httpapi/groups/permission.ts)及[消息结构](https://github.com/anomalyco/opencode/blob/v1.19.6/packages/schema/src/v1/session.ts)实现。Windows 依据[微软 AF_UNIX 权限说明](https://devblogs.microsoft.com/commandline/af_unix-comes-to-windows/)读取既有文件安全描述符，并复用标准库连接，不自行实现 socket 协议。

通用能力直接复用现成实现：JSON-RPC 使用 `acp-go-sdk`，WebSocket 使用 `coder/websocket`，HTTP、Basic 认证、JSON、上下文超时和流读取使用 Go 标准库，Windows 权限检查使用已有 `x/sys/windows`。手写部分只负责原会话身份校验、授权、官方协议字段转换、能力投影和重连语义。SSE 只触发权威快照校准，不另建事件解码、重放或消息队列协议。OpenCode 官方 SDK 为 JavaScript/TypeScript；此 Go 实现复用标准库，不额外引入 Node 进程。

## 能力矩阵

| Agent/传输 | 发现与历史 | 原会话恢复 | 已运行会话控制 |
| --- | --- | --- | --- |
| Codex 原 stdio | 原 `thread/list`、`thread/items/list` | 原 `thread/resume`，校验原 ID | 不附着任意 stdio/PTY 进程 |
| Codex `codex-control` | 显式已有控制 socket 的原生列表与回放 | 先校验原 ID、工作区和 loaded 状态，再向同一服务发送只含 threadId 的 resume | 已实现原线程事件、消息、原生审批和中断；macOS 0.169.0 已做真实 CLI＋本地模型夹具验收；Windows AF_UNIX 与只读 ACL 检查已实现、测试交叉编译通过，Linux/Windows 未做原生运行 |
| Claude Code `claude-cli` | 官方 JSON 列表报告前台/后台会话、工作区、原生 ID、进程与等待状态；无需 ACP。只发现，不读取 transcript | 不通过本传输恢复 | 不执行控制命令。官方 `claude attach` 提供后台会话的终端界面，但不提供本传输需要的结构化审批 ID 和安全附着握手 |
| Claude Code / ACP | 安装 `claude-agent-acp` 后使用原 `session/list`；历史通过 `session/resume` 加 `replayFrom:{type:start}` 读取 | 原 ACP v2 `session/resume` | 不把新 ACP 进程的恢复当作已运行的独立 CLI 接管。真实 0.107.3 初始化/列表/新会话握手通过，实际历史、工具和模型矩阵仍待验收 |
| OpenCode / ACP | 原 ACP 列表和 load 回放 | 原 ACP v1 `session/load` | 不通过 ACP 声称附着另一个进程 |
| OpenCode `opencode-http-v1` | 已有服务的 `/session`、`/session/status`、原消息与工具调用 | 使用原 session ID，不创建或 fork 会话 | 已实现 `/prompt_async`、`/permission` 原生 pending、一次性批准/拒绝、显式 `/abort`、SSE 与快照恢复；协议夹具通过，真实版本未验收 |
| OpenCode `opencode-http-v2` | 显式已有服务的 `/api/session`、原生状态与分页消息 | 使用原 session ID，不创建或 fork 会话 | 已实现 prompt、原生 pending 读取、一次性批准/拒绝、显式中断、SSE 与快照恢复；目前为协议夹具验证，未做真实 OpenCode 验收 |
| Antigravity 等 | 尚无 Adapter | 未支持 | 未支持 |

能力状态为 `supported`、`conditional`、`unsupported`。安装与列表 ID 不证明实时控制成立；旧 `resumable` 字段保留兼容，恢复仍需成功握手。`live_transports` 列出可显式选择的连接路径；没有端点和原会话证据时不会将旧历史标为可控。

ACP 历史读取仍使用 provider 的 load/resume 回放，明确披露 `history_mode=native_load_replay`；它不是完全无副作用的第三方文件读取。新的现有服务读取为 `native_read`。Host 不直接写第三方数据库、配置或会话文件。

## API 与连接方式

保留协议 `1` 和既有请求语义。新增能力标志 `external_session_metadata_v1`、`external_session_attach_v1`；客户端必须探测能力后发送新参数。

`POST /v1/agent-history`：owner 提交 `agent`、可选工作区和游标，以及 `connection`。返回与原列表相同的 `items`、`next_cursor`。例如连接已运行的 OpenCode v2：

```json
{
  "agent": "opencode",
  "directory": "/absolute/workspace",
  "connection": {
    "transport": "opencode-http-v2",
    "endpoint": "http://127.0.0.1:4096"
  }
}
```

受保护的 OpenCode 服务通过请求体中的 `username`、`password` 使用原生 Basic 认证，默认用户名 `opencode`。不把凭据放进 URL、命令行、返回值或 Session。端点限定为显式端口的字面量 loopback IP，禁止 URL 凭据、路径、查询、重定向、代理与 DNS 解析。

Claude 原生发现同样复用 owner 专属的 `POST /v1/agent-history`：

```json
{
  "agent": "claude",
  "directory": "/absolute/workspace",
  "connection": {"transport": "claude-cli"}
}
```

`directory` 可省略。`claude-cli` 禁止提交 endpoint 和凭据；它不能用于 `StartRequest.attach`。能力标志 `claude_live_discovery_v1` 表示 CLI 在 PATH，`discovery_transports` 列出 `claude-cli`；不代表具体版本已支持，失败时返回明确错误，不启动替代服务。结果区分 `conversation_id`（可选原 UUID）与 `external_session_id`（可选后台短 ID）；没有原 UUID 时保持空值，不用 PID 冒充。`native_kind`、`native_pid`、`native_state`、`native_status`、`waiting_for` 保留官方状态；`status_source=claude_agents_json`。看到待审批只表示原生 UI 正在等待，不会生成可提交的审批句柄。没有存活进程时，不仅凭 `working` 就声称正在执行。

查询有 4 秒预算、2 MiB 响应和 2000 行上限，复用原进程树取消；每页 50 条，游标对象消失时要求刷新。重复身份、损坏 JSON 和不兼容结构明确失败，不回退读取私有会话文件。发现结果标明 `resumable=false`、`live_control=unsupported`；原 ACP 历史/恢复是另一条能力。该路径只返回当前快照，客户端可重新查询，不假装提供实时事件订阅。

`POST /v1/sessions`：在原创建接口增加可选 `attach`，必须同时给出原生 ID、原工作区、`interaction=auto` 或 `structured`。推荐 auto 等待完整握手：

```json
{
  "kind": "agent",
  "agent": "opencode",
  "interaction": "auto",
  "directory": "/absolute/workspace",
  "request_id": "client-generated-unique-id",
  "resume": {"conversation_id": "ses_original"},
  "attach": {
    "transport": "opencode-http-v2",
    "endpoint": "http://127.0.0.1:4096"
  }
}
```

OpenCode v1 使用同样的 JSON 请求，只把 `transport` 改为 `opencode-http-v1`，必须连接用户原来运行的服务。Host 不执行 `opencode serve`，因为另起服务不能证明接入原 TUI。每次 v1 请求都携带原工作区 `directory`；全局 pending 仅筛选原 session，消息、part 和工具调用保留原 ID，并验证 session 归属。v1 列表按原生 ID 分页，游标对象删除后要求刷新；上游列表变化期间不提供事务快照保证。v1 一次读取有界完整消息列表，超过 2000 条或响应预算明确失败，不将截断历史标成完整。

Codex 改用 `agent=codex`、`transport=codex-control`，endpoint 为已有控制 socket 的绝对路径。Linux/macOS 支持官方长路径 rendezvous 符号链接，只读校验别名目录和最终私有 socket 的所有权/权限；不改权限、不启动服务。Windows 使用标准库 AF_UNIX，只读查询 socket 与父目录的 owner/DACL，拒绝其他普通主体的写入及 socket 内容读取权限；系统和管理员属于受信任主体。校验父目录时持有禁止删除共享的只读目录句柄，握手结束后释放；拒绝父目录本身为 reparse point。不会调用会修改第三方 ACL 的 Host 文件准备函数。需要支持 AF_UNIX 的 Windows 版本及可校验的权限；原生运行尚未验收。socket 不存在、版本不兼容、线程未 loaded 或工作区不符时明确失败。

连接成功后沿用原 `/v1/sessions/{id}/agent-state`、`/output`、`/activity`、`/agent-message` 和 `/v1/events`。不新增任意 RPC 转发接口。

| 增量字段 | 语义 |
| --- | --- |
| `session_capabilities` | 区分发现、历史、恢复、实时控制；披露历史方式和可用传输。 |
| `origin` | 历史记录 `external_history`；显式已有服务记录 `external_live`；已匹配的 Host 连接为 `host_managed`。 |
| `execution_status` / `status_source` | 仅用实际协议证据；纯历史为 unknown，不用时间戳猜测。 |
| `host_session_id` | 原 Agent、ID、工作区，以及显式传输身份匹配的活连接；重连使用原 Host ID。 |
| `external_connection` | 发现结果的无凭据连接身份。 |
| Session `external` | 持久化的无凭据连接身份；旧客户端继续使用原字段。旧版本严格读取 Session 元数据，降级前应保留备份并使用兼容版本恢复，不能假定旧二进制理解新字段。 |
| 聚合 `partial` | 临时失败保留原 Provider 游标，已完成 Provider 不重复读取；持续故障应退避重试。 |

原 GET 历史接口不变，省略 agent 仍优先 Codex；`agent=all` 聚合三类历史。相同时间戳按 Agent/ID 确定排序，最大聚合游标可往返。

## 状态、审批与重连保证

- 并发附着沿用原 `resumeMu` 和原 ID 复用。不同端点、工作区、任务、策略或执行身份发生冲突时拒绝，不平行创建控制会话。
- 只用 provider 真正待处理的审批 ID；OpenCode 仅发送 `once` 或 `reject`，不发送 `always`。重复消费和相同请求 ID 的不同内容被拒绝；不确定的网络写入结果保留 uncertain，不自动重发。
- 外部连接始终采用 Host 的人工确认策略，控制选项只暴露 confirm；建立连接后也不允许切换为 smart/auto，避免更改原审批语义或造成重连策略冲突。原 Host 自启会话的既有策略保持兼容。
- Codex 保留 native 请求 ID 以处理桌面 `serverRequest/resolved`；先到的已解决通知不会重建旧审批。未知原生提问交由桌面处理，不用 MethodNotFound 自动失败原任务；容量用尽时只分离连接。
- OpenCode 每次控制前重新校准原生状态和 pending。SSE 合并唤醒，快照按顺序投影；消息内容哈希避免原样重复事件。没有假设上游 SSE 支持持久重放。
- 快照、单次 HTTP 请求分别有 4 秒预算；每个响应最多 2 MiB、一次消息快照最多 2000 条及 4 MiB，pending 最多 64。达到预算明确失败，不静默宣称完整历史。SSE 高频唤醒合并到至少 250 毫秒间隔。
- 客户端断连不关闭 Host 的连接。OpenCode 上游短暂断连显示 disconnected，后台重读状态、消息和 pending 后恢复；Codex 连接断开后显式重新附着。Host 重启后两类连接均可用原 native ID 再次建立，不自动发送旧输入或重批。
- 原 `/cancel` 对外部连接表示分离，不中断原任务；中断必须显式发送 `AgentMessage` 的 interrupt。Host 关闭和限额清理不发送原生 decline 或 interrupt，也不伪造原任务完成。
- 原生服务仍决定订阅者退出后的生命周期。真实 Codex 存活测试保留原桌面连接，不承诺最后一个原生订阅者退出后第三方服务仍保留活线程。
- 独立执行身份或 Host 原生进程硬配额无法覆盖外部进程，附着时拒绝这些配置，不宣称提供隔离。当前没有跨桌面与手机的全局独占租约；真正的多 writer 仲裁仍由第三方原生服务负责。

## 实施状态与剩余验收

**已完成**：审计、三个既有 Agent 的独立 Adapter 契约、Codex 已有控制服务连接、OpenCode v1/v2 已有服务连接、Windows socket 只读权限校验与 AF_UNIX 接入代码、Claude 官方 CLI 原生发现、ACP v2 历史显式回放修正、原会话 ID/工作区校验、受支持传输的原生 pending 恢复、审批去重及乱序处理、增量投影和重连、资源与权限限制、协议与回归测试。

**明确未支持**：没有控制端点的任意 PTY/stdio 进程、Claude 独立活进程的 Host 结构化附着、Antigravity Adapter、上下文注入替代原会话、自动启动或重启用户服务。

**Claude 部分完成的原因与此前判断的修正**：此前仅以 Remote Control 判断“缺少本地附着入口”过于笼统。[官方 Agent View 文档](https://code.claude.com/docs/en/agent-view)还提供原生 JSON 状态查询和后台会话的终端附着。前者已实现；后者按 ID 打开已停止的会话时可以重启它，且没有向本 Host 暴露结构化 pending 请求 ID、工具调用关联和“只附着、不恢复/重启”的握手。本实现不自动执行 `attach`、`--bg`、`respawn` 或 `daemon`，也不通过终端文字猜测审批协议。用户可在官方终端中自行使用 `claude attach`，但这不计作 NimoteHost 的结构化接管。[官方 Remote Control](https://code.claude.com/docs/en/remote-control)连接官方服务，未被当成本地 LAN API。[现有 ACP Adapter](https://github.com/agentclientprotocol/claude-agent-acp)的活 session 恢复指自身进程持有的对象，不是独立 CLI 进程的控制连接。全部 Agent 的所有运行方式尚未全部支持。

**本机安装与验证范围**：Claude Code 2.1.311 位于 `~/.local/bin/claude`；ACP 0.107.3 固定安装在 `~/.local/share/nimotehost-claude-acp`，启动入口为 `~/.local/bin/claude-agent-acp`，通过上游公开的 `CLAUDE_CODE_EXECUTABLE` 复用现有 CLI，显式设置该变量时仍优先使用用户值。使用现成 npm 包，没有在 Go 项目内添加 Node 依赖。全局 npm 尝试因重复可选二进制解包触及空间上限而回滚，随后独立目录安装成功；本次临时缓存已清理。安装不会自动登录或执行模型任务。

**验收未完成**：真实 OpenCode、Claude/ACP 与实际外部模型的全面矩阵；Linux/Windows 原生运行、手机/平板与真实 SSH 网络；长期负载及复杂第三方工具状态。没有这些环境时不能标为通过。手机端还需要调用新的 attach 参数；本仓库没有修改独立的手机客户端。

新增唯一直接依赖 `github.com/coder/websocket v1.8.14`，用于官方 WebSocket 握手、帧、消息上限与关闭，来自已有本机缓存；没有网络安装、新 daemon、公网服务或隧道。测试边界见[测试记录](external-agent-sessions-tests.md)。
