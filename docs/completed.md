# 已完成功能 / Completed functionality

English / 简体中文 · `1.0.0-dev` · 更新 / Updated: 2026-11-13

本文只描述当前源码已实现的功能、使用方式与边界。未完成能力和验收统一见
[未完成功能](pending.md)。已实现不等于所有平台、手机和真实模型均已验收。
This is the implemented source contract, not a claim of full device acceptance.
See [pending work](pending.md) for remaining features and validation.

## 功能范围 / Features

| 功能 / Feature | 当前实现 / Implemented behavior |
| --- | --- |
| 可选 Go Host / Optional Go Host | 独立 Go module，通过既有认证 SSH 使用，无需 Connect、云账号或公网中继；不改变原 SSH/SFTP/Terminal 行为。 |
| Session Manager | 启动、列出、按 ID 附着、分离、输入、调整尺寸、终止、删除；进程由 Host 所有，手机断开不终止进程。Start/list/attach/detach/input/resize/cancel/delete remain daemon-owned. |
| 输出恢复 / Output replay | 每个会话保留最新 16 MiB 环形输出，使用绝对字节游标及保留起点检测过期；不重放命令。 |
| 平台后端 / Platform backends | Linux/macOS 使用 creack/pty；Windows 10 1809+ 探测 ConPTY，并使用 Job Objects 管理进程树。 |
| Agent 接入 / Agents | 已安装 Codex、Claude Code、OpenCode 的终端入口；Codex app-server，OpenCode ACP，Claude 通过已安装 claude-agent-acp。结构化能力按安装与握手协商，不自动下载 CLI。 |
| Agent 状态 / Activity state | 官方结构化事件投影为 running、idle、needs_approval、completed、failed、disconnected、unknown。存活的纯终端 Agent 显示 unknown，不匹配输出文字猜测状态。 |
| 对话历史 / Native history | provider 原生列表、transcript 和指定 conversation 恢复；原生 conversation ID 与 Host Session ID 分开，恢复失败不悄悄开启新对话。 |
| 审批 / Approvals | Codex 命令/文件审批；ACP 优先 allow_once/reject_once，不选择 allow_always。当前 pending ID、会话及请求 ID 由原适配器验证。 |
| 持久幂等 / Idempotency | 创建、终端输入、结构化消息的请求预留和回执；同一 ID 的不同操作、会话或内容冲突返回 409；未知结果不自动重跑。 |
| Task 关联 / Task links | 现有 SSH Task 菜单进入按 Task/工作区筛选的 Host 列表；新建及显式恢复携带 Task 引用；进入已有会话使用原 ID。 |
| 生命周期 Timeline | 每个会话保留最新 256 条摘要及序号，记录启动、执行、审批响应排队、完成与中断，只引用原输出游标。 |
| 前台通知 / Foreground notifications | 新 Host 按事件游标补取，覆盖列表轮询间隔内的短任务；已连接当前 Host 可在同工作区 IDE/AI/Git 前台继续观察。 |
| Git 审查 / Git review | 同认证工作区且目录匹配时进入已有 Git 面板并刷新；不复制 Git 实现或把 Agent 描述当真实 diff。 |
| 手机界面 / Mobile UI | 复用 Nimote 控件、终端、审批卡片、工具详情与通知列表；会话搜索、分组、固定、别名、原生历史、输出搜索/暂停/导出和横屏布局。 |
| 用户服务 / User services | Linux systemd user、macOS launchd LaunchAgent、Windows 当前用户 Task Scheduler；安装器检查就绪、保护活任务、保留升级备份。 |
| MCP / Packaging | stdio MCP 默认只读；提供模块/许可清单、校验和、源码导出、原生构建和六目标分发构建。 |

## 安装与服务 / Installation

普通用户使用完整分发包，无需 Go；从可信发布源获取包及校验和，核对 SHA256 后解压。
保留整个包，包括许可证、BUILD.json、MODULES.json 和 SHA256SUMS。
Use a trusted complete distribution, verify its checksums, and keep its license/manifests.

