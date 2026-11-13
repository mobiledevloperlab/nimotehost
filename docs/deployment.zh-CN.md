# 部署 Nimote Host

[English](deployment.md) | **简体中文**

推荐使用包内安装器：**校验 → 安装到固定用户目录 → 注册后台服务 → 确认就绪**，
均由一个命令完成。无需管理员权限、Nimote 账号或额外开放端口。

## 一键安装

预编译包用户无需 Go。先从可信渠道获得与系统/架构匹配的完整分发包，核对外部
SHA256 校验信息后解压。进入解压目录，以实际使用 SSH 的普通用户运行：

```sh
# Linux / macOS
sh scripts/install.sh
```

```powershell
# Windows PowerShell
.\scripts\install.ps1
```

也可以直接执行 `./nimote-host setup` 或 `.\nimote-host.exe setup`。安装器不下载远程
脚本、不执行 sudo、不更改 PowerShell 执行策略。若本机策略禁止脚本，直接使用
`nimote-host.exe setup`，遵循所在组织的执行策略。

源码用户在 **Host 模块根目录**执行同一命令；脚本会自动构建完整分发包，再调用
安装器，需要 Go 1.26.5 或更高版本，以及可用的模块缓存或依赖下载。所有源码命令均在独立 Nimote Host 仓库根目录执行。也可跨平台直接运行
`go run ./tools/install`，不依赖脚本执行策略或 Connect 仓库。

成功后会输出可供客户端填写的**可执行文件绝对路径**。填写既有 SSH 地址、端口、
账号及该路径即可。Host 不配置 SSH、不安装第三方 Agent，也不自动登录 Agent。
先确认客户端能够通过既有网络/VPN 使用该账号 SSH 登录这台机器。

默认安装位置：

| 系统 | 安装目录 | 后台服务 |
| --- | --- | --- |
| Linux | `~/.local/share/nimote-host` | 当前用户 `nimote-host.service` |
| macOS | `~/.local/share/nimote-host` | `com.nimote.host` LaunchAgent |
| Windows | `%LOCALAPPDATA%\NimoteHost` | 当前用户 `NimoteHost` 登录任务 |

使用安装器输出的绝对路径运行 `status` 和 `capabilities` 可随时检查状态。Unix 上使用
zsh 或 bash 的账户，setup 还会创建 `~/.local/bin/nimote-host`，并将该目录加入 SSH
非交互式命令使用的 shell 启动 PATH；不会覆盖已有同名命令。安装后重新建立 SSH 连接，
以刷新客户端缓存的环境快照。跨平台客户端应优先使用固定安装路径（Linux/macOS：
`$HOME/.local/share/nimote-host/nimote-host`；Windows：
`%LOCALAPPDATA%\NimoteHost\nimote-host.exe`），仅为自定义安装回退到 PATH 查找。

## 重复执行、升级与回退

- 已安装的完整包一致且服务正在运行时，重复执行直接返回就绪，不重启、不影响任务。
- 升级前完成整个新分发包校验及暂存，再请求正在运行的 Host **仅在空闲时关闭**。
  有活动进程时拒绝升级；空闲检查与关闭新任务入口是原子操作，避免中断任务。
- 首次升级尚不支持 `idle_shutdown` 的旧开发版本时，需要先等任务完成，用旧二进制
  执行一次 `stop`，再运行新安装器。已安装服务路径应指向上述默认目录；手工安装到
  其他目录的服务需先卸载服务定义，保留数据，再执行 setup。
- 完整目录通过同文件系统重命名切换，原包保存在相邻的 `.nimote-host-previous-*`
  目录。服务启动失败会尝试恢复旧包及原有运行状态，错误信息会说明恢复失败的原因。
  首次安装失败时移除新服务定义并保留凭据/记录；失败包保存在 `.nimote-host-failed-*`。
- 不自动降级或恢复私有状态。当前版本不迁移状态格式；未来存在状态迁移时，必须先
  遵循该版本的兼容性及备份要求。正常升级不恢复任务进程。

安装使用相邻的 `nimote-host.setup-lock` 目录防止并发。安装进程被强制终止时可能
留下锁、暂存目录或版本备份；先确认没有安装进程，再检查并清理。保留旧包可按
[维护指南](maintenance.zh-CN.md)手工回退，确认不再需要后可删除备份。

## 下载与校验

