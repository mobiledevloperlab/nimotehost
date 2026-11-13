# 更新记录

[English](CHANGELOG.md) | **简体中文**

## 尚未发布 — 1.0.0-dev

- 在已登录用户会话内实测 macOS 26.2/arm64 launchd 与 Windows build 26200/amd64
  Task Scheduler 的安装、重启、空闲升级和卸载。
- macOS 显式重启重新加载已保存的 LaunchAgent，避免正常关闭后 kickstart 卡住，保留原服务环境。
- Windows 计划任务 XML 改为 UTF-16LE+BOM；注册失败恢复或移除本地定义，使安装回退能够完成。
- 服务错误保留原生命令 stderr 诊断，不输出命令 stdout 中可能存在的无关环境信息。

- 新增源码一键安装及离线分发包安装、完整校验及事务式部署、旧包保留与失败回退，以及用于安全升级的原子空闲关闭接口。
- 简化为单终端部署；安装和重启验证就绪，重装保护活动任务，Unix 服务保存工具路径，并提供可操作的服务排障提示。
- 提供可部署归档，以及中英文安装、服务配置和运行验证指南。
- 从 Nimote Connect 拆出独立 Nimote Host。
- 提供认证回环 RPC、任务/输出/事件持久化、PTY/ConPTY，以及已安装的
  Codex、Claude Code、OpenCode 终端适配。
- 提供 Linux/macOS/Windows 当前用户服务集成，以及显式开启的 MCP 写工具。
- 提供带第三方许可声明和 SHA256 校验和的跨平台版本化构建。
- 提供中英文说明文档。

上述服务测试不覆盖操作系统重启或用户注销。完整移动设备/Agent 接管及统一结构化
审批仍待完成，当前版本为开发预览版。