```sh
# 源码构建 / Source build (Go 1.26.5+)
go run ./tools/build
# 指定目标 / Targeted distribution
go run ./tools/build -os windows -arch arm64 -version 1.0.0-dev
# Unix 源码或解压包 / Unix source or extracted distribution
sh scripts/install.sh
# 源码跨平台入口 / Cross-platform source installer
go run ./tools/install
```

Windows PowerShell 使用 `.\scripts\install.ps1`。将二进制保存在远端用户 PATH 中的
固定位置，不移动服务已引用的二进制。安装不会配置 SSH 或自动安装 Agent。
Windows PowerShell uses `.\scripts\install.ps1`; keep the installed path stable.

```sh
nimote-host start             # 前台运行 / foreground
nimote-host capabilities
nimote-host status
nimote-host install           # 当前用户后台服务 / per-user service
nimote-host restart
nimote-host stop
nimote-host uninstall         # 移除服务，保留身份和记录 / retain identity and records
```

Linux 注销后运行取决于 systemd 用户会话和 lingering 策略。macOS/Windows 用户服务
依赖登录会话，不保证注销后运行。Unix 服务安装保存绝对 PATH 项和 XDG_CONFIG_HOME；
工具路径改变后，让任务结束，再 stop/install 刷新。重复安装不重启健康服务，升级不强行
打断任务；回滚前备份状态，旧二进制可能不认识新字段。
Logout survival depends on the OS user-service policy. Finish active work before upgrading;
back up state before downgrading because old binaries strictly decode metadata.

## 身份与生命周期 / Ownership and lifecycle

Host 固定监听 `127.0.0.1:47831`；每个请求需要本地随机 256-bit Bearer token 和
`Nimote-Host-Protocol: 1`。手机通过原认证 SSH 调用远端 CLI，本地 token 不返回手机。
Unix 目录/文件使用 0700/0600，Windows 使用当前用户 DACL；启动使用状态锁。
Credentials stay on the host. Every RPC is authenticated within the existing SSH OS-user boundary.

浏览器 Origin 请求拒绝；不提供 CORS。请求体上限 1 MiB，未知字段、多个 JSON 值和非法
会话 ID 被拒绝。会话 ID 是 64 位十六进制随机串，Host ID 独立持久化。
权限域为当前 OS 用户；同一 SSH 账号内没有独立逐手机角色授权。

`session_disconnect_survival=true`、`session_restart_survival=false`：手机断开只分离；
停止 Host 会终止其托管进程，Host/OS 重启不恢复活进程，未完成记录标记 interrupted。
结构化最终结果只有存在协议证据时才保存为 completed/failed；旧记录缺证据时返回 unknown。
Mobile disconnect does not kill owned processes; Host/OS restart does not resurrect them.
Native conversation resume creates a new managed process for that conversation.

## RPC / Session API

手机优先复用 SSH exec 或 `rpc-stream`；无需新的消息总线。

```sh
printf '%s' '{"method":"GET","path":"/v1/capabilities"}' | nimote-host rpc
```

跨平台可使用 `nimote-host rpc-encoded <base64(JSON)>`，不要把用户输入插入 shell 字符串。
`rpc-stream` 使用 JSONL：greeting `{"stream_protocol":1}`，请求携带 id/method/path/body，
响应按 id 匹配，最多八个并发。关闭 bridge 不取消任务。
Use encoded argv, not shell interpolation. Closing the RPC bridge only detaches clients.

