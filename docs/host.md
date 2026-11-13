# Optional Nimote Host

[English](host.en.md) | **简体中文**

## 安装与运行

Host 是独立 Go 模块，通过既有认证 SSH 使用本机任务、终端和 Agent CLI。无需 Nimote 账号或公网中继。Linux、macOS、Windows 的服务均以当前用户身份运行。

```sh
go install ./cmd/nimote-host  # 在 Host 模块根目录执行
nimote-host start           # 前台运行，无账号绑定或控制面依赖
nimote-host capabilities    # JSON 能力和工具路径
nimote-host status
nimote-host stop
nimote-host install         # 安装到当前 OS 用户的后台服务并启动
nimote-host restart
nimote-host uninstall       # 停止并移除服务，保留任务记录/凭据
```

先将二进制放在固定位置且加入远端 SSH 命令的 PATH，再 install；不要移动已注册服务引用的二进制。Linux 使用 systemd user；没有用户服务管理器时使用前台模式。需要退出登录后继续运行的 Linux 主机，按系统策略配置 user lingering。macOS 使用 launchd LaunchAgent。Windows 使用当前用户的 Task Scheduler InteractiveToken 登录任务，LeastPrivilege，不需要管理员权限；用户注销结束后不保证运行，不是 LocalSystem 服务。Windows PTY 使用 ConPTY（Windows 10 1809+），进程树使用 Job Object。

Linux/macOS 安装时保存当前 PATH 的绝对路径项及 XDG_CONFIG_HOME；工具路径改变后，等待任务完成，执行 stop 再 install 刷新。Windows 任务使用用户登录环境。通过 capabilities 核对实际工具路径。没有自动下载安装第三方 Agent，也不跳过其登录和审批。

Host 固定只监听 `127.0.0.1:47831`，每台机器/网络命名空间支持一个活动 Host。状态位于 `os.UserConfigDir()/nimote-host`：Unix 目录 0700、文件 0600；Windows 目录和凭据使用仅当前用户的受保护 DACL。token 为随机 256 bit，任何接口包括能力查询均需认证。Host ID 独立随机持久化。文件锁覆盖启动、恢复及关闭阶段。不要将 token 放入手机配置、命令参数或日志。

## RPC v1

所有 HTTP 请求：`Authorization: Bearer <local token>`、`Nimote-Host-Protocol: 1`。错误使用非 2xx JSON，版本不兼容为 426，缺少认证为 401，浏览器 Origin 请求为 403，不提供 CORS。写 JSON 必须使用 `Content-Type: application/json`。请求上限 1 MiB；未知字段、多 JSON 值、无效会话 ID 均拒绝。

手机优先使用已认证 SSH exec：

```sh
printf '%s' '{"method":"GET","path":"/v1/capabilities"}' | nimote-host rpc
```

跨平台客户端使用 `nimote-host rpc-encoded <base64(JSON)>`，编码内容只作为一个安全参数，命令/目录/输入不拼接进入 SSH shell。该参数不是认证密钥；可能含任务内容，不适合放秘密。token 由远端 CLI 本地读取，HTTP 仅发往固定回环地址，禁用环境代理和重定向。也可通过现有 SSH 转发直接访问 RPC，但仍须安全取得 token；不能因为走 SSH 就省略 RPC 鉴权。

| 方法/路径 | 行为 |
| --- | --- |
| GET /v1/capabilities | version、protocol、min_protocol、platform、arch、host_id、features、tools |
| GET /v1/agent-history?agent=...&directory=...&cursor=... | Codex/OpenCode/Claude 原生 conversation 分页；按 provider capability 提供 |
| GET /v1/agent-history/{conversation_id}?agent=...&directory=...&cursor=... | 原生 transcript 与工具历史 |
| GET /v1/status | 运行状态，不暴露凭据 |
| GET /v1/sessions?after=revision | 全量或变化会话快照 |
| POST /v1/sessions | 创建 task / terminal / agent |
| GET /v1/sessions/{id} | 读取状态 |
| DELETE /v1/sessions/{id} | 删除已结束会话及输出 |
| POST /v1/sessions/{id}/cancel | 终止托管进程树 |
| POST /v1/sessions/{id}/input | `{"data":"base64 bytes"}`，仅活动终端 |
| POST /v1/sessions/{id}/resize | `{"rows":40,"cols":120}` |
| GET /v1/sessions/{id}/output?offset=0 | base64 data、start_offset、next_offset、retention_bytes；按绝对字节游标恢复；wait_ms=0..1000 等待新输出 |
| GET /v1/events?after=revision | events、cursor、resync_required；持久化最近 1024 个状态变化 |
| POST /v1/shutdown-if-idle | 仅空闲时关闭并阻止新任务；活动进程返回 409 |
| POST /v1/shutdown | 当前账户授权的优雅关闭 |

