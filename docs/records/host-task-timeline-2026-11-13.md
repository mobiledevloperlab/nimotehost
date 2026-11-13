# Host Task / Timeline 关联与前台事件通知续作

日期：2026-11-13。继续[首轮审计](../audits/cross-device-agent-workflow-2026-11-13.md)，
保留两仓库原有修改，未提交、安装或部署。Host 继续为 Go，客户端沿用 Flutter / Rust。

## 交付行为

1. 从现有 SSH Task 菜单选择 Sessions，在现有 Host 界面按 Task ID 与当前工作区
   目录筛选。选择已有记录直接进入原 Session，不新建/重跑命令。
2. 新建会话默认提供 Agent 表单，用户仍需明确提交。Task 引用跟随创建请求及
   显式原生对话恢复，持久保存在 Host Session 元数据。会话标题展示 Agent 与 Task
   名称，Task 配置文件及原有 Tasks 命令执行器不变。
3. Host 提供按会话单调序号保存的生命周期摘要；会话菜单的 Timeline sheet 使用
   现有 sheet/list 组件展示与刷新，不再复制提示、终端内容或工具 diff。
4. 新 Host 的通知使用事件游标，短任务在两次列表轮询之间完成仍能补取。游标保存在
   现有按 Host 身份隔离的偏好中，重连/重开面板继续；初次发现历史只建立基线。
5. 显式成功连接新 Host 后，在同工作区切到 IDE/AI/Git 前台仍可观察其状态和事件。
   隐藏面板停止输出读取/输入；后台或工作区改变后停止观察。未启用的 Host 不自动
   探测；旧 Host 保留原有隐藏暂停行为。隐藏且已不可达时探测间隔至少 15 秒。

## 实现边界

- `session_task_v1`：创建/Session 的可选 `task.id`（1..512 字节）及 `task.label`
  （最多 160 字节）。它是现有 Task 的引用，不是命令执行许可，也不会让原
  TaskExecutionResult 假装成功、运行过测试或通过 Git 提交验证。
- `session_activity_v1`：会话查询/创建回执的 `activity_sequence` 以及
  `GET /v1/sessions/{id}/activity?after=N`。事件页返回 `events/cursor/resync_required`。
  事件序号独立于进程 revision 和终端字节游标。
- 最新 256 条摘要保存在私有 `.activity` 文件，加载最大 256 KiB，包含时间、事件类型、
  状态、关联 ID 与原输出游标。旧日志未生成过的事件不会回填/伪造；原输出可能独立过期。
- 记录启动、就绪、提示接收、执行、轮次完成/拒绝、审批请求、审批响应排队、关闭及
  进程结束/中断。响应排队不等于上游确认或已执行；进程零退出码不证明结构化任务完成。
- Agent 回调使用独立摘要锁，不反向持有 Manager 锁。历史损坏/落盘错误只让相应历史
  API 报不可用，不隐藏或杀死已有会话。删除会话同时清理其摘要文件。
- 进程结束时保存有协议证据的最终结构化结果。重新加载后仍保留 completed/failed/
  disconnected；旧结构化记录只有零退出码而没有证据时返回 unknown，不推测任务完成。
- Task/主机作用域在创建和原生历史对话框打开期间发生变化，会拒绝旧操作；不同目录
  不可直接关联。旧 Host 不支持该字段时要求更新或清除筛选，不静默丢弃关联。
- 观察采用已有 scoped SSH/RPC，按当前环境和 Host ID 验证迟到结果。活动查询最多
  四个并发，失败单会话退避 30 秒。保留窗口不足只发一条重同步提示，不批量展示
  过期审批；原生审批本身继续验证当前 pending ID。
- 通知与手机偏好保存没有跨进程事务：崩溃边界可能重复通知，首次观察以前、已过期
  或存储故障时的历史不保证通知。这不放宽会话输入/审批的幂等与所有权规则。

## 验证

本轮在 macOS 本机完成下列检查，不替代真实 Agent / 设备验收：