| Method/path | 已实现语义 / Behavior |
| --- | --- |
| GET /v1/capabilities | version/protocol/min_protocol/platform/arch/host_id/features/tools |
| GET /v1/status | Host 运行状态 / Host status |
| GET /v1/sessions?after=revision | 全量或进程元数据增量 / full or process-metadata snapshots |
| POST /v1/sessions | 创建 task/terminal/agent / create session |
| GET /v1/sessions/{id} | 会话、Agent 状态与活动序号 / session and activity status |
| DELETE /v1/sessions/{id} | 删除已结束会话及输出/摘要 / delete finished session |
| POST /v1/sessions/{id}/cancel | 终止托管进程树 / terminate owned process tree |
| POST /v1/sessions/{id}/input | base64 终端输入 / terminal bytes |
| POST /v1/sessions/{id}/resize | rows/cols，1..1000 / terminal dimensions |
| GET /v1/sessions/{id}/output?offset=0 | data/start_offset/next_offset，wait_ms=0..1000 |
| GET /v1/sessions/{id}/agent-state | cursor/ready/busy/status/approvals/controls |
| POST /v1/sessions/{id}/agent-message | prompt/interrupt/approval/discover/configure/compact，按 provider 能力 / provider-gated actions |
| GET /v1/sessions/{id}/activity?after=sequence | events/cursor/resync_required |
| GET /v1/events?after=revision | 最新 1024 条进程元数据事件 / process-state events |
| GET /v1/agent-history?agent=...&directory=...&cursor=... | 原生对话列表 / native history |
| GET /v1/agent-history/{conversation_id}?agent=...&directory=...&cursor=... | transcript 分页 / transcript pages |
| POST /v1/shutdown-if-idle | 空闲时停止并拒绝新会话 / stop only if idle |
| POST /v1/shutdown | 停止 Host / graceful shutdown |

创建示例 / Example:

```json
{"method":"POST","path":"/v1/sessions","body":{"kind":"agent","agent":"codex","interaction":"auto","directory":"/absolute/workspace","request_id":"unique-random-id","task":{"id":"existing-task-id","label":"Fix build"}}}
```

- kind 为 task/terminal/agent。工作区必须是原生绝对目录；command/args 直接传给程序。
- interaction 为 terminal（默认）、structured、auto；auto 先完成握手，失败清理后才回退终端，
  不重放已分发提示。启动预算 45 秒；传输层为冷启动保留更长超时。
- resume.conversation_id 指向原生对话，不是 Host Session ID；不可恢复时不改为新对话。
- `agent-message` 要求最多 128 字节的 request_id。prompt 提供 text；approval 提供
  approval_id 和 decision=accept|decline。普通文本、终端字节和审批是不同操作。
- 结构化输出沿用 UTF-8 JSONL：ready/user/text_delta/message_completed/tool/turn_started/
  turn_completed/approval/approval_resolved/turn_rejected/error，附 type/data。
  在 output 字节游标与 agent-state.cursor 完全一致时应用快照，避免回放竞态。
- compact、模型、推理与斜杠命令按 provider controls 协商，不假装各 provider 能力相同。
- 结构化会话的手动“打开终端”入口可能创建独立 CLI，不承诺将原 thread 无缝迁移为 TUI。
  Opening a terminal from a structured session may create a separate CLI conversation.

## 能力、回执和历史 / Capabilities and receipts

| Feature flag | 合同 / Contract |
| --- | --- |
| mutation_idempotency_v1 | 创建与 input 的可选请求键；结构化消息已有必需键。 |
| agent_status_v1 | Agent 状态与进程 state 分离，未知协议停止原因保持 unknown。 |
| session_task_v1 | task.id 必填且最多 512 字节，label 可选且最多 160 字节；不执行原 Task 命令。 |
| session_activity_v1 | 会话内递增 activity_sequence；摘要页最多保留 256 条，过期/超前游标要求 resync。 |
| agent_history_v1 / provider_history_v1 | 原生历史按 provider 安装和实际能力协商；不扫描客户端主机文件。 |
| rpc_stream / agent_events_v1 / agent_controls_v1 | 分别协商 RPC bridge、结构化事件和控制。 |

新字段只在 Host 声明能力时使用，旧 Host 保持原行为。统一
`agent_structured_approval=false` 仍保守保留，不代表现有 Codex/ACP 审批代码不存在。
Gate new fields by capability. The umbrella approval flag remains false pending broader acceptance.

相同 request_id 必须绑定相同操作、会话和请求内容。执行前同步预留，完成后保存回执；
重试返回原回执而非最新快照。创建回执含 start_request_id。
`accepted` 只表示 Host 接收/排队，`terminal_written` 只表示写入终端；不等于 Agent 确认或任务完成。
Only structured provider evidence confirms completion; queue acceptance is not acknowledgement.

