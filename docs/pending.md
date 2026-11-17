# 未完成功能 / Pending functionality

English / 简体中文 · `2.4.0` · 更新 / Updated: 2026-11-17

当前源码已实现的功能、使用方式和协议只维护在[已完成功能](completed.md)。
本页以清单跟踪功能与验收：已完成子项标记 `[x]`，未完成、部分完成或待批准项标记 `[ ]`。
完成项的实现细节及证据分别见 completed 与加固测试记录，不把局部完成视为整体完成。
Implemented behavior lives in [completed functionality](completed.md). Checked items are complete within their stated scope; unchecked items remain open.

## 功能 / Features

- [x] 外部服务的可用接入路径（2026-11-17）：Codex 原控制 socket、OpenCode v2 原 HTTP 服务、原生 pending、状态校准和重连均已实现；Codex 真实 CLI＋本地模型夹具验证通过。详见[实施状态](external-agent-sessions.md)。
- [ ] 外部接管剩余验收：真实 OpenCode v1/v2、Claude 模型/工具流程、Linux/Windows 原生及手机/SSH 网络矩阵。OpenCode v1 HTTP 与 Windows Codex socket 只读验证代码已补齐，Windows 原生测试尚未执行。Claude 独立活进程的结构化接管尚未实现，官方终端入口可能重启停止的会话；Antigravity 尚未实现，不以历史恢复冒充接管。详见[测试边界](external-agent-sessions-tests.md)。
- [x] Claude 官方 JSON 原生发现；本机 Claude 2.1.311 / ACP 0.107.3 隔离初始化；ACP v2 历史显式回放。`claude attach` 是官方终端入口，但可能重启停止的 session，尚不能作为安全的结构化接管入口。此前“缺少本地入口”应按此更精确的边界理解。

- [ ] 统一 AI Session Dashboard：在现有 AI 模式按主机/工作区聚合会话、搜索与状态筛选；
  保存带观察时间的离线摘要，不将缓存伪装成实时状态。
  Unified AI-mode dashboard and timestamped cross-host offline summaries.
- [x] Linux 完整控制设备执行隔离：root + 私有有限额 cgroup v2 + 每会话专用 UID
  的显式 opt-in 后端与原生 WSL 验收已完成。
  Linux 的 UID 号段须由管理员跨 Host 保留，Agent 账号/凭据须独立配置；不提供通用容器或网络沙箱。
- [ ] macOS/Windows 完整控制设备执行隔离：无独立身份后端，当前拒绝完整控制授权。
- [x] 新格式请求键：七天有效期、安全在线轮换与容量告警。
- [ ] 旧格式请求键迁移及容量运维：
  旧格式回执仍不可安全淘汰，达到保留数量/空间限额时继续拒绝新操作。
  不通过删除旧回执允许延迟重放；需配套客户端迁移和明确的旧协议退役方案。
  Legacy-key migration remains open; expiring IDs now rotate safely within a bounded retry window.
- [ ] 完整同名 Session 方法封装：当前 session.list/start/attach/detach/peek/send/watch/stop/events
  由既有 REST/RPC 和订阅语义对应，尚无一套全部同名 JSON-RPC 方法。
  Same-name semantic Session methods, if needed, over the existing transport.
- [ ] 更完整的 Task/Timeline 协作：已有 Task 引用、原 Session 重附着和生命周期摘要；
  尚不自动导入所有外部已启动的终端，也不合并原 Agent 对话的完整历史或替代 TaskService 执行器。
  External-session enrollment and broader existing Task/Agent narrative integration.
- [ ] 独立于 Host 生命周期的活会话后端：当前 Host/OS 重启不会恢复活进程；
  tmux/psmux 等扩展需验证所有权、游标、取消及平台边界，不能按平台名称假定可恢复。
  Multiplexer-backed survival beyond daemon restart remains unimplemented.
- [ ] 后台/离线系统推送：当前只有前台、已连接 Host、局域网可达时的观察；
  Android/iOS 后台策略和通知权限尚需评估，不保证手机离线收到 LAN 消息。
  Background/offline system push remains outside current guarantees.

## 发布前验收 / Release acceptance

- [ ] Android/iOS 真机 × Linux/macOS/Windows Host × Codex/Claude Code/OpenCode 的完整矩阵：
  真实模型编码、同对话接管、审批批准/拒绝、超时/重复请求、Git Diff 审查及继续任务。
  Full physical-device/Host/Agent workflow matrix with real model and approval behavior.
- [ ] 应用切后台、断网/重连、多主机/多 Agent 并行；验证观察和输入的作用域隔离，
  保留窗口过期、超时取消、不可达、协议不匹配及旧 Host 降级。
  Network/lifecycle, concurrent sessions and compatibility acceptance on physical devices.
- [ ] Host 服务与系统重启、登录/注销、长时间运行的原生三平台验收；
  验证当前“不恢复活进程”的边界，不以交叉编译代替实际运行。
  Native three-platform service, login/logout and long-running validation.
