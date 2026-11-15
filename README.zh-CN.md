# NimoteHost

[English](README.md) | **简体中文**

NimoteHost 是通过既有认证 SSH 使用的独立 Go 服务，可托管持久任务、终端和已安装的
编程 Agent CLI。Host 只监听本机回环地址，所有 RPC 请求均需认证，无需 Nimote 账号、
控制面、Tailcat 或公网中继。

当前是 **`2.4.0` 开发预览版**，提供 Linux、macOS、Windows 的 amd64/arm64 构建，以及 Linux/Windows 32 位 x86（386）CLI 构建。
完整原生平台和移动客户端/Agent 验收仍待完成，见[未完成功能](docs/pending.md)。

功能状态见[已完成功能](docs/completed.md)和[未完成功能](docs/pending.md)；
加固决策与验证见[加固审计](docs/host-hardening.md)和[测试记录](docs/host-hardening-tests.md)。

## Agent 上游监测

[兼容性监测工作流](.github/workflows/agent-compatibility.yml)每 6 小时运行一次
（UTC 00:23、06:23、12:23、18:23）。首次运行或上游版本变化时，临时安装指定版本的
Codex、OpenCode、Claude ACP 适配器和 Claude Code，检查真实结构化会话初始化、
运行 Host 现有 Agent/协议/审批回归测试，并导出 Codex JSON Schema；不发送 prompt、不调用模型。
同时监测 Claude Agent SDK、ACP SDK 和 ACP Go SDK 的最新发布，但不会自动升级项目依赖。

推送到 GitHub 私有库的默认分支后，开启 Actions 和 Issues，在 Actions 中手动运行
`Agent compatibility monitor` 建立基线，然后订阅自动创建的 Issue。
后续版本变化和检查结果会以评论通知；邮件由 GitHub 通知设置决定。
失败的同一组版本自动重试一次，之后可手动勾选 `force` 重跑。普通无变化轮询不安装 CLI 或运行测试。
工作流无需额外 secret，使用内置 `GITHUB_TOKEN`；仅报告任务有 Issues 写权限。

检查通过只代表 Linux 上的初始化和现有模拟协议回归通过，完整审批、流式响应及恢复仍需真实模型验收。
检查失败也可能来自网络、安装或认证，需查看日志；SDK 最新版仅作为变更信号，实际检查使用
适配器解析出的依赖及仓库锁定的 Go SDK。Schema 保存为 artifact，尚未自动判断语义差异。
日志和 Schema 保留 7 天。定时运行可能延迟；免费额度由账户共享，需在 GitHub Billing 中
配置 Actions 预算及超额停止，工作流本身无法保证零费用。删除 Issue 的隐藏状态会中断基线读取。

## CPU 架构

| 系统 | CLI | GUI |
| --- | --- | --- |
| Linux | amd64、arm64、386 | amd64、arm64 |
| macOS | amd64、arm64 | amd64、arm64 |
| Windows | amd64、arm64、386 | amd64、arm64 |

