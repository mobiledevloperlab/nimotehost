# 外部 Agent 会话增强测试记录

English / 简体中文 · 更新：2026-11-17

本机 macOS arm64、`go1.27.4`；项目最低声明 `go 1.26.5`。所有 Go 验证使用 `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`；Go 测试本身不下载或安装 Agent。测试只操作临时目录、本次创建的服务和协议夹具，不接管用户真实任务。

## Claude 原生发现与真实 ACP 增量

本次依用户要求另行安装了 Claude Code 2.1.311 与开源 ACP Adapter 0.107.3。未执行真实模型请求、未读取用户会话内容。真实 CLI 测试使用临时 `CLAUDE_CONFIG_DIR`，ACP 测试使用假凭据和本地 HTTP 端点。

| 检查 | 实际结果 |
| --- | --- |
| 最终完整竞态回归 | `NIMOTE_TEST_CLAUDE_DISCOVERY=1 NIMOTE_TEST_CLAUDE_ACP=1 NIMOTE_TEST_CODEX_CONTROL=1 go test -mod=readonly -race ./...` 全部通过，退出码 0；Host 包 226.410 秒，覆盖最终生产源码。条件跳过的真实环境项目不计为通过。 |
| 原生发现、真实 CLI/ACP 与历史回放专项 | `NIMOTE_TEST_CLAUDE_DISCOVERY=1 NIMOTE_TEST_CLAUDE_ACP=1 go test -mod=readonly -race ./internal/host -run 'TestClaudeNativeDiscovery\|TestClaudeInstalledACPInitialization\|TestACPHistory\|TestExternalAdapters\|TestAttachedHistory' -count=1` 通过，25.970 秒。 |
| 真实 CLI 查询 | Claude 2.1.311 隔离空配置查询成功；这证明命令及 JSON 入口可用，不代表真实活跃会话已经验收。非空会话和状态组合由协议夹具验证。 |
| 真实 ACP | 0.107.3 的 ACP v2 initialize、空列表和结构化新会话创建通过，原生 ID 经既有会话更新发布，未降级终端。没有发送 prompt，不宣称真实工具审批、完整历史和上下文续接已经通过。 |
| 失败与边界 | 覆盖损坏/null/超预算输出、身份与工作区校验、空原 ID 不伪造、重复身份、分页与删除、未知状态、无 PID 不凭任务状态推断存活、owner 鉴权、失败消息脱敏、挂起查询取消，以及发现参数不能用于 live attach。 |
| ACP 回放 | 夹具要求 `session/resume` 携带 `replayFrom:{type:start}`，否则拒绝；回放结束和控制状态不再伪装成历史工具调用。原跨会话回放、ACK 校验与有界回放测试继续通过。 |
| 构建与静态检查 | macOS arm64、Linux/Windows amd64 产品构建、Windows amd64 Host 测试交叉编译、`go vet ./...`、文档检查和 `git diff --check` 通过。Windows/Linux 原生运行未验证。 |
| GUI 回归 | `go test -mod=readonly -tags ci ./...` 通过，4.405 秒；仅测试驱动，仍有既有 `-lobjc` 重复库警告。 |

安装全局 npm 的尝试触及磁盘空间上限并回滚；改为 Host 独立目录、省略可选二进制后成功，启动器复用已安装 CLI。最初真实 ACP 测试误将 Start 的初始 admission 记录当作异步更新后的会话，断言失败；已按既有接口读取更新后的记录，重跑通过，没有为测试改变会话 API。上述失败不计为通过。

## OpenCode v1 与 Windows 增量

本次增量没有新增依赖，复用既有 OpenCode 状态机、Go 标准库和已有 `x/sys/windows`。不代表三类 Agent 的全部运行方式均已完成支持。

| 检查 | 实际结果 |
| --- | --- |
| 最终完整竞态回归 | `NIMOTE_TEST_CODEX_CONTROL=1 go test -mod=readonly -race ./...` 全部通过，退出码 0；Host 包 211.027 秒。包含最终 v1 原生 false 回执修正和真实隔离 Codex 测试。之后只完善 Windows 测试夹具自身的 owner 准备与测试记录，另行交叉编译和文档校验。 |
| v1/v2、Codex、Adapter 与端点限制竞态专项 | `go test -mod=readonly -race ./internal/host -run 'TestOpenCodeV1\|TestOpenCodeAttach\|TestCodexAttach\|TestExternalSession\|TestExternalAttachment' -count=1` 通过，17.807 秒。 |
| v1 完整专项及新增边界 | 最终 `go test -mod=readonly -race ./internal/host -run '^TestOpenCodeV1' -count=1` 通过，6.834 秒。覆盖原 ID/工作区、全局 pending 过滤、重复审批、显式中断、prompt_async、不确定写入不重发、掉线恢复、发现分页、失效游标、会话删除/认证、原工具调用关联、超过 50 条消息、跨 session 的 part、超预算、未知状态及原生 false 回执拒绝。 |
| Windows 安全和传输测试编译 | `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -mod=readonly -c ... ./internal/host` 通过。新增 `TestCodexWindowsReadOnlyACL` 和 `TestCodexWindowsControlSocket`；覆盖 owner、null DACL、其他主体的写入/读取/删除子对象权限、继承 ACE，以及真实 AF_UNIX WebSocket 与 ACL 保持不变。这里只完成编译；未在 Windows 执行这些测试。现有三平台原生 CI 会收集它们，但本轮没有运行远程 CI。 |
| 产品与 GUI | macOS arm64、Linux/Windows amd64 产品构建通过；GUI `-tags ci` 单元测试通过，3.875 秒，仍有既有 `-lobjc` 重复库警告。 |
| 静态检查与文档 | `go vet ./...`、`GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./internal/host`、`go test -mod=readonly ./tools/build` 通过。 |
| Python | 19 项通过，2.143 秒。 |