- [ ] 完整 SSH/SFTP、Terminal、IDE、AI、Git、Tasks、Timeline 回归，覆盖未安装 Host、
  安装、断连、卸载四种状态；确认可选组件不影响原工作流。
  Original workflows with Host absent, enabled, disconnected and removed.
- [ ] 补齐被环境条件跳过的原生 scoped SSH、go-task 与 just CLI 集成检查。
  Run the native SSH and optional CLI tests currently skipped for missing dependencies.
- [ ] 当前源码的三平台原生 CI、安装/升级/回退验证、应用与 Host 配套分发验收后，
  再决定是否由开发预览版进入正式版本。统一 agent_structured_approval 标志保持保守，
  不以代码存在或 fake peer 测试代替全部真实审批验收。
  Release readiness and the umbrella approval capability require native and real-agent acceptance.

已删除的历史记录不作为当前版本验收结论；需要追溯时查阅 Git 历史。
Deleted historical reports are available in Git history and do not establish current release acceptance.

## 桌面版本剩余验收 / Desktop follow-up

- [ ] 将 macOS 13+、Windows 11 和 Ubuntu 22.04 作为候选最低环境，在各架构的干净系统完成安装、升级、重启和卸载测试；固定正式发行的 Go 版本后再公布最低要求。Linux GUI 的 glibc 2.35 检查不能代替显卡驱动、桌面库及其他发行版的兼容性验收。
  Candidate minimum environments and the Linux glibc ceiling require clean-system acceptance before publication; pin the release Go version.
- [ ] 只读设备的真实连接遥测，以及 SSH 密钥自动安装/移除和 Windows forced-command 原生验收。
  已有设备注册/列表/持久撤销和 GUI；SSH key 绑定目前由管理员配置，连接状态明确 unknown。
- [ ] Windows/macOS/Linux 真实桌面托盘、窗口、主题切换、登录/系统重启服务验收；
  Linux 不支持托盘的窗口回退已实现，Fyne 测试驱动不证明真实托盘可用。
- [ ] 新增 GUI 原生 GitHub Actions 五目标实际执行；Windows arm64/Linux arm64 GUI 评估，
  不以 Go 跨编译或 macOS 本机结果替代。Windows MSI、macOS 签名/公证、Linux 系统包后续评估。
- [ ] Desktop 的 SSH 连接历史、后台版本查询策略及自动更新安装。已接入 Agent 历史列表分页、
  transcript 读取、指定 conversation 恢复，以及显式官方版本查询/下载入口。
  Codex transcript 后续分页已接入；ACP 截断已显式标识，仍需上游可续读历史协议、
  真实 provider 恢复验收和安装包签名。
- [ ] NimoteCode 手机连接实测：共享配置默认端口及自定义端口、原 SSH RPC 行为、
  Host 未安装/已停止/运行三种状态；本仓库测试不代替移动端真机验收。


## Host 加固剩余门槛 / Remaining hardening gates

### 执行边界 / Approval boundaries

用户已明确：**重启任何机器必须先获得单独、明确批准**。提供 env.md 不代表授权重启。
系统登录/注销、生产 SSH/service/firewall 配置变更、系统级安装、挂载及文件系统配额变更，
也应在准备好影响范围、测试窗口与恢复方案后请求批准。本轮先使用临时目录、独立端口、
临时测试密钥与独立进程完成可逆验证；没有条件的项目保留于本页，不标记完成。
Machine reboots require explicit prior approval. Environment access alone does not authorize them.


- [ ] macOS/Windows 完整控制设备的独立执行身份；现有 session_operator 仍仅安全停止/拒绝。
- [x] Linux CPU 硬配额：在现有委派的 Ubuntu WSL 环境验证 cpu.max 与实际内核节流；
  未委派的系统仍明确拒绝，不自动启用全局控制器。
- [x] Linux 内存硬配额：实际触碰 128 MiB 内存，512 MiB 配额下成功、64 MiB 下由内核终止；
  CPU/内存原生 race 验收通过。
- [ ] macOS 硬 CPU/内存/进程配额尚未实现。
- [ ] 工作区磁盘硬配额：目前仅 Host 磁盘下限、输出/事件/账本容量与有限任务数，
  不限制任意工作区写入；应在专用卷/管理员文件系统配额下部署不可信任务。
- [x] Unix supervisor 的 Host SIGKILL 普通进程组清理，以及 Linux cgroup 下 setsid 后代撤权清理已验证。
- [ ] 默认 Unix 非 cgroup 任务主动脱离进程组后的回收；不承诺 supervisor 也被外部强杀、
  内核崩溃或断电时事务完整。
- [ ] 独立原生 Linux（非 WSL）、Windows/macOS 系统重启、登录/注销、长时间负载与安装升级回退验收；
  原生系统调用测试通过不等价于这些运维场景已验证。
- [x] Linux WSL 原生 SSH 全链路：用现有 OpenSSH、独立回环端口和临时密钥
  验证强制命令、Session 范围、shell/SFTP/TCP 转发/PTY 拒绝与撤权，原生 race 验收通过；
  没有修改现有服务或 authorized_keys。