`amd64` 是 Intel/AMD 的 64 位 x86，`arm64` 是 64 位 ARM（包括 Apple Silicon）。
`386` 是传统 32 位 x86，仅提供 CLI；macOS 没有 32 位构建目标。
32 位 ARM 不在当前发行矩阵内。同一分发包中的 CLI 与 GUI 必须匹配目标架构。
GUI CI 配置六个具备桌面 SDK 的原生 runner；仅 CLI 交叉构建通过不代表 GUI 或交互已验收。
Windows ARM64 使用 MSYS2 CLANGARM64（`CC=clang`、`CXX=clang++`），不能复用 x64 MinGW 编译器，
参见 [MSYS2 ARM64](https://www.msys2.org/docs/arm64/)。

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
`dist/<os>-<arch>/nimotehost`；Windows 文件名为 `nimotehost.exe`。例如 Linux amd64：

```sh
./dist/linux-amd64/nimotehost start
```

指定其他目标平台和版本：

```sh
go run ./tools/build -os windows -arch arm64 -version 2.4.0
```

完整分发目录包含当前中英文功能说明、许可证、模块清单、构建信息和 SHA256SUMS。
构建还会生成带版本的 `.tar.gz`（Linux/macOS）或 `.zip`（Windows）以及配套 `.sha256`。也可使用 Bash 包装脚本：
`./scripts/build-host.sh <os> <arch> [version]`。分发二进制时请保留整个分发目录。

将二进制放到远端用户 PATH 中的固定位置后，执行 `nimotehost install` 安装当前用户的
后台服务并等待就绪，或用 `nimotehost start` 前台运行。Host 不会配置 SSH，也不会安装第三方 Agent。
客户端断开不会停止任务；Host/系统重启会将未完成会话标记为 interrupted，不会恢复其进程。

## Windows 便携运行与安装

CLI `nimotehost.exe` 可放在任意当前用户可访问的本地目录（包括空格和中文路径），
通过 PowerShell 执行 `& 'D:\你的目录\nimotehost.exe' start`。GUI 分发包需保持 GUI 与 CLI
位于同一目录；单独下载 CLI exe 不包含图形界面，双击无参数不会启动 Host。

Windows Host 成功启动时，会在 `%APPDATA%\nimotehost\discovery.json` 自动记录当前
CLI 的绝对路径。配套新版客户端通过同一账号的 SSH 读取该文件，优先查找运行位置，
再回退到 `%LOCALAPPDATA%\NimoteHost\nimotehost.exe` 和 SSH PATH。
无需手动修改 PATH 或重启 SSH 服务；移动目录后需从新位置重新启动 Host。
已停止的 Host 仍可被发现，但客户端不会因此自动启动服务。旧客户端需要升级才能读取发现记录。

需要固定安装位置与登录启动时，从完整分发包运行 `scripts\install.ps1` 或 `nimotehost setup`；
安装需要完整包，单个 exe 的便携运行不需要安装。客户端 SSH 账号必须与运行 Host 的 Windows 账号一致。
Windows CLI 架构为 x64/amd64、ARM64/arm64 和 32 位 x86/386；GUI 当前发行目标为 amd64/arm64，
GUI 构建需要对应系统的原生工具链。发现机制不依赖 CPU 架构。

## 接口与能力

- 已认证 SSH exec：`nimotehost rpc` 或 `rpc-encoded`。
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

也可使用 `make build` 和 `make check`。独立 CI 包含三平台原生检查和八目标 CLI 交叉构建。
测试不安装真实系统服务。运行时代码均位于 internal 包，外部通过版本化 RPC/MCP 接入。
模块路径为 `github.com/mobiledevloperlab/nimotehost`。

## 许可证

本独立 Host 源码树采用 [Apache-2.0](LICENSE)，版权署名为 2026 Nimote contributors。
源码从 `nimote-connect/host` 提取，Connect 基线提交为 `4701d14`。
第三方组件保留各自许可证，见[第三方声明](THIRD_PARTY_NOTICES.zh-CN.md)。许可证和上游
授权文本保留英文原文；中文说明不替代这些原文。

## Fyne 桌面管理器 / Desktop

命令和分发包统一命名为 `nimotehost`，GUI 显示 `NimoteHost`。
升级前先关闭旧 GUI，再从完整包运行 `nimotehost setup`。安装器通过已认证的空闲停止
迁移旧数据目录；有活动会话或新旧数据目录冲突时不会强制迁移。身份、凭证与记录保留，
安装失败时恢复旧服务。GUI 会一次性导入原语言和主题偏好。Unix 上旧 SSH 命令和
原绝对可执行路径会转到新 CLI，原二进制另存用于恢复。Windows 上手动配置的 SSH
强制命令绝对路径需改用 `nimotehost.exe`。旧拼写仅用于升级兼容。

CLI 与 GUI 复用现有 Go Host、认证本地 RPC 和三平台用户服务；Fyne 在
`cmd/nimotehost-gui` 独立 module 中，CLI 不引入图形依赖。

```sh
go run ./tools/build -gui -version 2.4.0
```

需在目标系统本机构建并安装 C 编译器/桌面 SDK。产物为
`nimotehost-gui-<版本>-<系统>-<架构>.tar.gz`（Windows 为 zip），包含 CLI、
GUI、完整许可证、GUI-MODULES.json 和校验清单；原 CLI 发行包继续单独生成。
从解压包运行原安装脚本，会把整个包复制到固定目录并启动用户服务：Unix 为
`~/.local/share/nimotehost`，Windows 为 `%LOCALAPPDATA%/NimoteHost`。
随后打开安装目录中的 Windows `nimotehost-gui.exe`、macOS `NimoteHost.app`、
Linux `nimotehost-gui`。Linux desktop 文件需 GUI 在 PATH，或把 Exec 改为绝对路径。
也可从 `dist/gui-<系统>-<架构>` 直接运行；启用登录启动后不要移动此目录。

界面保留 Overview、Sessions、Devices、Settings 四个主要入口。首屏显示服务状态和主要操作；
会话按名称、工作区和可读状态展示，支持搜索、筛选，历史对话位于 Sessions → Past conversations。
设置页按常规、本地服务和关于分组；本地服务中可直接启停 Host、编辑端口与日志级别，
采用银灰浅色与深色外观、统一圆角和间距；支持端口校验、未保存提示、撤销修改与固定保存栏。
切换界面语言会保留配置草稿和会话筛选。历史对话支持展开阅读；详情可选择、复制，诊断与日志使用大面积分页视图。
关于页说明与 NimoteCode 的分工、SSH 使用方式、本地数据、第三方 Agent 数据边界及外观。
快捷键：macOS 使用 ⌘，Windows/Linux 使用 Ctrl；1–4 切换页面、逗号打开设置、F 搜索会话、R 刷新状态。
诊断位于 Help & diagnostics；审批仍展示实际命令与文件修改。
Settings → Language 支持英语、简体中文、西班牙语、日语、韩语和俄语，默认英语；
切换语言立即生效并持久保存，不重启 Host。Fyne 使用系统字体，Linux 的中日韩显示需安装
相应字体，例如 `fonts-noto-cjk`。
配置复用 OS 用户配置目录下的 `nimotehost/config.json`，默认 `127.0.0.1:47831`，
修改端口和日志级别前需停止 Host。GUI 语言、主题与 Linux 托盘偏好由 Fyne 单独持久化。

Windows/macOS 关闭窗口隐藏到托盘；Linux 默认关闭窗口仅退出 GUI，确认有托盘支持
后可在设置启用。退出 GUI 不停止 Host、SSH 或后台任务；明确 Stop/Restart 才结束
Host 任务。完整控制仍依赖 SSH 系统账号；Devices 已支持独立只读设备身份和撤销，
不自动改写 SSH 密钥。SSH 由系统管理，诊断不猜测其健康状态。
详细审计、构建依赖和限制见[已完成](docs/completed.md#desktop-architecture-audit)，
原生平台与手机验收见[待完成](docs/pending.md)。

## Device authorization / 设备授权补充

GUI Devices 和 `nimotehost devices add <name>` 可注册独立只读设备，
`nimotehost devices list` 列出设备，`nimotehost devices revoke <id>` 撤销。
管理员须把独立 SSH 公钥绑定到 `restrict` + `device-bridge` 强制命令；
不能同时给这把密钥普通 Shell 登录权限。凭证不发送给手机，撤销针对后续 Host 请求生效。
完整 SSH/任务执行权限仍属于系统账号，不受只读设备撤销隔离。
Observer identities use restricted SSH keys, not unrestricted same-user shell access.
设备注册及绑定方法、安全边界见 [设备授权说明](docs/completed.md#只读设备授权历史与更新入口--observer-devices-history-and-updates)。
Sessions → Past conversations 已接入原生历史查询/恢复；Settings 可查询最新官方版本并打开下载入口。

## 社区插件

参阅[插件平台](docs/plugin-system/architecture.md)、
[公开 HCP 1.0 规范](docs/plugin-system/spec/v1/README.md)及
[SDK / HTTP API Inspector 开发安装指南](docs/plugin-system/plugin-sdk.md)。
插件默认禁用，需明确批准后启用；运行第三方程序前请阅读[安全边界](docs/plugin-system/security.md)。

同时支持本地离线包安装和 GitHub 目录搜索、下载及安装。开发者只需提交
[目录元数据](docs/plugin-system/catalog.md)，源码与发布包保留在自己的仓库中。
