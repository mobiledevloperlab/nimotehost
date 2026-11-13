# 未完成功能 / Pending functionality

English / 简体中文 · `1.0.0-dev` · 更新 / Updated: 2026-11-13

当前源码已实现的功能、使用方式和协议只维护在[已完成功能](completed.md)。
本页只列未完成的功能与验收，不重复历史过程，不把已有代码再次列为缺失。
Implemented behavior lives in [completed functionality](completed.md). Items below remain open.

## 功能 / Features

- [ ] 统一 AI Session Dashboard：在现有 AI 模式按主机/工作区聚合会话、搜索与状态筛选；
  保存带观察时间的离线摘要，不将缓存伪装成实时状态。
  Unified AI-mode dashboard and timestamped cross-host offline summaries.
- [ ] 逐设备主体和会话权限：当前为 SSH OS 用户权限域，尚无同账号内按手机撤销、
  会话 ACL 或细粒度设备权限；不能信任客户端自报 device_id 代替认证。
  Per-device identity, revocation and session authorization within an SSH account.
- [ ] 请求账本长期轮换：当前达到数量/空间限额前拒绝新操作，无在线轮换或安全淘汰协议；
  删除会话仍保留回执，避免延迟请求重建任务。
  Safe long-term idempotency-ledger rotation without enabling delayed replay.
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