- [ ] Windows/macOS 原生 SSH 全链路验收：Windows 普通管理员上下文的临时 sshd
  认证未通过，当前 token 未报告 SeAssignPrimaryTokenPrivilege；
  需要经批准的专用服务身份继续定位/验收，不能将本次失败标为通过。
- [x] macOS owner 会话的 Codex/Claude Code/OpenCode + deepseek-flash：实际批准、拒绝、
  审批去重与对话恢复已验证；具体版本见测试记录。
- [x] Linux 独立 UID + session_controller 下 Codex 0.169.0：真实批准、拒绝、去重与新 UID
  恢复通过原生 race 验收；隔离运行时匿名 stream socketpair 兼容修复及内核负向回归通过。
- [ ] 更广泛的真实 Agent 版本/模型/平台矩阵：Linux Claude/OpenCode 对应 CLI/ACP 运行时
  与隔离凭据配置尚未提供/验收；已验证的版本不推广为全部隔离后端的兼容结论。
  ACP v2 全量 message upsert、background work、终端显示及复杂恢复状态仍需专项一致性验收。
- [ ] 独立安全审查、长期饱和/故障注入；非可关闭 io.Writer 的阻塞仍无法由通用接口强制终止。
  Linux 执行身份仅保护私有 Host/会话权限，不隐藏世界可读文件，不限制 IP 网络，
  不承担内核漏洞或其他 root 管理员配置错误的防护。

- [x] 逐设备公平准入与有限连接，明确状态修复、备份及恢复水位回归。
- [x] Windows Job Object 原生 CPU/内存/进程数硬限制；内存分配拒绝和 Job 生命周期已验证。

以上已勾选项均限定于所述实现和测试范围；未勾选项仍是发布门槛。
Implemented contract and rollback: [hardening audit](host-hardening.md).
Actual verification and limitations: [hardening tests](host-hardening-tests.md).
These remaining items prevent claiming that all P0/P1 release goals are complete.

## 待批准与缺少条件的项目 / Approval and prerequisites

| 项目 | 当前状态与所需条件 | 准备或执行边界 |
| --- | --- | --- |
| 任何机器重启、登录/注销 | 等待明确批准与可中断时间窗口 | 先确认目标主机、服务/会话影响、恢复入口；本轮零次机器重启 |
| Windows 原生受限 SSH | 当前管理员临时 daemon 的验收失败，未关闭 | 可在批准后创建专用临时 SYSTEM 服务，仅监听 127.0.0.1、独立端口/密钥/config，测试后删除；不得改现有 sshd 服务/登录权限来绕过条件 |
| macOS 受限 SSH | 缺少获准的 sshd 测试运行环境 | 不擅自开启 Remote Login 或修改用户 authorized_keys |
| 长期负载与独立 Linux OS 运维 | 尚无专用、可持续占用的隔离主机和运行窗口 | 现有短时故障/并发回归不能代替长期与系统启动验收 |
| 磁盘硬配额 | 后端代码未实现，且未指定可操作的独立测试卷/文件系统 | 需先确定项目/卷配额方案；挂载、启用文件系统配额等操作等待批准 |
| macOS 硬配额、macOS/Windows 独立执行身份 | 后端代码尚未完成，当前明确拒绝 | 需各 OS 的可验证身份/容器与权限方案；创建系统身份、AppContainer 配置/ACL 等状态变更须先审查和批准 |
| Linux Claude/OpenCode 隔离流程 | 当前选用的 Ubuntu WSL 未发现对应受支持 CLI/ACP/Node 运行时 | 待提供版本与独立凭据配置；不自动作系统级安装 |

上述项目均未被视为已接受风险或正式发布通过。已完成项及实际命令见
[completed](completed.md) 与 [测试记录](host-hardening-tests.md)。


## Agent workflows acceptance / Agent 工作流待验收（2026-11-14）

Repository implementation and sibling NimoteCode integration are documented in [completed functionality](completed.md). Remaining acceptance is explicit:

- [ ] Physical phone/tablet over actual SSH, foreground/background, network interruption and reconnect. ADB device discovery returned an empty list during this delivery. / ADB 未发现设备；手机实际连接、后台和断网重连未验收。
- [ ] A user-approved real test Issue, installed/authenticated gh account on Host, and a real configured Agent completing development, tests and PR creation. No test Issue was supplied and gh was absent locally. / 尚无指定测试 Issue，本机没有 gh，不能将本地 Git/协议夹具称为真实闭环。
- [ ] Installed Codex, Claude Code/ACP and OpenCode versions: history query, continued conversation, real approvals and attributable edit events. CLI discovery and fixture tests alone do not prove upstream runtime support. / 三类真实 CLI 与模型交互矩阵另行验收。
- [ ] Native Windows/Linux runtime for all new workflow behavior; compiling another target is not native acceptance. / 新功能的跨平台原生运行未由编译替代。
- [ ] Cross-host aggregation, semantic duplicate-development detection, identity-isolated test execution scope, larger evidence retention and advanced fork/Enterprise publication remain outside this first version. / 跨主机、语义重复识别、独立测试身份、扩展保留范围及 fork/Enterprise 另行增强。