发布后可从 [Releases](https://github.com/mobiledevloperlab/nimote-host/releases) 获得版本包。
本文不表示已有正式发布；开发阶段使用完整本地构建包或维护者提供的 CI 包。

支持 Linux、macOS（`darwin`）、Windows 的 amd64/arm64；Windows 终端要求 Windows
10 1809+。文件名为 `nimote-host-<version>-<os>-<arch>.tar.gz`，Windows 为 `.zip`。
包内保留许可证、上游授权文本、模块清单、构建信息、双语文档及安装脚本。

Linux 示例（版本和架构以实际下载为准）：

```sh
bundle=nimote-host-1.0.0-dev-linux-amd64.tar.gz
sha256sum -c "$bundle.sha256"
# 仅在校验成功后解压；进入一个新建空目录解压该可信包
```

macOS 使用 `shasum -a 256 -c "$bundle.sha256"`。Windows 示例：

```powershell
$bundle = 'nimote-host-1.0.0-dev-windows-amd64.zip'
$expected = ((Get-Content "$bundle.sha256") -split '\s+')[0]
if ((Get-FileHash $bundle -Algorithm SHA256).Hash -ne $expected) {
    throw 'Archive checksum mismatch'
}
Expand-Archive -LiteralPath $bundle -DestinationPath '.\nimote-host-package'
```

校验和检测内容变化，不能证明发布者身份；须信任下载渠道及校验信息。当前包没有
代码签名或公证。安装器会再校验解压后的全部清单文件，拒绝缺失文件、额外文件、
符号链接及不安全路径；不会复制凭据、源码工作区或 Git 历史。

## 系统环境与排障

Linux 需要 systemd 用户服务管理器。若要退出登录后继续运行，按本机管理策略启用
lingering：先用 `loginctl show-user "$USER" -p Linger` 检查，再由管理员按需执行
`loginctl enable-linger "$USER"`。WSL 仅在发行版运行期间可用。macOS 需要用户 GUI
登录会话；Windows 任务以 LeastPrivilege 运行，需要保持登录，不是 LocalSystem 服务。

Linux/macOS 安装保存当前 PATH 的绝对路径项及 XDG_CONFIG_HOME，不复制其他变量和
凭据。请从日常用户终端安装，避免临时工具目录。路径变化后，等待任务完成，执行
`stop` 再执行固定安装路径下的 `install` 刷新；`restart` 不重新捕获环境。
Windows 使用登录环境。第三方 Agent 必须以同一用户预先安装、登录。

检查命令（将 `nimote-host` 替换为安装器输出的绝对路径）：

```sh
nimote-host status
nimote-host capabilities
# Linux
systemctl --user status nimote-host.service
journalctl --user -u nimote-host.service -n 50 --no-pager
# macOS
launchctl print "gui/$(id -u)/com.nimote.host"
```

Windows 使用 `schtasks.exe /Query /TN NimoteHost /V /FO LIST`。Linux 短时间多次启动可能
触发 systemd 的启动限流；排除日志中的失败原因后，执行
`systemctl --user reset-failed nimote-host.service` 再重试。

Windows 与 WSL 镜像网络切换时，共享回环端口可能在另一平台停止后继续短暂被占用。
若未列出监听进程但绑定仍失败，先停止另一平台 Host，等待端口释放后再重试；两平台
不要同时占用该端口。Linux 若已触发启动限流，还需按上文清除失败状态。

服务管理器不可用时，可选择 `start` 前台运行；不要同时启动两个实例。停止、重启及
卸载会终止活动任务。不要删除运行锁或按旧 PID 杀进程，不要公开暴露 47831 端口。
Linux 自定义服务环境建议使用 `systemctl --user edit nimote-host.service`，重装保留
这些 drop-in；直接修改生成的主服务定义会被覆盖。

## 验证客户端与卸载

从另一台设备复用相同账号、SSH 端口及实际地址，例如：

```sh
ssh -p 2222 user@host '"$HOME/.local/share/nimote-host/nimote-host" capabilities'
```

核对 Host ID、协议和工具路径。token 由远端 CLI 本地读取，不复制到手机设置。
Host 部署不增加移动端 UI，客户端仍需实现 [RPC/MCP 协议](host.md)；实际依赖前验证
断开与重连。

卸载时使用固定安装路径下的 `uninstall`，再按需删除安装目录和版本备份。凭据、
身份、任务记录和输出保留在 `os.UserConfigDir()/nimote-host`；删除状态必须是单独的
明确操作。普通升级和卸载不删除这些数据。
