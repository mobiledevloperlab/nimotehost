# 维护、兼容性与发布

[English](maintenance.md) | **简体中文**

## 模块边界

模块路径为 `github.com/mobiledevloperlab/nimote-host`。构建不需要本地替换、父工作区、
Connect 账号或中继依赖。传输客户端放在模块之外，通过已记录的 RPC/MCP 接口使用 Host。
`internal/host` 负责协议、持久化和进程管理；`internal/hostservice` 负责用户服务；
`cmd/nimote-host` 负责命令路由；`tools/build` 负责生成分发包。

## 复用与职责

通用协议、序列化、HTTP、密码学和平台基础能力应使用 Go 标准库、官方 SDK 或维护中的
开源库。引入依赖前检查兼容性、取消语义、内存上限、许可证和上游测试；已有 SDK 的
协议不再另写一套通信引擎。

| 职责 | 实现 |
| --- | --- |
| MCP | 官方 `modelcontextprotocol/go-sdk` |
| Agent JSON-RPC 连接 | Coder ACP SDK 的通用 `Connection`，交互会话与历史查询统一复用 |
| Codex 线路兼容 | 用 `encoding/json` 去掉 `jsonrpc` 成员的薄适配层；方法、状态和事件映射由 Host 负责 |
| Unix PTY | `creack/pty` |
| Windows ConPTY 与进程归属 | `golang.org/x/sys/windows`；Host 连接原生句柄与 Job Object |
| HTTP、JSON、令牌和 XML 转义 | Go 标准库 |
| 重放游标、会话持久化、服务定义与事务升级 | Host 业务逻辑，保持已记录的状态与操作系统契约 |

`internal/host/agent_rpc.go` 负责 SDK 生命周期、有上限的操作派发和审批交接。
提供方文件负责将结果映射到 Host 状态；内部操作名称不依赖 SDK 的线路请求 ID。
输入写入应在状态锁之外执行，取消请求不能越过尚未发出的提示词，关闭时须清理待审批操作。
实时记录在交给 SDK 前执行 2 MiB 单帧限制；历史查询使用 SDK 自带的有界扫描器。

`server.go` 负责认证 HTTP 边界，`rpc_client.go` 负责本地 RPC 调用，
`agent_history_http.go` 负责历史路由及超时预算。历史查询的 20 秒预算包含在服务端
25 秒响应预算和客户端 30 秒预算内。此次通信层重构不改变 Protocol 1 或持久化格式。

## 开发和依赖升级

运行 `make check`，或[贡献指南](../CONTRIBUTING.zh-CN.md)中的 Go 命令。Connect 是独立仓库，其模块与检查单独维护；Host 检查不要求同级检出。
Dependabot 每周检查 Go 依赖和 Actions 更新。

升级 Go 依赖的步骤：

1. 只在本模块执行 `go get module@version` 和 `go mod tidy`。
2. 审阅上游代码及授权变化，从准确版本更新 `third_party/<directory>/LICENSE` 及相关
   专利和声明文件。可用 `go mod download -json module@version` 定位模块目录。
3. 更新 `third_party/dependencies.json` 和中英文第三方声明。新增模块须增加许可目录和
   版本记录。发布提交不应含本地 `replace`，构建工具会拒绝未知模块或版本。
4. 执行测试、race、vet 和六目标构建。Windows/macOS 运行时改动还须通过对应原生 CI。
   审阅整个变更，包括 go.sum 和许可证。

升级 Go 工具链时，更新 go.mod 和文档中的最低版本，从新工具链更新
`third_party/go/LICENSE` 和 PATENTS，再运行相同检查。二进制构建还会附上实际构建
工具链的许可证与专利文件；源码声明应与最低工具链保持一致。

## 协议与状态变更

软件版本和协议版本独立：发布构建设置 `host.Version`，`Protocol` 和最低兼容版本
单独声明。优先新增兼容字段；服务端会拒绝未知请求字段，因此增加请求字段需要能力或
版本协商。适配器和结构化审批未就绪时，能力字段必须如实反映。

保留已有状态读取能力、输出偏移、Host 身份和凭据。修改持久化格式之前记录迁移方法。
开发阶段不对状态格式变更提供通用降级兼容保证。相关变更需要恢复测试，覆盖中断进程
和旧事件。涉及公开行为的文档变更须同步维护中英文版本。

## 构建与发布

使用跨平台 Go 入口，PowerShell 也适用：

```sh
go run ./tools/build -os linux -arch amd64 -version 1.0.0-dev
go run ./tools/build -os darwin -arch arm64 -version 1.0.0-dev
go run ./tools/build -os windows -arch amd64 -version 1.0.0-dev
```

每个 `dist/<os>-<arch>/` 包含二进制、项目和第三方许可证、MODULES.json、BUILD.json、
SHA256SUMS。分发时保留完整目录。版本必须以数字开头；原生服务和真实客户端/Agent
验收完成之前，使用预发布版本。

CI 覆盖原生平台检查，并构建、上传六个完整分发包；也可通过 workflow_dispatch 指定
版本手动构建。创建 release 前，下载并检查产物，发布由维护者执行。CI 产物和本地
检查不代表代码签名或公证。

## 已安装服务的升级与回退

1. 查看 `nimote-host version` 和 capabilities，下载或构建目标包，使用可信发布中公布
   的校验信息核对 SHA256SUMS。
2. 等待任务完成，再执行 `nimote-host stop`。停止 Host 会终止剩余托管进程，确认停止后
   再替换文件。不要覆盖仍在运行的 Windows 可执行文件。
3. 保留私有状态目录；涉及迁移时，先做私密备份。备份含凭据和任务输出。
4. 在用户服务引用的原固定路径替换二进制，运行 `nimote-host install` 更新服务定义并
   启动，或用 `nimote-host start` 前台运行。移动二进制时需更新已注册的服务路径。
5. 通过认证 SSH 检查 version、status 和 capabilities。

回退时先停止 Host，再恢复原路径的旧二进制。若发生状态迁移，遵循该版本的降级说明，
不要假设旧程序可读取新状态。恢复备份中的凭据或状态需由用户明确选择。升级不会自动
重启中断任务，也不会自动批准 Agent 操作。

构建会清空所选 `dist/<os>-<arch>/` 后重新写入，请勿在该目录存放手动维护的文件。
同时在 `dist/` 生成带版本的 `.tar.gz`（Linux/macOS）或 `.zip`（Windows）及配套
`.sha256`。普通用户无需 Go 即可按[部署指南](deployment.zh-CN.md)安装这些包。
