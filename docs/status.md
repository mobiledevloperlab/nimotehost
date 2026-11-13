# Nimote Host 状态与历史验收

Host 已拆为独立仓库。以下验收结果保留原日期与范围，不表示所有平台已完成发布验收。

## 独立 Nimote Host（当前测试入口）

- 目录：独立仓库根目录；程序：`nimote-host`；版本：`1.0.0-dev`。
- 通过既有认证 SSH 使用，固定监听本机回环地址，RPC 均需 token。无需 Nimote 账号、
  Firebase、Tailcat 或公网中继。
- 支持任务、交互终端、已安装 Agent CLI 的终端入口、持久化记录/输出/事件及 stdio MCP。
- 客户端断开不终止任务；Host/系统重启不恢复活进程。统一结构化 Agent 审批尚未实现。
- 独立开源目录包含 Apache-2.0、中英文文档、三平台服务代码、六目标构建、CI 和发布归档。
- 历史本地证据包括双模块测试、Host race/vet、六目标交叉构建、源码包独立构建及 Linux
  归档前台启动/查询/停止。交叉构建不代表 macOS/Windows 原生服务验收已通过。
- NimoteCode 的 master 未包含专用 Host 入口；`nimote-host` 分支包含适配。2026-11-13
  已在该分支构建独立测试 APK，经 Android ADB 对接 macOS Host，并修复页面渲染与
  非交互会话 resize 问题。修复保留在客户端专用分支，尚未合入 master，详见
  [本次实机测试记录](records/host-adb-validation-2026-11-13.md)。
- 同日复用手机保存记录完成 Windows / WSL2 核心与手机面板补测，覆盖任务断线继续、
  输出恢复、终端输入/取消；修复 Windows 结构化执行、盘符目录、ConPTY 输入及任务换行。
  客户端补充提交 `c5d78641e`，56 项测试通过；该轮未覆盖服务生命周期，详见
  [Windows / WSL2 补测](records/host-win-wsl-adb-validation-2026-11-13.md)。

- 随后的原生服务安装补测已通过 macOS 26.2/arm64、Windows build 26200/amd64 的
  安装、重启、停止、升级、终端和卸载，并完成测试安装清理、原 WSL 服务恢复。
  本轮未注销用户或重启系统，详见[原生服务验收记录](records/host-service-install-validation-2026-11-13.md)。
- 真实 OpenCode 1.18.30 + `deepseek-flash` 已完成 Mac → Android 手机 → Mac
  同进程、同对话接管，验证实际改码、3 项测试、原生权限拒绝与允许、手机重连及对话记忆。
  测试安装与隔离数据已清理，原服务恢复。Codex/Claude Code 尚未实测，详见
  [真实 Agent 接管记录](records/host-agent-handoff-validation-2026-11-13.md)。

开始测试：[中文逐步教程](host-testing.zh-CN.md)。部署、协议、维护见[文档索引](README.md)。
仍需完成的客户端与实机工作见[Host 剩余任务](planning/host-1.0-remaining.md)。