内容冲突返回 409；进行中的预留、崩溃后结果缺失、部分写入或回执保存失败返回 503 uncertain。
不能换新键绕过不确定结果。认证失败 401，Origin 拒绝 403，协议不符 426。
手机只在同一 Host 身份且支持幂等时重试丢失的创建响应一次，不自动重放终端输入。
Do not replay uncertain operations with a new key. A filesystem/process transaction is not promised.

进程 revision、活动 sequence、输出字节 offset 分开使用。生命周期只存事件摘要与输出引用，
不存提示、输出或工具 diff；审批响应排队不代表上游已执行。损坏的摘要报告不可用，不杀死会话。
通知游标按 Host 保存在既有偏好中，首次历史建立基线，重连补取；序号缺失/重复拒绝推进游标。
窗口不足只提示重同步，不批量弹出过期审批。通知与偏好保存不具备跨进程事务，崩溃可能重复提示。

| 资源 / Resource | 限制 / Limit |
| --- | --- |
| 会话 / Sessions | 32 active，256 retained |
| 输出 / Output | 每会话最新 16 MiB，绝对字节游标；旧 .log 可读 |
| 进程事件 / Process events | 最新 1024 条 |
| 生命周期 / Lifecycle | 每会话最新 256 条，加载最多 256 KiB |
| 请求账本 / Request ledger | 65,536 keys，64 in-flight，64 MiB（含结果预留），单回执最多 2 MiB |
| 结构化轮次 / Structured work | 4096 不同工作请求后拒绝新工作，保留活动审批/中断处理 |
| 手机缓存 / Client cache | 四个会话视图 LRU，100 条应用内通知；不重复存大量输出 |
| 前台观察 / Foreground observation | 活动查询最多四并发，失败退避 30 秒；隐藏且不可达的 Host 至少 15 秒探测一次 |

已显式成功连接的新 Host，可在同工作区 IDE/AI/Git 前台继续观察；隐藏面板不读取终端输出
或发送输入。切后台、工作区变化或销毁后停止。未访问 Host 不自动连接；旧 Host 隐藏即暂停。
Foreground observation is scoped to a previously connected Host and the current workspace;
there is no background/offline system push.

## MCP 与开发检查 / MCP and checks

`nimote-host mcp` 使用 MCP `2025-11-25` stdio 生命周期，默认仅提供 host_capabilities、
host_sessions、host_output；本地设置 `NIMOTE_HOST_MCP_WRITE=1` 才提供 host_start、host_cancel。
MCP 不启动 daemon，不自动打开端口。依赖及授权见[第三方声明](../THIRD_PARTY_NOTICES.md)。

Agent 适配继续放在原 Go provider 分支，复用 SDK、认证、启动探测、输出/事件和生命周期；
不自动下载 CLI，不绕过审批，不重放已分发提示。新增 provider 需要真实握手、取消、异常退出、
审批、回放与清理覆盖，不将 fake peer 当真实模型验收。
Adapter changes reuse the existing provider/SDK boundary and must preserve ownership and replay safety.

```sh
go test -mod=readonly -race ./...
go vet ./...
python3 -m unittest discover -s test -p 'test_*.py'
go run ./tools/build
# Unix 干净源码构建 / clean source build
sh scripts/check-clean-build.sh
```

最近功能回归：macOS 本机 Go race/vet 通过；NimoteCode Host/Tasks/通知 318 项通过、
3 项因缺少原生 SSH/paramiko、go-task、just 环境跳过；静态分析通过。
Linux/macOS/Windows × amd64/arm64 六目标构建通过；交叉构建不等于对应系统实机验收。
The latest functional regression passed locally; the complete native/mobile matrix remains pending.

源码、发行、维护流程见根目录 [README](../README.md)、[贡献指南](../CONTRIBUTING.md)、
[安全政策](../SECURITY.md)和[更新记录](../CHANGELOG.md)。历史审计与截图已移出维护文档，
可从 Git 历史查阅；当前状态只维护本页与[未完成功能](pending.md)。
