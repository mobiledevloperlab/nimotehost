# 已完成功能 / Completed functionality

English / 简体中文 · `1.0.0-dev` · 更新 / Updated: 2026-11-14

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
| 审批 / Approvals | `agent_execution_policy_v1` 提供 confirm/smart/auto 会话策略；未指定保持 confirm。智能模式只自动批准可验证的工作区读取、搜索和 ACP 结构化编辑，未知/越界/删除仍转原生 pending。自动模式由 daemon 回复单次原生批准，客户端断开仍有效；不选择 ACP allow_always，保留 provider 原有审批与沙箱配置。策略更新要求会话空闲，先保存 Session 元数据再生效，非确认策略禁止终端降级。 |
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
nimotehost start             # 前台运行 / foreground
nimotehost capabilities
nimotehost status
nimotehost install           # 当前用户后台服务 / per-user service
nimotehost restart
nimotehost stop
nimotehost uninstall         # 移除服务，保留身份和记录 / retain identity and records
```

Linux 注销后运行取决于 systemd 用户会话和 lingering 策略。macOS/Windows 用户服务
依赖登录会话，不保证注销后运行。Unix 服务安装保存绝对 PATH 项和 XDG_CONFIG_HOME；
工具路径改变后，让任务结束，再 stop/install 刷新。重复安装不重启健康服务，升级不强行
打断任务；回滚前备份状态，旧二进制可能不认识新字段。
Logout survival depends on the OS user-service policy. Finish active work before upgrading;
back up state before downgrading because old binaries strictly decode metadata.

## 身份与生命周期 / Ownership and lifecycle

Host 默认监听 `127.0.0.1:47831`（可通过共享私有配置改端口，始终为 loopback）；每个请求需要本地随机 256-bit Bearer token 和
`Nimote-Host-Protocol: 1`。手机通过原认证 SSH 调用远端 CLI，本地 token 不返回手机。
Unix 目录/文件使用 0700/0600，Windows 使用当前用户 DACL；启动使用状态锁。
Credentials stay on the host. Every RPC is authenticated within the existing SSH OS-user boundary.

浏览器 Origin 请求拒绝；不提供 CORS。请求体上限 1 MiB，未知字段、多个 JSON 值和非法
会话 ID 被拒绝。会话 ID 是 64 位十六进制随机串，Host ID 独立持久化。
权限域为当前 OS 用户；原有完整 SSH 客户端仍共享账号权限；新增的受限 observer 设备使用独立可撤销身份。

`session_disconnect_survival=true`、`session_restart_survival=false`：手机断开只分离；
停止 Host 会终止其托管进程，Host/OS 重启不恢复活进程，未完成记录标记 interrupted。
结构化最终结果只有存在协议证据时才保存为 completed/failed；旧记录缺证据时返回 unknown。
Mobile disconnect does not kill owned processes; Host/OS restart does not resurrect them.
Native conversation resume creates a new managed process for that conversation.

## RPC / Session API

手机优先复用 SSH exec 或 `rpc-stream`；无需新的消息总线。

```sh
printf '%s' '{"method":"GET","path":"/v1/capabilities"}' | nimotehost rpc
```

跨平台可使用 `nimotehost rpc-encoded <base64(JSON)>`，不要把用户输入插入 shell 字符串。
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
| POST /v1/sessions/{id}/agent-message | prompt/steer/interrupt/approval/discover/configure/compact，按 provider 能力 / provider-gated actions |
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
- Codex controls.steer=true 表示支持执行中追加消息。agent-state 返回 turn_id 和
  can_steer；turn_started.data 同时返回 id 和 can_steer。发送 action=steer、text、
  turn_id 和唯一 request_id，Host 通过 turn/steer 的 expectedTurnId 校验当前轮次。
  启动中、压缩中、过期轮次和 ACP 会话拒绝此操作，不自动启动新轮次或排队。
  HTTP 回执只确认 Host 接收；provider 确认后输出 user_followup（text/turn_id/request_id），
  拒绝或进程停止时输出 steer_rejected（text/request_id/error），不改变当前轮次状态。
  每会话最多保留 16 个待确认追加消息；重试必须使用相同 request_id 和请求内容。
  Codex steering is bound to the expected active turn; late replies never revive or stop a turn.
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
| mutation_expiry_v1 | 支持七天有效期的请求键、安全回执轮换及状态中的容量诊断。 |
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
声明 `mutation_expiry_v1` 的 Host 接受 `v2.<Unix秒时间戳>.<64位小写随机十六进制>`，
Go 客户端可调用 `host.NewRequestID()`；GUI 在后台协商能力后用于审批与恢复操作，
连接旧 Host 时使用原有不透明请求键，协商失败则不提交操作。
请求键有效期七天，允许客户端时钟最多超前五分钟。过期请求在副作用前返回 400，
不能通过改用新键自动重试；应先核对会话。Host 启动及后续变更请求触发的每小时清理中，
先持久化只增不减的过期边界，再删除过期回执；时钟回拨或重启不会重新允许这些键。
旧格式回执继续保留，不通过淘汰旧键破坏原有防重放保证。
`GET /v1/status` 的 `request_storage` 与 GUI Help & diagnostics 提供数量、空间、旧回执数量和 80% 容量告警。
New expiring IDs allow safe bounded-window cleanup; legacy receipts remain reserved indefinitely.
Clients must negotiate the capability on each connection and must not submit expiring IDs to older Hosts.
`accepted` 只表示 Host 接收/排队，`terminal_written` 只表示写入终端；不等于 Agent 确认或任务完成。
Only structured provider evidence confirms completion; queue acceptance is not acknowledgement.

内容冲突返回 409；进行中的预留、崩溃后结果缺失、部分写入或回执保存失败返回 503 uncertain。
不能换新键绕过不确定结果。认证失败 401，Origin 拒绝 403，协议不符 426。
手机只在同一 Host 身份且支持幂等时重试丢失的创建响应一次，不自动重放终端输入。
Do not replay uncertain operations with a new key. A filesystem/process transaction is not promised.

进程 revision、活动 sequence、输出字节 offset 分开使用。生命周期只存事件摘要与输出引用，
不存提示、输出或工具 diff；审批响应排队不代表上游已执行。损坏的摘要报告不可用，不杀死会话。
单个输出日志损坏不再阻止 Host 启动：保留会话状态，在 error 中标记输出不可用，
读取该日志仍返回错误；不清空或覆盖原损坏文件。超前事件游标明确要求全量同步。
Unix 状态文件替换同步文件内容和父目录；Windows 保留文件刷新，不声称具备 POSIX 目录同步语义。
通知游标按 Host 保存在既有偏好中，首次历史建立基线，重连补取；序号缺失/重复拒绝推进游标。
窗口不足只提示重同步，不批量弹出过期审批。通知与偏好保存不具备跨进程事务，崩溃可能重复提示。

| 资源 / Resource | 限制 / Limit |
| --- | --- |
| 会话 / Sessions | 32 active，256 retained |
| 输出 / Output | 每会话最新 16 MiB，绝对字节游标；旧 .log 可读 |
| 进程事件 / Process events | 最新 1024 条 |
| 生命周期 / Lifecycle | 每会话最新最多 256 条，写入及加载均最多 256 KiB；超限先淘汰最早事件，单条超大事件拒绝且不推进游标 |
| 请求账本 / Request ledger | 65,536 keys，64 in-flight，64 MiB（含结果预留），单回执最多 2 MiB |
| 结构化轮次 / Structured work | 4096 不同工作请求后拒绝新工作，保留活动审批/中断处理 |
| 手机缓存 / Client cache | 四个会话视图 LRU，100 条应用内通知；不重复存大量输出 |
| 前台观察 / Foreground observation | 活动查询最多四并发，失败退避 30 秒；隐藏且不可达的 Host 至少 15 秒探测一次 |

已显式成功连接的新 Host，可在同工作区 IDE/AI/Git 前台继续观察；隐藏面板不读取终端输出
或发送输入。切后台、工作区变化或销毁后停止。未访问 Host 不自动连接；旧 Host 隐藏即暂停。
Foreground observation is scoped to a previously connected Host and the current workspace;
there is no background/offline system push.

## MCP 与开发检查 / MCP and checks

`nimotehost mcp` 使用 MCP `2025-11-25` stdio 生命周期，默认仅提供 host_capabilities、
host_sessions、host_output；本地设置 `NIMOTEHOST_MCP_WRITE=1` 才提供 host_start、host_cancel。
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

既有客户端功能回归记录：NimoteCode Host/Tasks/通知 318 项通过、
3 项因缺少原生 SSH/paramiko、go-task、just 环境跳过；静态分析通过。
Linux/macOS/Windows × amd64/arm64 六目标构建通过；交叉构建不等于对应系统实机验收。
当前 Host 增量的全量测试、race、vet 与原生验收结果见文末及[加固测试记录](host-hardening-tests.md)；
上述客户端记录不代表本轮重新测试了 NimoteCode。
The complete native/mobile matrix remains pending; current Host verification is recorded separately.

源码、发行、维护流程见根目录 [README](../README.md)、[贡献指南](../CONTRIBUTING.md)、
[安全政策](../SECURITY.md)和[更新记录](../CHANGELOG.md)。历史审计与截图已移出维护文档，
可从 Git 历史查阅；当前功能状态维护在本页与[未完成功能](pending.md)，
加固架构及实际测试分别维护在 [host-hardening](host-hardening.md) 与[测试记录](host-hardening-tests.md)。

<a id="desktop-architecture-audit"></a>

## 双入口审计与桌面版本 / Desktop architecture audit

本次审计依据入口、RPC/server/storage/manager/provider、hostservice、hostinstall、
构建器、CI 和现有测试完成；保留 `internal/host` 作为单一 Core，不机械搬迁目录。
Audit findings and reuse boundaries:

| 范围 / Area | 已有实现与处理 / Finding and decision |
| --- | --- |
| 入口 / Entry | cmd/nimotehost: help/version/setup/install/uninstall/start/stop/restart/status/capabilities/rpc/rpc-encoded/rpc-stream/mcp；参数与语义保留。 |
| 生命周期 / Lifecycle | start 前台 daemon；状态锁、认证 shutdown、Manager.Close 托管进程树；GUI 仅客户端，未安装服务时 detached 启动独立 CLI。 |
| Core | 原任务、32 活跃/256 保留会话、输出环、幂等、事件、Agent SDK、历史、MCP 全部保留。无需重新实现终端或 Agent。 |
| SSH/认证 / Auth | SSH exec 携带 RPC，Host 不实现 SSH server；本地 256-bit Token + 用户权限；已提供独立 observer、Session 范围身份/撤销及 Linux opt-in controller。GUI 不扫描 authorized_keys 冒充手机清单。 |
| IPC | 原认证 loopback HTTP 满足已有 IPC 优先复用原则；没有增加管理 HTTP 端口，也没有新增 UDS/Named Pipe 或公开监听。 |
| 服务 / Services | systemd user、launchd、Windows 登录计划任务；新增显式 CLI 路径构造器，避免原生服务错误启动 GUI。 |
| 平台 / Platform | Unix PTY/flock/进程组；Windows ConPTY/Job Objects/LockFileEx/DACL；保留。GUI 启动使用 Unix setsid / Windows detached process group。 |
| 配置 / Configuration | 原无配置文件，端口为固定 loopback；现新增共享私有 config.json，默认不变，停止 Host 才能修改。GUI 生命周期通过 control.lock 跨进程串行化。 |
| 环境 / Environment | 原 NIMOTEHOST_MCP_WRITE=1、Unix PATH/XDG_CONFIG_HOME、Windows LOCALAPPDATA、交互 SHELL 均保留。新增 Fyne 自身主题/首选项存储；不复制环境凭证。 |
| 发布 / Distribution | 原 tools/build、安装事务、许可证校验、源码导出和六目标 CLI 构建保留；-gui 原生附加构建，独立归档，打包 CLI 与 GUI 许可证。 |
| 移动协议 / Protocol | Protocol=1、RPC 路径/请求/响应、能力协商不变；旧移动端通过同一已更新 CLI 读取端口。没有启用 P2P/DERP。 |

新增代码 / Added boundaries:

- `internal/desktop`: 认证 RPC facade、Host 状态、真实会话、Agent 快照/审批、
  跨进程启停锁、独立进程启动和脱敏诊断。它不导入 Fyne。
- `cmd/nimotehost-gui`: 独立 Fyne module、四入口侧边导航、托盘、主题、六语言与管理界面。
  系统托盘菜单与关闭拦截使用 [Fyne 原生接口](https://developer.fyne.io/explore/systray)。
- `internal/host/config.go`, `log.go`: 私有共享配置、Host 锁下写入、64 KiB 有界日志。
  Token、Agent/终端输出不进入新运行日志；导出仅包括健康摘要，不包含运行日志。

启动 GUI 不自动启动 Host。Start 检测已有 Host；未运行时复用用户服务或启动独立 CLI。
GUI 五秒轮询元数据，不读取终端输出；关闭/崩溃不拥有 daemon 的取消上下文。
Windows/macOS 关闭隐藏；Linux 托盘能力无法通过 Fyne desktop 接口可靠探测，因此默认
正常窗口退出，仅在用户明确启用已知可用托盘后隐藏。Quit GUI 与 Stop Host 分开。
Enabling login startup installs/starts the existing user service; disabling removes it.
Both require Host stopped to avoid implicit interruption. User login, not machine boot
without a user session, is the native service guarantee.

### 桌面交互与国际化 / Desktop interaction and localization

Overview、Sessions、Devices、Settings 为四个一级入口。首屏只显示本地服务状态、主要操作、
活动/待审批摘要和手机连接指南；不以 Host 正常运行证明 SSH 或手机已连接。
Sessions 使用名称与工作区，支持搜索、运行/待审批/结束筛选，优先展示待审批；完整 ID 仅在详情内。
原生历史位于 Past conversations，直接阅读对话正文和工具结果，保留分页和同对话恢复。
停止/重启进入二级菜单并确认影响；端口/日志级别默认折叠；诊断和原始数据按需打开。
All four pages use existing Fyne widgets; no web frontend or custom UI framework is introduced.

视觉层采用 Fyne 公开 Theme/ThemeOverride、List 与 Layout 接口：横向图文侧栏、服务状态区、
活动/待处理计数与手机连接并列卡片，主次文字和状态颜色分层。浅/深主题即时切换，卡片跟随
当前主题，不保存旧配色。未知/不可用状态的计数显示 `—`，不把不可达显示为零活动。
点击待处理入口会清空旧搜索并切回本机审批列表，避免历史子页或旧筛选遮住请求。
主窗口采用 960×640 的最小逻辑尺寸避免导航和长文案挤压，内容区仍可滚动。
测试额外检查浅/深配色的正文/辅助文字/主要按钮对比度、两主题六语言布局及按钮尺寸。
These palette checks are scoped to application colors, not a complete accessibility certification.


六种语言与 NimoteCode 对齐：`en`、`zh-Hans`、`es`、`ja`、`ko`、`ru`。默认英语，
Settings → Language 即时切换并保存 Fyne 偏好；重建窗口内容只取消旧视图查询，不停止 Host 或任务。
`translations/*.json` 内嵌到 GUI，复用 Fyne 已依赖的 go-i18n 和 x/text；无新模块版本。
未知语言回退英语。名称、命令、目录、用户/Agent 正文及技术 JSON 不翻译；文件选择器遵循系统/框架语言。
Fyne 正式构建使用系统字体回退，Linux 应提供中日韩字体；`ci` 测试禁用系统字体扫描，
不能用该模式的截图判定真实中日韩字体。macOS 非 `ci` 预览已检查中日韩字形。

回归覆盖六份目录的键集合、模板变量一致性、字面键引用、语言偏好、会话筛选排序、
未知状态、回收行状态文字尺寸、命名设备表单及六语言四页面窗口尺寸。
可选预览命令见 CONTRIBUTING；真实 OS 托盘、辅助功能和母语审校仍需分别验收。

交互依据 / References: [Docker Desktop navigation](https://docs.docker.com/desktop/use-desktop/)、
[Tailscale preferences](https://tailscale.com/docs/features/client/manage-preferences)、
[Apple disclosure controls](https://developer.apple.com/design/human-interface-guidelines/disclosure-controls)。
采用这些入口分层原则，不复制其他产品的界面资产或引入其网络/账号体系。

### 桌面构建与安装 / Desktop build and installation

```sh
# 必须在目标 OS / 架构的原生 runner 上运行 / native build
# Go 1.26.5+；CLI 仍可 CGO_ENABLED=0 独立编译
go run ./tools/build -gui -version 1.0.0-dev
```

- Linux amd64：gcc、libgl1-mesa-dev、xorg-dev、libxkbcommon-dev；运行需要兼容
  OpenGL/X11（Wayland 可通过 XWayland）的桌面环境。CLI 服务器不需要这些依赖。
- macOS amd64/arm64：Xcode Command Line Tools；生成包含 GUI 与 CLI 的 `.app`。
  发布包目前未签名/公证；开发者正式分发须使用自己的 Developer ID 对内部 CLI、GUI、
  app 依次 codesign，再 notarize/staple，重新生成校验和与归档。不得声称已完成签名。
- Windows amd64：原生 Go + MinGW-w64 gcc（CI 使用 msys64/mingw64/bin）；生成无控制台
  GUI 与 CLI zip。沿用用户安装脚本，无需管理员权限，不提供 MSI/Authenticode 签名。
- Windows arm64、Linux arm64 CLI 保留。GUI 可在对应原生桌面/SDK 下执行相同构建命令，
  但当前 GUI CI 已配置 Linux arm64 原生构建，但尚未执行验收；Windows arm64 GUI 未配置，不能当作已支持的 GUI 发布产物。

`dist/gui-<os>-<arch>` 和 `nimotehost-gui-<version>-<os>-<arch>` 为桌面完整分发；
原 CLI 名称不变。现有脚本可安装完整 GUI 包，保留整个固定安装目录。
macOS 应启动已安装目录中的 app，不要在临时解压目录启用长期服务后删除它。
同一 v* Tag 在所有必需 CI 成功后统一上传 CLI、GUI 与源码；所用 runner 标签参考
[GitHub 官方列表](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)。
GUI 构建不支持无 SDK 的普通 GOOS/GOARCH 交叉编译，构建器会明确失败。

原有自动测试覆盖协议、认证、会话、Agent、幂等、安装事务与服务定义。
新增自动测试覆盖真实 CLI 后台进程、管理进程退出后继续运行、重新连接、重复启动、
配置锁/一致性、端口冲突、窗口隐藏/关闭/重新打开/GUI 退出后活跃任务继续运行。
Fyne 窗口测试使用无显示器测试驱动；不能替代真实系统托盘、登录/重启与手机验收。

本次桌面改造实际验证 / Verification for this change: 完整 `go test -mod=readonly -race ./...`、
根模块 `go vet ./...`、桌面模块 `go test -tags ci ./...` 与 `go vet -tags ci ./...`、
两模块 `go mod verify`、5 项 Python 检查均通过。CLI 六目标构建成功，
macOS arm64 GUI 原生编译、联合打包和真实图形启动通过，GUI 退出不发送 Host 停止请求；
归档清单逐文件校验通过。Fyne 2.8 选择器的缺译占位键通过公开翻译资源 API 补齐；
不修改 SDK 或进程环境来切换语言。三平台托盘、签名、服务/系统重启和手机矩阵仍列在 pending。

## 只读设备授权、历史与更新入口 / Observer devices, history and updates

现新增 `nimotehost devices list|add <name>|revoke <id>` 与 GUI Devices 注册/撤销。
每个设备使用独立随机凭证，凭证保留在 Host 本机私有 devices 目录；记录存储凭证摘要。
每次 HTTP 请求重新验证设备记录；撤销持久化后，旧 RPC stream 后续请求也失败。
已经通过认证的在途请求可能完成，不承诺撤销会回滚此前已返回或已读取的数据。
其他设备、owner Token 与运行任务不受影响。列表不返回原始凭证或摘要。

原有 `devices add` 默认创建 observer：允许状态、能力、会话元数据、会话输出、activity 和 Agent
状态读取；禁止任务创建/输入/审批/停机/原生历史 helper 等所有写入或进程启动入口。
observer 可看到该账号的会话输出，授权前应确认该可见范围；新增 Session 范围身份可限制读取；原有 observer 仍为账号范围读取。
Default observer devices are read-only because unrestricted same-user command execution would expose owner
credentials and defeat revocation. Existing full SSH accounts are not retroactively isolated.

Unix 管理员为每台设备准备独立 SSH 公钥，把 GUI 显示的 `restrict,command="..."`
条目安装到对应账号的 authorized_keys，替换其中 PUBLIC-KEY 为真实公钥；该密钥不能
同时保留不受限的登录条目。强制命令为固定绝对路径的 `nimotehost device-bridge <id>`，
仅接受原有 rpc/rpc-encoded/rpc-stream/status/capabilities 桥接，不执行 SSH 原始字符串。
`restrict` 禁止转发、PTY 和 user rc；详见 [OpenSSH 官方说明](https://man.openbsd.org/sshd)。
本功能不自动修改 SSH 配置，也不修改已有普通 SSH 密钥。Windows 需按服务器默认 shell
配置正确强制命令；GUI 不输出未经原生验证的 Windows 授权行。
撤销 Host 权限后，还应移除相应受限 SSH key 来禁止后续 SSH 登录；Host 撤销无需等待该操作。

Sessions → Past conversations 从当前 Host 的原生 provider API 读取历史列表（支持列表分页）与 transcript，
Codex transcript 可用 Next transcript page 继续读取，保持原生游标。
每条历史投影按编码后的 JSON 限制为 24 KiB，缩短内容明确标记 `truncated`，
最多 50 条的 Codex 页保持在 RPC 的 2 MiB 限额内。ACP 没有可续读的 transcript 游标，
先用最多 4 MiB 的轻量原始通知缓冲排空 SDK，再生成最多 2,000 条、总预算 1 MiB 的投影；
超过任一预算返回 `truncated=true`，
不把截断结果当作完整历史。
GUI 使用原 conversation_id 显式恢复新结构化 Agent 会话；不重放旧提示，不自动转成新对话。
加载目录/Agent 与展示数据绑定，编辑查询条件不会改变已选记录的恢复目标。
Settings 可显式查询官方 GitHub 最新发行版，并在用户确认后打开下载页，使用原有校验和
安装事务更新；不执行下载代码、不静默覆盖活跃服务。版本查询使用
[GitHub 官方 releases API](https://docs.github.com/en/rest/releases/releases)。
原生跨主机 Dashboard、离线推送、活进程跨 daemon/OS 恢复等仍列 pending，不能用本次
桌面映射或 fake provider 测试代替它们的实现与真机验收。

设备功能基线验证 / Baseline device verification: 全量 Go race、两模块 vet、Fyne 生命周期、
真实 CLI device-bridge 集成、旧 RPC stream 后续请求撤销、持久撤销、其他设备和 owner
隔离、越权写入拒绝均通过。权限边界模糊测试约 51 万次未失败。此基线不包含真实 SSH
服务器 key 配置；后续 Linux WSL 的真实 OpenSSH 与 opt-in controller 验收见下方。
物理手机、Windows/Linux 图形环境和跨平台完整控制设备仍未完成发布验收。


## Host 加固增量 / Host hardening increments

2026-11-14：保持 Protocol 1、SSH/RPC/MCP、现有 Session JSON 和请求账本格式；未修改 UI/客户端。

- Session 范围的 session_observer / session_operator 与 `devices add-scoped`；
  operator 只可取消、打断、拒绝，列表、事件和逐项访问均检查授权，逐请求检查撤销。
- Linux 高级独立执行身份：owner 在创建 Session 时显式指定专用 numeric UID/GID，
  root 管理员配置私有有限额 cgroup v2 委派与保护的可执行文件后，
  `devices add-controller <name> <session-id>...` 可授权该 Session 输入、审批等完整控制。
  UID 每会话独占且永久保留；清除 owner 环境、禁止提权及命名 Unix socket broker 连接。
  允许运行时信号唤醒所需的已连接匿名 stream socketpair，拒绝可重连的其他类型与 io_uring。
  Darwin/Windows 与不满足条件的 Linux 明确拒绝，不能把同用户 shell 当作隔离。
- 审批校验活动 thread/turn、原生可选决策、轮次结束失效；重复请求绑定完整消息哈希与主体。
- 私有 resources.json 配置并发、保留、运行时限、磁盘下限；Windows Job Object
  支持 CPU、总 committed memory、活动进程数；Linux 配置已委派的 cgroup v2 后支持
  对应控制器下的 CPU、memory 和 tasks 限制。macOS 硬配额明确不支持。
- HTTP 连接上限 128；设备分别最多 8 读/2 写并发，owner 有独立保留容量；
  读写准入分离、历史 helper 最多四个，不新增隐式任务重试队列。
- Unix 独立 supervisor 通过 Host lifetime pipe 处理 Host SIGKILL、取消及根进程退出；
  子进程 PID 保留到组清理完成再 reap，避免旧 PID 误杀。Linux 配置 cgroup 时也清理 setsid 后代；
  默认 Unix 进程组不保证清理主动逃逸后代。历史 helper 复用进程所有权和取消路径。
- 本地 `repair inspect` / `repair apply <plan-id>`：只在 Host 停止时执行，
  验证审查计划未变、先保存备份、隔离损坏元数据及重建事件，保留回执/凭据/输出，
  持久递增重置水位通知旧 cursor 重同步；未知或不安全状态拒绝修复。
- 共享 Agent Adapter 描述、结构化进程启动与历史 helper 生命周期；显式版本探测、
  配置感知的 HostCapabilities、资源状态与稳定错误分类。Windows 原生验收发现的
  目录迁移锁共享、日志截断与默认工作目录问题已修复并纳入回归。

Implemented: scoped authority, Linux opt-in execution identities, native containment,
fair admission, explicit backed-up recovery and shared Agent process lifecycle.
Platform support and verification are bounded by the actual backend; no release certification is implied.

权限模型、部署前提、资源语义及回滚见 [Host 加固审计](host-hardening.md)；
实际测试、未验证项与剩余风险见 [加固测试记录](host-hardening-tests.md) 和 [待办](pending.md)。

### 已完成清单与验收范围 / Completed checklist and verification scope

以下 10 项与 [pending 清单](pending.md) 的已勾选项对应；每项只在明确的平台和测试范围内完成。
实现提交：`82e86d2`、`4fb0c6e`、`876f481`；pending 状态同步提交：`059d55e`。

- [x] Linux 完整控制设备执行隔离：root、私有有限额 cgroup v2、每会话独占 numeric UID/GID、
  私有身份记录与 session_controller；Ubuntu WSL 原生身份边界及 race 验收通过。
- [x] 新格式请求键：七天有效期、安全在线轮换、容量告警；旧格式回执的退役迁移仍未完成。
- [x] Linux CPU 硬配额：核对 cpu.max，真实负载观察 nr_throttled 增长；只操作新建测试子树，
  未自动启用全局控制器。
- [x] Linux 内存硬配额：实际触碰 128 MiB，在 512 MiB 配额下成功、64 MiB 下由内核终止；
  CPU/内存均通过原生 race 验收。
- [x] Unix supervisor 与 Linux cgroup 清理：验证 Host SIGKILL 后普通进程组停止，以及
  cgroup 内 setsid 后代在撤权时停止；默认非 cgroup 后端的主动逃逸后代仍有边界。
- [x] Linux WSL 真实 OpenSSH：临时 daemon/端口/密钥覆盖强制命令、Session 范围访问、
  shell/SFTP/TCP 转发/PTY 拒绝、持久撤权及 owner 仍可访问；原生 race 验收通过。
- [x] macOS owner 的真实 Codex/Claude Code/OpenCode + deepseek-flash：批准、拒绝、
  重复审批仅一次原生答复、原 conversation 恢复和随机 marker 连续性均已验证。
- [x] Linux 独立 UID 下 Codex 0.169.0 + deepseek-flash：使用 session_controller 实际完成
  批准、拒绝、去重及新 UID 下恢复；owner 显式转移私有 profile。原生 race 验收通过，
  并修复 Tokio 匿名 Unix stream socketpair 启动兼容性；命名 broker 等负向内核回归保留。
- [x] 公平准入与安全恢复：逐设备读/写并发、有限连接、owner 保留容量；明确状态修复、
  备份及递增恢复水位均有回归覆盖。
- [x] Windows Job Object：原生 CPU/committed-memory/进程数限制、真实内存分配拒绝及
  Job 生命周期已验证；不代表 Windows 独立执行身份后端已经实现。

当前 Host 增量验证：macOS `go test ./...`、`make check`（全量 readonly race、vet、模块校验、
5 项 Python 检查）、原生构建通过；Ubuntu WSL 全量测试、race、vet、原生构建，以及显式
root 身份/CPU/内存/SSH/真实 Codex 的 race 验收通过。Windows 当前代码 Host 包原生回归通过，
本轮未重跑 Windows 全模块 race/vet/build；六目标测试编译通过只表示可编译。
逐项命令、版本、基线与增量结果见 [host-hardening-tests](host-hardening-tests.md)。

Windows 临时 SSH 验收仍未通过，macOS SSH、独立 Linux OS 运维、系统重启/登录注销、
长期负载、磁盘硬配额、macOS 硬配额及 macOS/Windows 独立执行身份仍在 pending。
重启任何机器必须等待用户单独明确批准；本轮未重启、未改现有 SSH 服务，临时目录及模型凭据已清理。
These checked items do not establish full cross-platform acceptance or release readiness.
