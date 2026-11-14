# Host hardening tests / 加固测试记录

English / 简体中文 · 2026-11-14 · 实际执行记录，不作绝对无 Bug 或正式发布认证。

## Environments / 环境

- macOS Darwin 25.2.0 arm64，Go 1.27.4：本机原生执行。
- Windows amd64：通过提供的 SSH 环境执行 Win32/ConPTY/Job Object 原生测试，
  Go 1.27.4，Strawberry GCC；Windows 11 专业工作站版 10.0.26200（Build 26200）。
- Linux amd64：同一测试主机的 RobotOS WSL2，Linux
  6.18.58.1-microsoft-standard-WSL2；Go 1.27.4、GCC。执行真实 Linux 系统调用，
  并非仅交叉编译；不等价于独立 Linux 主机的系统启动/服务验收。
- env.md 中 Linux SSH 认证未通过，未修改其账户、SSH 服务或密码；改用已授权
  Windows 主机内的 WSL。测试全部使用专用临时状态、端口与工作区。
- Windows 首次依赖下载网络超时，使用仓库锁定的 15 个公共 Go module 版本构造
  临时 file GOPROXY；保留 go.sum、校验流程和 go mod verify，没有关闭校验。
  临时官方 Go SDK 下载按官方 SHA256 清单校验。
- go.mod/go.sum 与生产依赖没有变更；最低 Go 1.26.5 未单独运行。

## Commands and results / 命令与实际结果

| Command / 执行项 | Actual result / 实际结果 |
| --- | --- |
| macOS `go test ./...`，最终 `go test -json ./...` | PASS；381 个通过的测试/子测试，5 个明确环境门控 skip，零失败；子测试计数不代表 381 个独立端到端场景 |
| macOS `make check` | PASS；5 个 Python 检查、go mod verify、`go test -mod=readonly -race ./...`、go vet，未报告 race |
| macOS `go test -race ./...`、`go vet ./...` | PASS；最终生产变更已由 make 的 readonly race/vet 完整回归，另执行直接命令通过 |
| Windows `go test -json ./...`、`go test -race ./...`、`go vet ./...`、`go run ./tools/build` | PASS；321 个测试/子测试通过，13 个平台/环境门控 skips，零失败；race/vet/原生分发构建 exit 0 |
| WSL Linux 同上四项 | PASS；382 个测试/子测试通过，6 个平台/环境门控 skips，零失败；race/vet/原生分发构建 exit 0 |
| Linux `NIMOTE_TEST_ROOT_ISOLATION=1 <host-test> -test.run=^TestRootIsolatedExecutionBoundary$ -test.v` | PASS，最终 0.56s；root-owned public helper 重入，专用 UID、内核 cgroup 设置与撤权清理实际验证 |
| Linux 同一 root-only 验收，用原生 `go test -race -c` 生成的测试二进制执行 | PASS，4.59s；身份隔离、并发撤权与 cgroup 清理未报告 race |
| Windows 撤权与 pinned-reader 原子替换回归，`-test.count=30` | PASS；每项连续 30 次；保持旧 reader 的完整旧快照，新路径发布完整新快照 |
| macOS supervisor/SIGKILL/job-control/版本探测，`-race -count=3` | PASS，30.934s；真实 SIGKILL 独立 Host fixture、任务及终端后代清理 |
| ACP v1 恢复、v2 生命周期/权限 subject/初始化、版本探测，`-race -count=3` | PASS；原生模型问题修复后新增回归 |
| GUI 模块 `go test -mod=readonly -race -tags ci ./...` | PASS，24.519s；Fyne headless，未修改 UI；既有 Apple duplicate -lobjc linker warning 未隐藏 |
| GUI 模块 `go vet ./...`、`go vet -tags ci ./...` | PASS |
| 六目标 `GOOS=... GOARCH=... CGO_ENABLED=0 go test -mod=readonly -c -o <temp> ./...` | PASS；darwin/linux/windows × amd64/arm64；仅说明这些额外目标的测试代码可编译 |
| 六目标 `go run ./tools/build -os ... -arch ...` | PASS；生成既有分发格式、清单和校验和 |
| `sh scripts/check-clean-build.sh` | PASS；全新 module/build 缓存、readonly race、vet 和原生分发构建均 exit 0 |
| `./scripts/export-source.sh`、Python 文档/源码导出测试、`git diff --check`、gofmt | PASS；源码包排除环境/凭据和本地任务文件；5 个 Python 检查、文档 Go 检查及格式检查通过 |

上述原生 Linux/Windows 检查运行的是本轮源码；不把上一提交或交叉编译的结果冒充最终原生结果。

## Live model acceptance / 真实 Agent 与模型

