# 真实 Agent 跨设备接管验收

日期：2026-11-13。Host 基线提交 `dc48a66`。使用 `env.md` 指定的真实机器与
模型服务；认证信息仅在内存中读取，不进入提交或证据。本轮未修改运行时代码。

## 环境与边界

- Host：原有 Linux/WSL2 部署，保留原身份与既有会话。
- 客户端 A：独立物理 Mac，经其原生 SSH 调用 Host RPC。
- 客户端 B：一台物理 Android 手机，通过 NimoteCode 的 Tasks → Host sessions
  面板附着、查看输出和输入。ADB 仅操作实际手机界面；两个 ADB 地址不计作两台设备。
- Agent：机器已有的真实 OpenCode 1.18.30，调用真实 DeepSeek API 的
  `deepseek-flash`；没有模型或 Agent 模拟器，没有安装、升级 Agent CLI。
- 手机使用已有隔离 QA APK，版本 `2.3.0+63`，SHA256 见证据。
  客户端源码检出参考为 `8344e5b73`；本轮未重新构建 APK，不据此声明二者完全对应。
- 为隔离模型配置、数据库、缓存和工作区，暂时停止原用户服务，使用同一已安装 Host
  二进制、原状态和身份前台运行。临时启动器最终 exec 原 OpenCode；模型密钥仅传入
  子进程环境。接管全过程未重启 Host 或 Agent，结束后恢复原 systemd 用户服务。

## 实际执行结果

| 步骤 | 操作与证据 | 结果 |
| --- | --- | --- |
| Mac 发起真实编码任务 | 夹具的加法函数故意写成减法，先独立确认测试失败；首轮提示提供随机挑战码，要求不写入项目或命令 | PASS |
| Agent 修改代码 | 真实工具调用将减法修为加法，执行 3 项 unittest，写入 `DESKTOP_PHASE_DONE` | PASS |
| 手机断开与重新附着 | 强制停止并重开手机 App，原 Host 会话及 Agent 仍运行，恢复终端输出 | PASS |
| 手机继续同一对话 | 通过实际手机输入后续提示，不重复提供挑战码；Agent 从原对话正确回忆并写入第二行 | PASS |
| 手机拒绝原生权限请求 | 在终端菜单选择 Reject；数据库记录该次工具调用为用户拒绝错误，没有执行该次命令 | PASS |
| Mac 接回 | 手机退出后，Mac 在同一会话发起第三轮提示；对新的测试命令请求选择 Allow once，实际输出 3 项通过 | PASS |
| 完成与独立核验 | Agent 写入 `DESKTOP_RETAKE_DONE`；独立重跑夹具测试，3/3 通过 | PASS |

三个用户轮次属于同一 OpenCode 会话，Agent 进程在手机与 Mac 接管前后保持一致。
记录包含 13 条 assistant 消息、12 次完成的工具调用、1 次被拒绝的调用；Host 保留
320326 字节输出，无截断。挑战码只在首轮用户提示中提供，手机和 Mac 的续接提示均
未包含它；项目文件最终为三行阶段标记，其中第二行包含正确挑战码。

初始手机配置和早期审批存在其他操作输入，未将其全部归因于 ADB；明确审计的动作是
后续手机提示、手机 Reject 和 Mac 接回的 Allow once。不能据此声称所有早期审批
均由单一自动化输入完成。

## 清理核验

- 已取消并删除本轮 Agent 的 Host 会话及输出，保留原有会话。
- 已停止临时 Host，恢复原 systemd 用户服务的运行状态、原身份和原 Agent 启动路径。
- 已卸载本轮安装的隔离手机 QA App 及数据，保留原有两个 NimoteCode 应用。
- 已删除 Mac 临时 SSH 文件、本机控制脚本、截图、临时工作区及隔离 Agent 配置、
  数据库、缓存；原 OpenCode 二进制 SHA256 未变。
- Host 原有审计事件按现有保留策略保存，没有改写用户审计历史。

[脱敏结构化证据](../audits/evidence/host-agent-handoff-2026-11-13.json)仅保留结果、
计数及摘要，不保存地址、账号、凭据、设备标识、原始终端或对话数据库。

## 尚未覆盖

本轮只验收 OpenCode，未验收 Codex、Claude Code、双手机或 iOS。Mac 使用 SSH RPC，
未测试 Mac GUI 客户端。验证的是运行中会话接管，不是 Host/OS 重启后的活进程恢复。
原生终端审批通过，统一结构化审批仍未实现，`agent_structured_approval=false`。
完整客户端兼容性与发布门禁继续见 [Host 剩余任务](../planning/host-1.0-remaining.md)。
