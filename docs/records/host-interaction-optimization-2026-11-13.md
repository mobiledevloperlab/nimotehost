# Host 客户端交互延迟优化

日期：2026-11-13。客户端为相邻 `nimotecode` 仓库的本地工作区，基线 `5b28c38e`。
本次修复客户端没有启用既有 `rpc_stream` 能力的问题，不需要修改或部署 Go Host。

## 原因与改动

原客户端每次输入、resize、会话列表和输出读取均执行一次 `nimote-host rpc-encoded`，
输出依赖一秒一次的列表刷新。即使 Host 已支持复用连接，客户端仍承担重复启动远端进程
和等待下一次轮询的开销。

现在先以原有方式探测可选 Host；仅在 Host 声明 `rpc_stream` 时尝试建立持久化 SSH
exec 通道。该通道不分配 PTY，以 JSONL 请求 ID 匹配并发响应，复用原生有界 I/O worker。
活动会话通过 `wait_ms=1000` 独立读取输出，服务端有输出立即唤醒；输入和列表请求可以
并行进行。每批输出之间给 Flutter 留出一帧时间，未变化的列表不重建整页。

这次仅改变手机与 Host 的通信。Host 与 Agent CLI 仍使用 PTY/ConPTY；OpenCode HTTP
接口、Codex App Server 和 Claude SDK 等结构化 Agent 适配尚未在本次改动中实现。
不能把新 RPC 通道称为结构化 Agent 对话或结构化审批。

## 原功能兼容边界

- 未安装、未启动或协议不兼容的 Host 仍只影响 Host 面板。普通 SSH、SFTP、终端启动
  不增加 Host 探测或安装步骤，原操作方法保持原路径。
- 旧 Host 没有 `rpc_stream` 时继续使用单次 RPC。新通道协商失败也可在派发业务请求
  之前回退；已派发输入、创建、取消或其他操作在断线/超时后不自动重放。
- 新增 `ssh_host_rpc_open` 使用独立认证连接和固定的 `nimote-host rpc-stream` argv。
  普通终端构造仍默认分配 PTY；没有替换普通终端或 SFTP 的传输方式。
- 隐藏 Host 面板、进入后台和销毁面板仅关闭该面板拥有的桥接通道，不调用全局 SSH
  断开，不取消后台任务。帧间等待、未派发输入和迟到的通道分配均有清理。
- 长连接读写检查原生连接 scope；切换主机或重连同一主机后，旧通道不能继续派发输入
  或把旧输出送入新会话。切换会话和暂停输出同样检查选择代次。

主要客户端文件：`lib/host/host_client.dart`、`host_rpc_stream.dart`、
`host_rpc_channel.dart`、`host_panel.dart`，以及 `ScopedSshSession.checkCurrent`。
原生入口在 `rust/src/api/ssh_and_terminal.rs`，非 PTY 通道在 `ssh_shell.rs`。
Rust/Dart/C 桥接声明已由项目固定版本的生成器更新，发布时必须与原生库一起重新编译。

## 自动验证

最终在最新 Host 界面工作区上合并运行 `test/host`、
`test/services/scoped_ssh_session_test.dart` 和 `test/services/scoped_ssh_native_test.dart`，
启用匹配原生库及本地协议对端，共 113 项全部通过。

- Host 客户端测试：107 项通过。新增覆盖 JSONL 分片、乱序响应、输入/输出并发、
  超时不重放、缺失/旧 Host、协商回退、迟到分配清理、scope 隔离和实时输出。
- 原 SSH、SFTP、连接恢复、分块读文件和终端测试：808 项通过。最初按配置跳过的
  原生桥接检查在生成匹配动态库后单独执行通过；连接 scope 的 Dart 检查追加 4 项通过。
- Rust 终端模块原有单元测试：16 项通过。
- 本地 Paramiko SSH/SFTP 对端测试 `host_rpc_detach_preserves_ssh_sftp_and_terminal`：
  通过。确认只有普通终端请求 PTY，RPC 收发 JSON 不回显；关闭 RPC 后同一普通终端
  仍能交互，原 SSH 命令及 SFTP 文件读取正常，错误 scope 被拒绝。
- 原有本地 SSH/SFTP 对端测试 `live_scope_rejects_old_host_and_same_host_reconnect`：
  通过，覆盖切换主机、同主机重连及原命令/文件访问的 scope 隔离。
- 匹配原生库的 Flutter 桥接测试：2 项通过。包含无连接时原 scoped SSH 与新增 Host
  入口失败关闭，以及 Flutter → Rust 事件流 → 本地 SSH 的 JSONL 往返。关闭 Host
  桥接后，原 SSH 命令和 SFTP 仍可使用，未分配 PTY。
- 原生库构建：通过。首次默认 dev 构建因磁盘不足失败，只清理该次新产生的编译缓存，
  使用 `CARGO_PROFILE_DEV_DEBUG=0 CARGO_INCREMENTAL=0 cargo build --manifest-path
  rust/Cargo.toml --lib` 重试成功，没有改变应用构建配置或既有安装。
- Host 与相关测试的定向静态分析：无问题。
- Host 工作流测试适配工作区现有界面：图标按钮使用 tooltip 定位，快捷创建在空列表
  中验收，已有历史通过新建入口进入表单；没有为通过测试修改产品界面行为。

本地协议对端不代表真实 Agent 或物理手机测试。本次没有覆盖已安装应用，也没有发布
APK/IPA；需要重新编译客户端后在实际网络上确认延迟、帧率和跨设备体验。未给出未经
测量的百分比或毫秒级性能提升结论。
