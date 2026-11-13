# Nimote Host

[English](README.md) | **简体中文**

Nimote Host 是通过既有认证 SSH 使用的独立 Go 服务，可托管持久任务、终端和已安装的
编程 Agent CLI。Host 只监听本机回环地址，所有 RPC 请求均需认证，无需 Nimote 账号、
控制面、Tailcat 或公网中继。

当前是 **`1.0.0-dev` 开发预览版**，提供 Linux、macOS、Windows 的 amd64/arm64 构建。
完整原生平台和移动客户端/Agent 验收仍待完成，见[未完成功能](docs/pending.md)。

当前文档仅保留[已完成功能](docs/completed.md)和[未完成功能](docs/pending.md)。

## 部署预编译版本

普通用户无需安装 Go。按照[已实现的安装与服务说明](docs/completed.md#安装与服务--installation)校验完整分发包、
复制到固定目录、配置当前用户后台服务，再通过 SSH 验证访问。

## 一键安装

在 Host 源码根目录（需 Go 1.26.5+）或已解压分发包内（无需 Go）执行：

```sh
sh scripts/install.sh
```

也可跨平台直接运行 `go run ./tools/install`（源码安装）。Windows PowerShell 使用 `.\scripts\install.ps1`。安装器校验完整包、复制到固定用户
目录、注册后台服务并确认就绪；重复执行不重启正常服务。升级保留旧包，并拒绝
中断活动任务。系统前置条件、旧预览版升级及恢复见[安装与服务说明](docs/completed.md#安装与服务--installation)。

## 构建与运行

安装 Go 1.26.5 或更高版本，克隆本仓库后，在仓库根目录执行：

```sh
go run ./tools/build
```

该命令可在 PowerShell 和 Unix Shell 中运行。生成的二进制位于
`dist/<os>-<arch>/nimote-host`；Windows 文件名为 `nimote-host.exe`。例如 Linux amd64：

```sh
./dist/linux-amd64/nimote-host start
```

指定其他目标平台和版本：

```sh
go run ./tools/build -os windows -arch arm64 -version 1.0.0-dev
```

完整分发目录包含当前中英文功能说明、许可证、模块清单、构建信息和 SHA256SUMS。
构建还会生成带版本的 `.tar.gz`（Linux/macOS）或 `.zip`（Windows）以及配套 `.sha256`。也可使用 Bash 包装脚本：
`./scripts/build-host.sh <os> <arch> [version]`。分发二进制时请保留整个分发目录。

将二进制放到远端用户 PATH 中的固定位置后，执行 `nimote-host install` 安装当前用户的
后台服务并等待就绪，或用 `nimote-host start` 前台运行。Host 不会配置 SSH，也不会安装第三方 Agent。
客户端断开不会停止任务；Host/系统重启会将未完成会话标记为 interrupted，不会恢复其进程。

## 接口与能力

- 已认证 SSH exec：`nimote-host rpc` 或 `rpc-encoded`。
- 本地认证 HTTP RPC：任务/会话管理、终端输入与尺寸调整、按字节偏移恢复输出、持久化事件。
- stdio MCP：默认只读，写工具必须由本地操作者显式开启。
- Agent 终端适配：已安装的 Codex、Claude Code 和 OpenCode CLI；已有 app-server/ACP 结构化审批与原生历史，运行时能力和完整实机验收分别判定。

详细说明：[已完成功能与协议](docs/completed.md)、[未完成功能与验收](docs/pending.md)、
[贡献指南](CONTRIBUTING.zh-CN.md)、[安全政策](SECURITY.zh-CN.md)。

## 开发

```sh
go test -mod=readonly -race ./...
go vet ./...
```

也可使用 `make build` 和 `make check`。独立 CI 包含三平台原生检查和六目标交叉构建。
测试不安装真实系统服务。运行时代码均位于 internal 包，外部通过版本化 RPC/MCP 接入。
模块路径为 `github.com/mobiledevloperlab/nimote-host`。

## 许可证

本独立 Host 源码树采用 [Apache-2.0](LICENSE)，版权署名为 2026 Nimote contributors。
源码从 `nimote-connect/host` 提取，Connect 基线提交为 `4701d14`。
第三方组件保留各自许可证，见[第三方声明](THIRD_PARTY_NOTICES.zh-CN.md)。许可证和上游
授权文本保留英文原文；中文说明不替代这些原文。

## Fyne 桌面管理器 / Desktop

CLI 与 GUI 复用现有 Go Host、认证本地 RPC 和三平台用户服务；Fyne 在
`cmd/nimote-host-gui` 独立 module 中，CLI 不引入图形依赖。

```sh
go run ./tools/build -gui -version 1.0.0-dev
```

需在目标系统本机构建并安装 C 编译器/桌面 SDK。产物为
`nimote-host-gui-<版本>-<系统>-<架构>.tar.gz`（Windows 为 zip），包含 CLI、
GUI、完整许可证、GUI-MODULES.json 和校验清单；原 CLI 发行包继续单独生成。
从解压包运行原安装脚本，会把整个包复制到固定目录并启动用户服务：Unix 为
`~/.local/share/nimote-host`，Windows 为 `%LOCALAPPDATA%/NimoteHost`。
随后打开安装目录中的 Windows `nimote-host-gui.exe`、macOS `Nimote Host.app`、
Linux `nimote-host-gui`。Linux desktop 文件需 GUI 在 PATH，或把 Exec 改为绝对路径。
也可从 `dist/gui-<系统>-<架构>` 直接运行；启用登录启动后不要移动此目录。

首屏管理 Host 启停、重启；Sessions 展示真实会话及结构化 Agent 状态/待审批；
Settings 提供登录启动、端口、日志级别和 System/Light/Dark；Diagnostics 提供
健康信息、64 KiB 有界运行日志、诊断复制及导出。配置复用 OS 用户配置目录下
`nimote-host/config.json`，默认 `127.0.0.1:47831`，修改前需停止 Host。
GUI 主题与 Linux 托盘偏好由 Fyne 单独持久化。

Windows/macOS 关闭窗口隐藏到托盘；Linux 默认关闭窗口仅退出 GUI，确认有托盘支持
后可在设置启用。退出 GUI 不停止 Host、SSH 或后台任务；明确 Stop/Restart 才结束
Host 任务。设备认证继续依赖 SSH 系统账号，当前无逐设备身份/撤销接口，GUI 不伪造
设备列表，也不改写 SSH 密钥。SSH 由系统管理，诊断不猜测其健康状态。
详细审计、构建依赖和限制见[已完成](docs/completed.md#desktop-architecture-audit)，
原生平台与手机验收见[待完成](docs/pending.md)。