`TestLiveAgentApprovalDeclineAndConversationResume` 只在显式设置
NIMOTE_TEST_LIVE_AGENT 并提供独立 CLI/model 配置时执行；默认 CI 不装账号或调用模型。
真实模型验收在 macOS owner 会话中执行；隔离 UID 下的 Agent 配置另列待验收。所有模型调用仅用临时工作区、随机无敏感 marker、env.md 提供的 deepseek-flash。
密钥只进入子进程环境；日志脱敏，未提交配置、凭据、模型历史或 env.md。

| Provider / 实际版本 | Result / 结果 |
| --- | --- |
| Codex CLI 0.167.2，Responses API，read-only/on-request 配置 | PASS，最终 14.49s |
| OpenCode 1.19.5，独立 HOME/XDG，所有工具 ask | PASS，最终 8.35s |
| Claude Code 2.1.268 + @agentclientprotocol/claude-agent-acp 0.107.0，独立 CLAUDE_CONFIG_DIR，Write/Edit/Bash ask | PASS，最终 10.21s |

每个提供方都实际完成：请求批准写入 approved.txt，重复同一审批 HTTP request_id，
只产生一个原生答复；取消并恢复相同 conversation_id 后，在不读取旧文件的提示下
写入同一随机 marker；拒绝创建 denied.txt，确认文件不存在。批准/拒绝均至少经过
一个真实原生权限请求，不将自动允许当作审批测试成功。

测试 CLI 安装在临时工具目录，无全局安装或生产自动安装。旧 @zed-industries 0.23.1
只支持旧协议，被既有 major-version 校验拒绝；新包可选 native CLI 下载超时，验收显式
使用现有可运行的 Claude CLI，未依赖损坏的下载文件。临时 npm 重新链接曾中断 OpenCode
启动，恢复到同版本已验证的本地原生可执行文件后重跑通过。这些环境失败未被标为产品测试成功。