创建示例：

```json
{"method":"POST","path":"/v1/sessions","body":{"kind":"task","command":"go","args":["test","./..."],"directory":"/absolute/workspace"}}
```

终端使用 `kind: terminal`；Agent 使用 `kind: agent, agent: codex|claude|opencode`，其余参数为 CLI 参数，遵循已安装版本的交互终端行为，不依赖非公开协议。Windows `.cmd/.bat` 包装器自动通过 `cmd.exe` 执行简单参数，拒绝包含 Shell 展开字符的包装器路径/参数；复杂 Shell 任务应显式使用 `cmd.exe`。原生 executable 的参数按 Win32 argv 转义。

每次创建随机会话 ID，成功返回后由调用者保存。公布 `mutation_idempotency_v1` 的 Host 支持创建及终端输入的可选 `request_id`（最多 128 字节），结构化消息原已要求该字段，详见下文。旧 Host 响应丢失仍必须先列出会话，不能盲目重发。所有进程以当前 OS 用户权限执行，无额外提权；token 等价于该用户的命令执行权，不提供多租户角色系统。

任务和终端独立于 HTTP/SSH 请求上下文。最多 32 个活动进程、256 个保留会话，每会话通过有界环形日志保留**最近 16 MiB** 输出（另有 24 字节文件头），达到上限后继续记录新输出并覆盖最旧内容；旧版 `.log` 前缀日志仍可读取，新会话使用 `.ring`；会话的 output_bytes / output_truncated 字段披露输出总量和截断。当请求游标早于保留范围时，响应 start_offset 大于请求 offset，客户端应重置 UTF-8 解码状态并提示历史缺口。输出按文件写入，正常结束执行 Sync，元数据通过同目录临时文件、Sync、Rename 原子更新；不承诺系统突然断电时最后输出字节不丢失。会话结束后可手动删除回收空间。事件游标过旧时应全量刷新会话。日志仅记录 ID、类别和状态，不记录命令内容或认证值。

客户端断开不影响进程；**Host/OS 重启不等于进程恢复**。重启时未完成记录标为 interrupted，不按持久化 PID 盲目发信号。Host 主动停止会终止托管进程树。Unix 下脱离进程组/会话的恶意或自行 daemonize 的程序不在普通进程树终止保证内。

## MCP 与第三方接入

