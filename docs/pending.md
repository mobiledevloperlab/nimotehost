# 未完成功能 / Pending functionality

English / 简体中文 · `1.0.0-dev` · 更新 / Updated: 2026-11-13

当前源码已实现的功能、使用方式和协议只维护在[已完成功能](completed.md)。
本页只列未完成的功能与验收，不重复历史过程，不把已有代码再次列为缺失。
Implemented behavior lives in [completed functionality](completed.md). Items below remain open.

## 功能 / Features

- [ ] 统一 AI Session Dashboard：在现有 AI 模式按主机/工作区聚合会话、搜索与状态筛选；
  保存带观察时间的离线摘要，不将缓存伪装成实时状态。
  Unified AI-mode dashboard and timestamped cross-host offline summaries.
- [ ] 完整控制设备隔离及会话 ACL：已实现独立只读设备身份和逐请求撤销；
  拥有同一 OS 用户任意命令执行权的客户端不在该隔离范围内。完整控制需独立系统身份或沙箱，
  当前仍无每设备可执行任务或每会话写权限策略。Authenticated observer revocation is implemented;
  execution-capable device isolation and per-session ACL remain open.
- [ ] 旧格式请求键迁移及容量运维：七天有效期的新格式请求键已支持安全在线轮换和容量告警；
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