Windows/macOS/Linux 的声明是源码构建目标；交叉编译不能替代原生运行。OpenCode v1/v2 本轮均使用协议夹具，未安装真实 OpenCode。

## 增量前的已有基线

| 检查 | 实际结果 |
| --- | --- |
| 两种接管传输竞态专项 | 通过，10.845 秒；后续增加 owner API 与乱序审批专项，12.412 秒通过。 |
| 真实 Codex 0.169.0 | `NIMOTE_TEST_CODEX_CONTROL=1 go test -mod=readonly -race -v ./internal/host -run '^TestCodexControlInstalledCLI$'` 通过，5.563 秒。使用隔离 CODEX_HOME 和固定回复的本地模型服务；真实桌面线程执行时附着，关闭 Host 后原任务继续完成；重连后通过 Host 续发消息，原 ID 和模型请求中的原上下文均保留。没有调用外部模型。 |
| 最终完整竞态回归 | `NIMOTE_TEST_CODEX_CONTROL=1 go test -mod=readonly -race ./...` 全部通过，退出码 0；Host 包 208.418 秒，包含最终 Adapter 入口、异常 ID/通知、审批竞态、外部策略限制与真实 Codex 上下文续接测试。依赖环境的其他验收可能跳过，不能作为真实环境证据。 |
| GUI 单元测试 | 最终 `go test -mod=readonly -tags ci ./...` 通过，4.361 秒；仅单元测试驱动。链接器有既有 `-lobjc` 重复库警告。 |
| `go vet ./...` | 最终源码通过；沙箱中一次缓存读取受限后，在允许读取缓存的环境复核成功。 |
| Linux/Windows amd64 测试交叉编译 | 测试二进制交叉编译通过。最终产品源码三目标构建另行通过；编译不代表原生运行验收。 |
| 产品构建 | macOS arm64 本机构建、Linux amd64 和 Windows amd64 交叉 `go build -mod=readonly ./...` 全部通过；交叉构建使用 `CGO_ENABLED=0`。 |
| Python、校验和与许可 | 19 项 Python 测试通过；`go mod verify` 通过；16 个依赖的实际模块图与许可清单一致，WebSocket 原始许可证已保留；`git diff --check` 通过。 |

## 自动化覆盖

| 场景 | 验证 |
| --- | --- |
| 缺少 CLI、版本或协议不兼容 | 原 Adapter 测试、列表集合/ID 校验、ACP 主版本拒绝、连接失败不降级为新会话。已有控制连接不依赖安装 CLI。 |
| 原进程与原会话 | Codex Unix WebSocket 夹具与真实 CLI；OpenCode v1/v2 HTTP 夹具只允许原 session 路径，不创建或 fork。 |
| 审批恢复与多个客户端 | 原 pending ID，16 个并发批准只成功一次；桌面先解决的请求不再接受手机批准；先到 resolved 不重建旧审批；pending 先于 resume turn 快照到达时仍正确关联。 |
| 未实现的原生请求 | 保留桌面所有权，等待原生解决；不发送 MethodNotFound 导致原任务失败。 |
| 审批策略一致性 | 外部连接始终 confirm，不能在连接后启用自动审批；原自启会话的策略测试继续通过。 |
| 上游网络中断 | OpenCode 状态变为 disconnected，重读原服务后恢复；相同消息快照不重复写入事件。 |
| 写入已执行但确认丢失 | uncertain 结果重复请求不再次发送；改变同一请求 ID 的内容发生冲突。 |
| Host 退出与重启 | 分离不发送 interrupt/decline；原 busy/pending 保留；重新建立连接使用同一 native ID 和新 Host 记录。 |
| 会话删除、串线、认证 | 404/错误认证失败；跨会话 pending 和回放拒绝；owner 才可发现和附着；结果不含密码。 |
| 端点安全 | 禁止公网、DNS 主机名、URL 凭据、路径、查询、特权端口和重定向；真实 Codex 别名只读解析，不修改权限。 |
| 原能力回归 | 完整回归包含原 SSH/RPC、终端、任务、审批、输出/事件游标、幂等、恢复和安装管理测试；条件跳过的原生验收不算通过。 |

早期基线在沙箱内因本地监听权限中断，后续测试在允许临时本地监听的环境运行。实际 Codex 验证发现 CLI proxy 与控制 socket 握手不匹配，已移除该实现，改为官方 WebSocket；又发现官方 socket 路径别名和空线程尚无 rollout 的差异，已按只读别名验证和真实本地测试 turn 修正。文档清单检查曾失败，已同步新的文档/API 清单并通过最终完整回归。上述失败没有作为通过证据；最终结果以上表为准。

## 未验证与未支持

- OpenCode 本轮没有真实安装环境；v1/v2 依据官方接口实现并由协议夹具验证。v1 使用有界完整读取，超过 2000 条消息或响应预算时明确失败。
- Claude 原生发现已用真实 CLI 空配置验证，非空状态由夹具验证；ACP 初始化已真实验证，真实模型、历史与审批仍待验收。官方终端 `attach` 与 Host 结构化附着分开，后者仍 unsupported。
- Codex 真实验收使用本地固定模型回复，不代表外部模型、安全审批、所有复杂工具和多 writer 场景均已通过。
- Windows、Linux 原生运行、最低 Go 版本与其他架构；Windows Codex 控制 socket 所有权验证和原生测试代码已实现，但仅完成交叉编译与静态检查，尚未原生执行。
- 手机/平板、后台/断网、真实 SSH 网络和系统登录/重启、长期负载。没有执行任何机器重启。
- 不声称绝对零 Bug；架构、API、能力矩阵见[实施说明](external-agent-sessions.md)。