MCP 协议层使用官方 [`modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk) v1.8.0；ACP 历史查询的 JSON-RPC 连接使用社区 [`coder/acp-go-sdk`](https://github.com/coder/acp-go-sdk) v0.13.5，统一处理请求匹配、取消和有序回放。Host 保留工具授权、进程托管及统一事件适配。

`nimote-host mcp` 提供 stdio MCP，固定兼容 `2025-11-25` 初始化生命周期；新协议客户端应允许协商到该版本。标准输出只写 JSON-RPC，错误日志走标准错误。默认仅暴露 host_capabilities、host_sessions、host_output。只有本地操作者显式设置 `NIMOTE_HOST_MCP_WRITE=1` 才增加 host_start、host_cancel；这些请求继续经过 Host RPC 鉴权、配额、路径与进程检查。MCP 不启动 daemon、不自动装载网络入口、不接收任意 URL。

参考协议：[MCP lifecycle](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2025-11-25/basic/lifecycle.mdx)、[MCP tools](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2025-11-25/server/tools.mdx)。Windows 实现参考 [ConPTY lifecycle](https://learn.microsoft.com/en-us/windows/console/creating-a-pseudoconsole-session) 和 [Job Object startup attributes](https://devblogs.microsoft.com/oldnewthing/20230209-00/?p=107812)。

SFTP 仍由原客户端负责。本版本没有把 SFTP 复制到 Host，也没有新增自动跨主机文件搬运、系统推送通知。Codex 可选结构化模式的审批见下文。

## 构建与验收

```sh
./scripts/build-host.sh linux amd64
./scripts/build-host.sh darwin arm64
./scripts/build-host.sh windows amd64
go test ./...
```

CI 添加三平台原生 Host 测试（含 PTY/ConPTY）及六目标交叉构建。**提交 CI 配置不代表这些 runner 已运行通过。** macOS/Windows 原生服务与真实移动设备接管仍待验收。本模块的构建支持与真实平台运行验收分别记录。

## 持久 SSH RPC

能力 `rpc_stream` 表示支持 `nimote-host rpc-stream`：在一个不分配 PTY 的 SSH exec 通道中，以 UTF-8 JSON 行复用双向 RPC。首帧为 `{"stream_protocol":1}`；请求为 `{"id":1,"method":"GET","path":"/v1/status"}`（可带 body），响应为 `{"id":1,"result":...}` 或 `{"id":1,"error":"..."}`。最多 8 个并发请求，响应可能乱序，按 id 对应。token 仍由远端 CLI 读取，不发给手机。输入不可因超时或断线自动重放；关闭通道只分离，不取消后台会话。客户端可同时等待输出和提交输入，避免逐按键创建进程。旧 Host 保持单次 RPC 回退。


### Codex / OpenCode / Claude 结构化交互与升级兼容

兼容策略参考 [Zed External Agents](https://zed.dev/docs/ai/external-agents) 和
[JetBrains ACP](https://www.jetbrains.com/help/ai-assistant/acp.html) 的协议适配隔离：
Agent 保有自己的运行时、认证和工具，客户端消费稳定的宿主事件。
ACP Agent 应通过 [initialize](https://agentclientprotocol.com/protocol/v1/initialization)
协商版本和能力。当前 Codex 适配器使用官方
[app-server 稳定接口](https://learn.chatgpt.com/docs/app-server)，不启用 experimentalApi，
不抓取 ANSI、不解析 TUI 文案、不依赖隐藏协议。OpenCode 使用官方
[`opencode acp`](https://opencode.ai/docs/cli/acp/)，显式协商 ACP v1。Claude 通过维护中的 [claude-agent-acp](https://github.com/agentclientprotocol/claude-agent-acp)
复用同一 ACP v1 会话层。

Linux/macOS/Windows 10 1809+ 的新客户端同时检查 `codex_structured`、`agent_events_v1` 和
`codex_structured_auto`（OpenCode 对应 opencode_structured / opencode_structured_auto，Claude 对应 claude_structured / claude_structured_auto），然后使用 `kind: agent, agent: codex, interaction: auto`
创建会话；OpenCode / Claude 分别将 agent 改为 opencode / claude。Host 在接收任何用户任务前完成 initialize
和 thread/start（ACP 为 session/new）；握手失败、超时（各 Agent 统一 45 秒）、
必需字段缺失或进程退出时，先终止并清理探测进程，再启动原生 CLI 终端，
返回 `interaction: terminal` 和 `compatibility_fallback` 原因。成功返回 structured。
不按 CLI 版本号硬编码允许列表，每次创建实际探测当前安装的 CLI。ACP 版本不支持、
握手格式不符和初始化认证失败都进入同一回退路径；运行中的认证/模型错误只报告错误，
不自动重发。

显式 `interaction: structured` 可用于要求结构化接口的集成；省略 interaction
或选择 terminal 保留旧行为。结构化模式及 auto 不接受 CLI args。新客户端
连接旧 Host、终端模式的继续会话保持原行为。客户端创建表单可以显式选择终端，
结构化会话标题栏也始终提供打开终端的入口。运行中的任务不会自动切换、取消或重发；
终端入口创建独立会话，不承诺把原 thread 无缝迁移为 TUI。

- `POST /v1/sessions/{id}/agent-message` 接受 `action: prompt|interrupt|approval`，
  必须附带最多 128 字节的 request_id。prompt 提供 text；approval 提供 approval_id
  和 decision: accept|decline。同一运行会话内重复 request_id 不会再次执行。
- `GET /v1/sessions/{id}/output` 沿用字节游标，data 解码后为 UTF-8 JSONL。
  `agent_events_v1` 的每行包含 type 和 data：ready、user、text_delta、message_completed、
  tool、turn_started、turn_completed、approval、approval_resolved、turn_rejected、error。
  文本事件使用 id/text，工具事件使用 id/kind/title/status/output/changes；changes 固定为
  path/diff 数组。审批使用 id/kind/command/cwd/reason/changes，并附带 item_id/grant_root（若有）。客户端不读取 Codex method、
  原始 params 或 CLI 特定的 tagged union。
- `GET /v1/sessions/{id}/agent-state` 返回 cursor、ready、busy、approvals。
  客户端仅在回放游标等于 cursor 时应用快照，以恢复保留窗口之前产生的待审批请求。
- `agent_history_v1` 表示至少一个 provider 支持原生历史；Host 还公布
  `codex_history_v1`、`opencode_history_v1`、`claude_history_v1` 子能力。Codex 使用 app-server
  thread API，OpenCode/Claude 使用 ACP session API。目录过滤和 provider cursor 由 Host
  adapter 处理，客户端不读 provider 私有文件。列表返回 opaque `conversation_id`，与 Host
  process session ID 不同；恢复请求在 `POST /v1/sessions` 中附带
  `resume: {"conversation_id":"..."}`。ACP 恢复需要原 workspace directory。恢复失败绝不回退到新对话。
- input/resize 不适用于结构化会话。interrupt 只停止当前轮次；cancel 结束进程。

未知可选通知和新增字段被忽略；未知服务端交互请求返回明确 JSON-RPC 错误，
不会被默认为批准。app-server / ACP 变化只在 Host 的对应适配器内处理；Host 的 v1 事件字段
采用增量扩展，未来破坏性变化必须增加能力版本，继续支持旧客户端，不能原地改变 v1 语义。

进程及审批等待归 Host 所有，断开客户端不会取消任务。Host 重启仍将未完成会话标记
为 interrupted，本版本不自动恢复 thread。仅支持命令和文件修改审批；每个会话最多
接受 4096 个不同请求后不再启动新轮次，但仍允许当前轮次的审批和中断，并保留这些控制请求的去重记录；后续工作需创建新会话。输入管道失败会关闭适配器并终止托管进程；会话结束后状态快照不再报告就绪、忙碌或待审批。输出受 16 MiB 滚动窗口约束，客户端历史
限制为 500 项、约 2 Mi 字符，单条文本最多 256 Ki 字符。事件通过已有 RPC 长连接的
output 长轮询读取。未安装 Claude ACP 适配器时，Claude 继续使用终端模式。

ACP prompt 使用 session/prompt，完成以该请求的 stopReason 响应为准。取消通过
session/cancel 通知发送，并为待审批请求返回 cancelled；收到完成响应前不启动下一轮。
审批“确认”只选择 allow_once，绝不映射到 allow_always；不存在单次选项时保留终端入口，
“取消”优先选择 reject_once，否则返回 cancelled。工具的部分更新会合并已有描述及 diff
预览；不支持的客户端文件/终端/问答调用返回明确错误，不声明未实现的 ACP 能力。

兼容性测试覆盖：旧 Host/终端路径、真实已安装 Codex 握手、OpenCode 2.3.0 自动模式真实握手（无模型调用）、模拟握手报错/超时/ACP 版本不符、
新增字段/未知通知、稳定事件适配、审批去重、状态快照及子进程清理。模拟升级测试并不
等于所有未来 CLI 的互通保证；不兼容时保留终端可用性，适配层可以独立于客户端更新。


### Claude 适配器与 Windows 管道

Claude 的结构化能力仅在 PATH 中找到 claude-agent-acp 时公开；原生 claude CLI 仍是
终端回退入口。Host 不执行 npx/latest、不自动安装或升级依赖。推荐安装维护项目的
固定版本独立程序包，并保留同目录中配套的 Claude CLI；它不需要系统 Node。npm 发行版
`@agentclientprotocol/claude-agent-acp@0.106.0` 要求 Node 22+。旧发行包名仍可用，只要提供
相同的 claude-agent-acp 命令。身份认证和模型由适配器处理，Host 不自动登录或切换凭据。

Windows 使用三个独立的匿名管道：stdin 传请求，stdout 只传协议，stderr 单独收集诊断。
进程仍通过 Win32 CreateProcess 的 Job List 属性原子加入 Job Object，取消/退出会清理
整个进程树；只继承显式指定的三个标准句柄。此路径不使用 ConPTY，并继续沿用现有
原生 executable / .cmd / .bat 参数转义约束。失败启动会关闭管道和 Job 句柄。

Windows 原生测试覆盖三个 Agent、npm 风格 .cmd 包装器、含空格的路径、中文及多行长输入、
stderr 隔离和停止后的进程清理。这些测试由现有 host-native Windows CI job 执行；
macOS 本地交叉编译成功不能替代 Windows 原生运行验收。

Claude 独立适配器 0.106.0 已在 macOS 通过真实 ACP 会话初始化探测，未发送模型任务。
首次启动可能耗时较长；初始化等待现统一为 45 秒。超出后自动回退终端，后续新建会话会重新探测，不永久禁用结构化模式。


### 冷启动超时预算

能力 agent_cold_start_wait 标识已安装此修复。结构化 auto 创建等待 45 秒，包含插件发现、认证状态检查和模型目录初始化。仅对这类
POST /v1/sessions 请求，桥接 HTTP 客户端等待 60 秒、服务端响应 deadline 为 65 秒、
App 的单次 SSH RPC 和持久 RPC 等待至少 75 秒。普通输入/输出/状态请求、连接协商和
其他会话创建沿用原有短超时。显式不兼容或进程退出仍立即回退，超时等待不自动重放请求。

回归测试使用延迟 12.5 秒响应的真实子进程和 HTTP socket，验证慢启动不会被旧的
5/7/10 秒限制降级；客户端虚拟时钟覆盖三个 Agent、两种 RPC 传输、并行状态请求、
最终超时及不重放行为。仅更新 host 或仅更新 App，不能替代整条链路的超时修复。

## 请求回执与 Agent 活动状态

`mutation_idempotency_v1` 在协议 1 上增加按 OS 用户持久化的请求记录。新客户端
使用随机请求 ID；重试必须复用相同 ID 和完整请求内容。ID 绑定操作、目标会话及
解码后的 JSON 内容，复用于其他内容返回 409。身份认证先于回执读取。
创建结果含 `start_request_id`，可用列表检查创建结果。重试返回最初回执，不是
新进程或最新会话快照；当前状态需重新查询。终端输入返回 `ok`、`request_id`、
`stage: terminal_written`；结构化消息返回 `stage: accepted`，仅表示 Host 接收并
排入发送队列，不能当作 Agent 确认或任务完成。后两者以结构化事件/快照为证。

分发前同步保存预留记录。并发请求仍在执行、重启后只有预留而无结果、终端部分
写入或执行后无法保存结果时，返回 503 和结果不确定错误。不得换新 ID 绕过去重。
创建可通过 `start_request_id` 检查；不确定的终端写入无法证明是否送达。
这是重复执行抑制，不是文件系统与 Agent 的分布式事务，也不保证突然断电/存储故障
下恰好执行一次。不会自动重放未知操作。读取超时/取消只分离客户端；显式取消
会话才终止进程树。

记录保存哈希、结果元数据/错误（含创建时原有命令/参数元数据），不复制终端流或
结构化提示正文。最多 65,536 个
请求键、64 个进行中操作，以及包含预留结果空间的 64 MiB 记录；单条结果最多
2 MiB。达到限制在分发前拒绝。删除会话后保留回执，避免延迟重试重新启动已删除
任务；不自动淘汰去重记录。目前没有在线轮换命令，长期部署前需评估保留策略并
备份状态，不可在仍有重试时删除记录。旧客户端省略 ID 时保持旧行为，不具备这些
重试保证。NimoteCode 仅在同一 Host 身份仍公布该能力时，对响应丢失的创建使用
原请求重试一次；不自动重放终端输入。

协议兼容覆盖旧 App/新 Host 及经过能力检测的新 App/旧 Host，不保证状态文件降级：
旧二进制严格解析会话元数据，可能拒绝新增持久字段 `start_request_id`。
回滚 Host 二进制前保留状态备份。

`agent_status_v1` 在完整会话查询增加 `agent_status`，在 `agent-state` 增加
`status`。进程 `state` 保持协议 1 原语义。结构化状态只来自适配器协议事件：
`running`、`idle`、`needs_approval`、`completed`、`failed`、`disconnected`、
`unknown`。一轮完成后进程可继续等待下一轮。未知停止原因保持 unknown。
终端 Agent 存活时活动状态为 unknown，不能据此猜测审批/空闲。`after` 修订号及
`/v1/events` 仍仅覆盖进程元数据变化；Agent 活动使用完整列表轮询或结构化输出游标。

`session_disconnect_survival=true`、`session_restart_survival=false` 明确区分
手机断开与 Host 重启。原生对话恢复是为指定 provider 对话创建新托管进程，
不是恢复已死亡 PTY。每个会话属于原有 SSH OS 用户权限域，未增加逐设备权限或
多租户角色系统。

进程结束时保存有协议证据的最终结构化结果，重载会话后保留该结果。旧结构化记录
只有成功退出码而无已保存证据时返回 unknown，不推测为 completed。

手机端沿用现有 Host 界面。应用前台时，状态变化接入现有
NotificationService，操作绑定原主机/会话并检查当前作用域；初次加载历史完成状态
不会重复通知。新 Host 通过 `session_activity_v1` 的持久事件游标补取短任务；旧 Host
仍使用快照轮询，短于轮询间隔的变化可能漏报。没有后台/离线推送。会话菜单仅在
同一认证工作区且绝对目录匹配时打开现有 Git 面板；其他仓库需通过原流程切换工作区。
变更由现有 Git runner 查询，不把 Agent 描述当成真实 Git 状态。
Task 引用及生命周期历史见下文。

## Task 引用与生命周期历史

`session_task_v1` 允许创建请求携带可选的
`task: {"id":"existing-task-id","label":"任务名称"}`，会话快照也返回该引用。
ID 必填且最多 512 字节，名称可选且最多 160 字节。引用由会话的 Host 和目录限定，
不授权执行 Task 命令，不改写 Task 配置，也不会把 Agent 活动伪装成 TaskService
命令已执行、测试通过或成功完成。

NimoteCode 现有 SSH Task 的菜单可进入按 Task 与当前工作区目录筛选的 Host 会话。
选择已有会话直接附着原 ID。新建会话和显式恢复原生对话，只有在 Host 公布能力且
目录匹配时才携带 Task 引用。创建/历史对话框打开后若 Task 或 Host 作用域改变，
旧操作会被拒绝。旧 Host 需要更新或清除该筛选，不会静默创建无关联会话。
原 Tasks 的终端执行流程继续保留。

`session_activity_v1` 在会话查询/创建回执增加 `activity_sequence`，并提供
`GET /v1/sessions/{id}/activity?after=<sequence>`。结果包含 `events`、`cursor`、
`resync_required`。事件具有会话内单调递增的 `sequence`、`type`、`time`、
`output_offset` 及可选的 `state`/`reference_id`。事件序号、进程 revision 和输出
字节游标是不同概念。负数/非法游标返回 400；过期或超前游标返回保留历史及重同步标记。

历史记录会话启动/结束/中断、Agent 就绪、提示接收、轮次开始/完成/拒绝、审批请求、
单次允许/拒绝响应排队及审批关闭。`turn_started` 可能在 Host 入队后合成，不等于
provider 确认；审批响应排队也不等于上游已执行。`session_finished.state` 是进程状态，
`turn_completed.state` 则来自结构化 provider 结果。没有结构化完成证据的零退出码
进程不会被显示为任务完成。

每个会话使用私有 `.activity` 文件保留最新 256 个摘要事件，加载大小有限制。
输入/输出、命令、提示和工具差异仍保存在原有位置，不复制到摘要。输出游标指向
原有滚动日志，日志内容可以独立过期。生命周期变更使用原子同步写入，文本增量
不触发摘要落盘。日志锁独立，不在 Agent 回调内反向获取 Manager 锁。
历史损坏/写入失败会报告不可用（500），不会隐藏或终止已有会话。删除会话会清除
对应摘要文件；升级前的旧会话不会被伪造出过去的生命周期事件。

会话菜单的 Timeline 使用现有 sheet/list 组件，显示 Task 身份和保留摘要，可显式刷新。
它补充现有 Agent 工具叙事，不建立第二个任务执行器或对话存储。Host 重启后如实
记录会话中断，不重跑命令。

用户显式成功连接支持活动事件的 Host 后，其保留面板可在同工作区的 IDE、AI、Git
前台时继续观察元数据/事件。隐藏面板不读取终端输出，也不发送输入。应用切后台或
切换工作区后停止观察，未访问过的 Host 不会自动连接。已连接 Host 随后不可达时，
隐藏面板最多每 15 秒探测一次。旧 Host 保留隐藏面板即暂停的行为。

前台通知只在现有按 Host 隔离的偏好中保存每个会话的事件游标。首次发现的历史
会话建立基线；本机创建的会话保存创建游标，可补取创建后立刻完成的轮次。
重连/重开面板从已存游标继续，查询最多四个并发，单会话历史查询失败退避 30 秒。
历史保留窗口不足时只提示一次重同步，不批量弹出过期审批。作用域变化后的迟到
结果不会通知当前主机。通知与游标保存并非应用终止事务，崩溃时可能重复提示；
请求幂等和原生审批所有权仍独立保证。首次观察以前或保留窗口以外的事件不保证
通知，后台暂停与局域网可达性限制仍然存在。