配置参考：[DeepSeek Codex](https://api-docs.deepseek.com/quick_start/agent_integrations/codex/)、
[DeepSeek Claude](https://api-docs.deepseek.com/quick_start/agent_integrations/claude_code/)、
[DeepSeek OpenCode](https://api-docs.deepseek.com/quick_start/agent_integrations/opencode/)。
ACP 恢复语义参考 [v1 session setup](https://agentclientprotocol.com/protocol/v1/session-setup)，
v2 的异步完成与 subject 参考 [migration](https://agentclientprotocol.com/protocol/v2/migration)。

## Regression coverage / 覆盖与异常场景

| Files / 用例 | Assertions / 关键断言 |
| --- | --- |
| hardening_devices_test.go、devices_test.go | 逐请求撤权、并发认证、范围过滤、跨会话拒绝、主体回执隔离、重启去重、最大范围/损坏记录拒绝；旧 observer 与 operator 权限保留 |
| execution_identity_linux_test.go | 真实独立 UID/GID、空附加组、owner 环境不继承、no_new_privs、Host token/进程权限拒绝、AF_UNIX broker 与 io_uring socket 绕过路径拒绝而 IP 可用；私有有限额委派、保护的 Host 可执行文件、内核 memory/swap/tasks 设置；两控制设备隔离、撤权后旧回执拒绝和 setsid 后代停止，另一设备不受影响；身份不可重新授权 |
| hardening_approvals_test.go、agent_execution_policy_test.go、acp_agent_test.go | 线程/轮次/Session 校验、单次决策、重复审批竞争、轮次结束后旧 accept 失效；ACP 恢复空对象/换 ID/null 拒绝、v2 插入 ACK 不结束工作、外国/未知状态、重复 idle、冲突/未知 permission subject 拒绝 |
| admission_test.go | 设备 8 读/2 写上限、并发 acquire/release 无残留、owner 独立保留容量、连接上限及关闭唤醒阻塞 Accept |
| hardening_resources_test.go、resource_quota_windows_test.go | 并发准入、退出释放槽位、磁盘/输出/运行时失败、无约束后备绕过；Win32 查询真实 CPU/memory/process limit，512 MiB VirtualAlloc 在无配额下成功、256 MiB Job 下失败 |
| supervisor_unix_test.go、hardening_process_unix_test.go、原生 PTY/ConPTY 测试 | Host SIGKILL、根进程退出、descendant heartbeat 停止、exit code、私有入口拒绝、交互 job control、断线重连；恢复为 interrupted，不用旧 PID 猜测归属 |
| recovery_test.go | 停机锁、陈旧计划拒绝、损坏 metadata/events 备份隔离、健康 Session 与 receipts/output/activity 保留、未知文件阻断、递增 reset 水位和断线 cursor 重同步 |
| storage_windows_test.go、config_test.go、migration_test.go、structured_windows_test.go | pinned reader 与原子发布、管理锁跨迁移/回滚保持、日志截断、原始迁移状态保留、结构化大 Unicode/多行输出完整分页及 stderr 隔离 |
| 既有 history、SDK、请求账本、输出/events、服务/安装与 desktop facade 回归 | 取消、超时、恢复、回执轮换/保留、界面层与 CLI daemon 生命周期；生产 UI 未改动 |

磁盘故障通过注入验证，不填满开发机磁盘；Linux aggregate cgroup 测试只创建/删除
专用子树，不改变系统全局控制器、账户或服务。CPU 控制器未向此测试层级委派，
没有运行 Linux CPU 限速负载试验。Linux 内存测试核对实际内核设置，未声称已运行
完整 OOM 压力矩阵；Windows 内存验收是真实 committed allocation 拒绝。

## Failures diagnosed and fixed / 失败根因及修复

1. Windows 空 cmd.Dir 被传为空字符串工作目录，CreateProcess 返回无效路径；改为 nil
   继承工作目录，Env=nil 按 exec.Cmd 语义继承环境。原生 Job/分配测试通过。
2. Windows 追加模式句柄缺少截断权限；RunLog 在自身互斥锁内 seek 后写入，保留容量断言。
3. Windows 目录迁移即使 share-delete 仍被子文件句柄阻断；管理锁移到同父目录并统一新旧名称，
   原始状态/回滚测试和持锁 rename 排斥测试通过。旧版本 manager 必须在升级前关闭。
4. Windows 读取/ACL 句柄与 MoveFileEx replacement 竞争；使用受保护的 share-delete reader、
   FileRenameInfoEx POSIX 原子发布及有界旧系统冲突处理。初版 native rename 缓冲缺终止 NUL，
   重复测试出现 ERROR_INVALID_NAME；保留 UTF-16 终止符并用不含 NUL 的长度后连续 30 次通过。
5. 原 Windows 大输出测试只读取第一页 64 KiB，末尾 completion 位于后页；测试现在沿 offset
   累积完整输出，原 Unicode、内容相等与 stderr 不混入断言均保留，未扩大生产分页上限。
6. macOS race re-exec supervisor 收尾延迟消耗版本探测预算；目标完成报告与 guardian 回收分离，
   唯一 reaper 始终运行且 Close 加入。保留原两秒目标预算与超时断言，未关闭 race 诊断。
7. 真实 OpenCode 暴露 ACP 恢复成功空响应；保留请求 ID 并拒绝返回另一 conversation。
8. 真实 Claude 暴露 ACP v2 初始化、prompt 插入 ACK、异步 state_update 与 permission subject
   差异；经共享语义适配后真实三阶段验收和新增 race 回归通过。
9. 最后审查发现事件日志水位也必须参与修复 reset 的最大值计算；新增时钟回拨/旧日志高水位回归，保证修复后的 cursor 单调前进。
10. 测试驱动曾误用 WSL 已清空的 /tmp 目录及未正确引用 PowerShell 的点号参数；修复驱动后
   重新执行，不把未启动测试计为通过。未以删除测试、skip 失败或降低断言隐藏问题。

## Explicit limits / 明确边界

macOS 默认回归的 5 个测试级 skips：Linux systemd parser、三个 installed-provider
握手 opt-in、真实模型 opt-in。真实模型由上表显式执行，不用默认 skip 替代。
Linux 的 6 个默认 skips：macOS 专用 zsh discovery、三个 installed-provider opt-in、
root-only 隔离 opt-in、真实模型 opt-in。root-only 另行显式执行并通过。

Windows 的 13 个 skips：两个 installer symlink/manifest_symlink 用例（当前账户缺少创建
symlink 的 developer privileges，未启用系统开发者模式）；仅 Unix 适用的 service 环境捕获、
systemd parser、zsh discovery、Unix command link、bash startup、旧 executable alias、
POSIX executable bits；两个 POSIX smart-command/ripgrep recognizer；POSIX 版本探测 fixture；
真实模型 opt-in。Windows 自身的 Job/ConPTY/协议 pipe、资源与撤权测试均实际执行。
这些 skips 保留现有平台条件，没有新增 skip 来避开失败。

尚未验收：独立 Linux OS 的运维生命周期、三平台登录/注销与系统重启、长时间饱和、
真实 sshd forced-command/SFTP/转发拒绝、所有 Agent 版本与模型、图形桌面/手机真机。
Linux 隔离 UID 下的真实 Agent/凭据配置及受限 syscall 兼容尚未验收；三提供方模型结果
来自 macOS owner 会话，不与 Linux terminal 的身份/资源隔离结果混淆。
没有 macOS 硬配额或 macOS/Windows 独立执行身份后端；无工作区磁盘硬配额。
默认 Unix 进程组无法覆盖主动逃逸后代；Linux cgroup 后端已覆盖该撤权用例。
不保证外部杀死 guardian、内核失败或断电后的事务完整；无独立安全审计结论。
详见 [剩余风险](pending.md) 和 [权限/部署/回滚](host-hardening.md)。
