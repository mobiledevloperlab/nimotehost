# Host 原生服务安装、升级与卸载验收

日期：2026-11-13。基线提交 `e42ce2d`，包含本轮发现问题的工作树修复。
本轮范围为此前未完成的 macOS/Windows 原生后台服务安装验收；不是完整 1.0 发布签署。
认证信息仅从本地环境文件读取，不写入脚本、命令参数、证据或报告。

## 环境与执行方式

- macOS 26.2 / arm64，已登录 GUI 用户；使用真实 launchd LaunchAgent。
- Windows NT 10.0.26200.0 / amd64，已有控制台登录用户；使用真实当前用户 Task Scheduler，
  `InteractiveToken`、`LeastPrivilege`、无运行时限。
- Linux/WSL2 原有 Host 是用户此前明确部署的服务，不作为可删除的测试安装。
  Windows 测试期间暂停它，结束后恢复运行、启用状态和原 Host 身份，保留既有记录。
- 使用完整默认分发包及 `1.0.0-dev.native-qa` 升级包，经 SSH 上传、核对外部 SHA256，
  再由安装器校验包内清单。macOS 使用包内 Shell 入口，Windows 直接运行二进制 setup。
- 未安装 Go、Agent CLI 或其他第三方工具。macOS 原生测试通过交叉构建的测试二进制
  在真实 macOS 执行；测试源码仅作为只读夹具上传，完成后一起移除。

## 原生服务结果

| 用例 | macOS | Windows |
| --- | --- | --- |
| 完整包校验及一键安装 | PASS | PASS（修复 XML 编码后） |
| 真实后台服务注册与启动 | PASS | PASS |
| 安装 SSH 通道退出后仍可新建 SSH 查询 | PASS | PASS |
| 状态权限 / 保护 DACL | 目录 0700、token/服务定义 0600 | 保护 DACL，仅当前用户规则 |
| 未认证 HTTP 返回 401 | PASS | PASS |
| 普通任务、输出及中文/空格工作目录 | PASS | PASS |
| 相同包 setup 不重启正在运行的任务 | PASS | PASS |
| 升级包在活动终端存在时拒绝操作 | PASS | PASS |
| 服务内 PTY/ConPTY 输入、resize、正常退出 | PASS | PASS |
| Host 服务重启、同一身份及任务输出持久化 | PASS（修复重启路径后） | PASS |
| 空闲升级、服务就绪与身份保留 | PASS | PASS |
| stop 后不可用，再 restart 恢复 | PASS | PASS |
| uninstall 移除注册、保留状态 | PASS | PASS |
| 最终删除本轮状态、安装目录、临时包及备份 | PASS | PASS |

macOS 五个包的原生测试通过：`cmd/nimote-host`、`internal/host`、
`internal/hostinstall`、`internal/hostservice`、`tools/build`。两平台额外执行了最新服务
回归，包括任务 XML 编码、注册失败恢复、macOS 重载顺序及 stderr/退出码保留。
Linux 执行 `go test -mod=readonly -race ./...` 与 `go vet ./...`。

## 发现的问题及修复

1. macOS 在认证正常关闭后执行 `launchctl kickstart -k`，作业停留在
   `spawn scheduled`，重启超过 45 秒仍未恢复。显式重启改为 bootout/bootstrap
   已保存的定义；不重新捕获或重写环境。真实重启、停止后再启动及升级复测通过。
2. Windows `schtasks /Create /XML` 拒绝带 UTF-8 声明的定义，报告无法切换编码。
   改为 UTF-16LE、匹配的 XML 声明及 BOM；增加非 ASCII 与补充平面字符回归。
3. Windows 注册失败仍留下 `service.xml`，导致安装器误判已安装并尝试删除不存在的
   原生任务，阻碍回退。注册失败现在恢复旧定义；首次失败则移除新标记。
4. 原生命令错误此前只显示 exit status，掩盖具体原因。错误中补充有界 stderr，
   保留退出码，不输出 stdout 中可能存在的环境信息；新增回归验证。

Windows/WSL 镜像网络还出现共享回环端口回收延迟：另一平台关闭后，端口可暂时
无法独占绑定，即使没有列出监听进程。通过实际绑定探测等待端口释放后，Windows
完整用例通过；反向恢复 WSL 时也等待释放并清除本轮触发的 systemd 启动限流。
没有修改 SSH、防火墙、网络配置或默认端口，此现象补入部署排障文档。

## 清理核对

- macOS：LaunchAgent 已卸载；安装、状态/凭据、升级备份、暂存/锁目录及全部上传
  测试包/源码/测试二进制均移除；核对无 Host 进程和测试临时目录。
- Windows：计划任务已删除；安装、状态/凭据、升级备份、失败包、锁及测试临时目录
  均移除；对失败包先核对本轮构建 SHA256 再删除，核对无 Host 进程。
- 本机：临时 SSH askpass/known_hosts、测试协调脚本、交叉测试二进制及 native-qa
  构建产物在收尾删除；标准分发产物和源码保留。原 WSL Host 已恢复运行。
- 清理只针对本轮创建的安装和测试文件，不删除原有服务、应用、开发工具或用户状态。

[脱敏结构化证据](../audits/evidence/host-service-install-2026-11-13.json)保存逐项结果及
修复补丁摘要，不含地址、账号、密码、SID、Host token 或模型密钥。

## 未覆盖边界

没有注销用户或重启操作系统；登录触发/用户运行身份通过真实服务定义核对，不能
等同于实际重新登录或开机验收。没有重新运行移动端全功能回归、真实模型对话、
跨设备 Agent 接管或统一结构化审批。后续发布门禁仍见
[Host 剩余任务](../planning/host-1.0-remaining.md)。