| 检查 | 结果 |
| --- | --- |
| `go test -mod=readonly -race ./...` | 全部包通过，含 Codex/ACP SDK 协议 peer 审批与生命周期关联 |
| `go vet ./...` | 通过 |
| `flutter test test/host test/tasks test/widgets/notification_test.dart` | 318 项通过、3 项环境条件跳过 |
| `flutter analyze lib/host lib/tasks/ui/tasks_panel.dart test/host test/tasks/project_tasks_panel_test.dart` | 无问题 |
| `python3 -m unittest discover -s test -p 'test_*.py'` | 5 项文档/源码导出测试通过 |
| 六目标 Host 构建 | linux/darwin/windows × amd64/arm64 均通过；只生成分发产物，没有安装 |
| 两仓库 `git diff --check` | 通过 |

Flutter 的 3 项跳过分别为原生 scoped SSH 动态库/paramiko、go-task CLI、just CLI
未提供对应测试环境。新增手机流程使用 Android/iOS Widget variant，不是实机系统行为。

新增回归覆盖：实际 Task 菜单导航、同 Task/不同目录隔离、重附着无 create、明确关联的
Agent 创建、旧 Host 拒绝静默丢关联、创建表单打开期间更换 Task、生命周期摘要展示与
过期提示、短任务在相同列表状态之间完成的通知、游标持久化与 Host 隔离、迟到事件
防误通知、重复/缺失序号拒绝、隐藏面板继续观察/停止输出、后台暂停、工作区改变后
停止流量，以及原有 Tasks/Host 的执行和 UI 回归。

Go 新增覆盖：Task 引用复制与持久化、创建重试不重复写历史、并发序号、256 条保留
窗口、恢复游标、权限前置、非法游标、历史删除/损坏/存储失败边界、重启中断记录、
SDK 审批响应摘要、最终结构化状态保存/重载。结构化协议测试使用 fake peer，不是
Codex/Claude/OpenCode 的真实模型编码验收。

## 修改文件

相对首轮新增/继续修改如下；原有首轮变更全部保留。

| 仓库 | 文件及职责 |
| --- | --- |
| Host | 新增 `internal/host/activity.go`、`activity_test.go`：摘要存储、分页、保留窗口、错误边界 |
| Host | `manager.go`、`protocol.go`、`server.go`：Task 引用、游标提示、端点、恢复与删除 |
| Host | `agent.go`、`codex_agent.go`、`acp_agent.go`、`agent_rpc_test.go`、`agent_status_test.go`：结构化生命周期、审批响应、最终结果保存/重载 |
| NimoteCode | 新增 `lib/host/host_activity_view.dart`：摘要 Timeline sheet |
| NimoteCode | `host_client.dart`、`host_panel.dart`、`host_preferences.dart`、`host_session_draft.dart`、`host_session_widgets.dart`：Task 上下文、能力检测、持久游标、前台观察与通知 |
| NimoteCode | `lib/tasks/ui/tasks_panel.dart`：复用原 Task 菜单进入关联会话 |
| NimoteCode | `lib/l10n/host/host_*.arb`、`lib/l10n/intl_*.arb` 和生成的 `lib/generated/l10n*.dart`：六种已有语言 |
| NimoteCode | `test/host/host_fixture.dart`、`host_workflow_test.dart`、`host_preferences_test.dart`、`test/tasks/project_tasks_panel_test.dart`：两种手机平台 Widget 流程、真实菜单导航、旧 Host 与作用域回归 |
| 文档 | 本记录、中英文 Host 协议、状态、计划与首轮审计的续作指引 |

## 剩余范围

统一 AI 模式跨主机 Dashboard / 离线聚合缓存、逐设备权限、完整语义 API 的同名
方法封装、请求账本长期轮换，以及三 Agent × 三 Host 平台 × Android/iOS 的真实
接管验收仍需继续。现有 Task 与 Host 的关联尚不自动导入所有外部已启动的终端，
也不自动合并原 Agent 对话的完整 Timeline。原生对话恢复与 Host 活进程恢复仍是
不同能力；本后端 Host/OS 重启不会恢复活进程。后台/离线系统推送仍未新增。
