# Nimote Host 手工测试教程（中文）

更新：2026-11-13。适用程序：`nimote-host 1.0.0-dev`，源码在本仓库根目录。
本教程测试独立 Host，不需要 Nimote 账号、Firebase 或公网中继，也不使用旧的
`nimote-connect login/start`。建议先用 Linux/macOS 完成步骤 1–10，约 20–30 分钟，
再按需测试后台服务与远端 SSH。Windows 的环境设置见文末。

## 1. 准备测试环境

需要两个终端窗口：**A 运行 Host，B 发送测试命令**。两者使用同一台机器、同一系统用户、
同一套用户配置目录。准备 Python 3（用于构造 JSON）和 Go 1.26.5+（仅源码构建需要）。
不要使用 sudo。最好用测试用户；本教程会启动本机进程，但不会操作你的业务项目。

在终端 B 检查：

```sh
cd /path/to/nimote-host      # 若仓库位于其他路径，替换这一行
python3 --version
go version
curl --noproxy '*' -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:47831/v1/status
```

最后一条若连接失败/显示 `000`，说明端口没有可访问的 HTTP 服务，可以继续；若返回 `401`
或其他 HTTP 状态，先确认是否已有 Host 或其他进程使用端口。不要直接启动第二个实例、
删除锁文件或停止不属于本次测试的进程。`curl` 只做连通性检查，不提供认证材料。

已有预编译包时，按[中文部署指南](deployment.zh-CN.md)校验、解压后，
跳过步骤 2，并将下文 `HOST_BIN` 改为解压后的可执行文件绝对路径。

## 2. 构建测试版本

在仓库根目录执行：

```sh
go run ./tools/build
```

预期输出当前平台的二进制路径和归档路径，例如 Linux amd64：

```text
dist/linux-amd64/nimote-host
dist/nimote-host-1.0.0-dev-linux-amd64.tar.gz
```

该构建不会安装后台服务。归档包含部署文档、许可证及校验和。源码检查可选执行：

```sh
go test -mod=readonly -race ./...
go vet ./...
```

## 3. 设置测试路径并启动 Host

在终端 B 中（当前位于 `host/`）设置：

```sh
export HOST_BIN="$(pwd)/dist/$(go env GOOS)-$(go env GOARCH)/nimote-host"
export HOST_TEST_DIR="$(mktemp -d /tmp/nimote-host-manual.XXXXXX)"
printf 'HOST_BIN=%s\nHOST_TEST_DIR=%s\n' "$HOST_BIN" "$HOST_TEST_DIR"
"$HOST_BIN" version
"$HOST_BIN" help
```

预期 version 为 `nimote-host 1.0.0-dev`。保存打印的两个绝对路径。已使用预编译包时，
直接设置 `HOST_BIN=/你的解压目录/nimote-host`，仍需创建 `HOST_TEST_DIR`。

在终端 A 中，把路径换成刚才打印的实际值：

```sh
export HOST_BIN=/实际的绝对路径/nimote-host
"$HOST_BIN" start
```

预期看到 `listening on 127.0.0.1:47831`，命令持续运行。保持 A 打开。

回到终端 B：

```sh
"$HOST_BIN" status
"$HOST_BIN" capabilities
```

预期 status 中 `running: true`；capabilities 中 `protocol: 1`、`public_transport: false`。
记录 `host_id`，以后重连和重启时应保持一致。`tools` 中没有某个 Agent 表示当前环境未发现
该 CLI，不代表 Host 启动失败。`agent_structured_approval` 当前应为 `false`。

## 4. 创建 RPC 测试助手

以下命令都在终端 B 执行。复制整个代码块，生成一个只存在于临时测试目录的助手：

```sh
cat > "$HOST_TEST_DIR/rpc.py" <<'PY'
import json, os, pathlib, subprocess, sys
method, path = sys.argv[1:3]
request = {"method": method, "path": path}
if len(sys.argv) > 3:
    body = json.loads(sys.argv[3])
    if method == "POST" and path == "/v1/sessions":
        body.setdefault("directory", str(pathlib.Path(__file__).resolve().parent))
    request["body"] = body
result = subprocess.run([os.environ["HOST_BIN"], "rpc"],
    input=json.dumps(request), text=True, capture_output=True)
if result.returncode:
    print(result.stderr, file=sys.stderr)
    raise SystemExit(result.returncode)
print(json.dumps(json.loads(result.stdout), ensure_ascii=False, indent=2))
PY
```

验证助手：

```sh
python3 "$HOST_TEST_DIR/rpc.py" GET /v1/sessions
```

预期是 JSON 数组，首次测试可以是 `[]`，已有其他记录时不必为空。助手默认让新任务工作在
本次临时目录，避免操作业务目录。下面将返回 JSON 保存到文件，是为了保留会话 ID。

## 5. 测试后台任务、退出码和输出恢复

创建一个两秒后结束的任务：

```sh
python3 "$HOST_TEST_DIR/rpc.py" POST /v1/sessions \
  '{"kind":"task","command":"sh","args":["-c","printf HOST_TASK_BEGIN; sleep 2; printf HOST_TASK_END"]}' \
  > "$HOST_TEST_DIR/task.json"
cat "$HOST_TEST_DIR/task.json"
TASK_ID="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$HOST_TEST_DIR/task.json")"
sleep 3
python3 "$HOST_TEST_DIR/rpc.py" GET "/v1/sessions/$TASK_ID"
```

预期：创建返回一个 64 位十六进制 ID；最终 `state: succeeded`、`exit_code: 0`。

读取并解码输出：

```sh
python3 "$HOST_TEST_DIR/rpc.py" GET "/v1/sessions/$TASK_ID/output?offset=0" > "$HOST_TEST_DIR/output.json"
python3 - "$HOST_TEST_DIR/output.json" <<'PY'
import base64, json, sys
result = json.load(open(sys.argv[1]))
print(base64.b64decode(result["data"]).decode("utf-8", errors="replace"))
print("next_offset =", result["next_offset"])
PY
OFFSET="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["next_offset"])' "$HOST_TEST_DIR/output.json")"
python3 "$HOST_TEST_DIR/rpc.py" GET "/v1/sessions/$TASK_ID/output?offset=$OFFSET"
```

预期解码文本为 `HOST_TASK_BEGINHOST_TASK_END`。从 `next_offset` 再读取时，`data` 是空字符串，
不会重复返回之前的字节。单次读取最多 64 KiB，大输出应持续按 `next_offset` 读取。

再创建一个失败任务：

```sh
python3 "$HOST_TEST_DIR/rpc.py" POST /v1/sessions \
  '{"kind":"task","command":"sh","args":["-c","exit 7"]}' > "$HOST_TEST_DIR/failed.json"
FAILED_ID="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$HOST_TEST_DIR/failed.json")"
sleep 1
python3 "$HOST_TEST_DIR/rpc.py" GET "/v1/sessions/$FAILED_ID"
```

预期 `state: failed`、`exit_code: 7`。创建操作收到成功响应只说明会话创建成功，不代表任务执行成功。

## 6. 测试终端输入、resize 和取消

```sh
python3 "$HOST_TEST_DIR/rpc.py" POST /v1/sessions \
  '{"kind":"terminal","command":"sh","args":["-i"]}' > "$HOST_TEST_DIR/terminal.json"
TERM_ID="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$HOST_TEST_DIR/terminal.json")"
INPUT_BODY="$(python3 -c 'import base64,json; print(json.dumps({"data":base64.b64encode(b"echo HOST_PTY_OK\n").decode()}))')"
python3 "$HOST_TEST_DIR/rpc.py" POST "/v1/sessions/$TERM_ID/input" "$INPUT_BODY"
python3 "$HOST_TEST_DIR/rpc.py" POST "/v1/sessions/$TERM_ID/resize" '{"rows":40,"cols":120}'
sleep 1
python3 "$HOST_TEST_DIR/rpc.py" GET "/v1/sessions/$TERM_ID/output?offset=0" \
  | python3 -c 'import base64,json,sys; print(base64.b64decode(json.load(sys.stdin)["data"]).decode(errors="replace"))'
python3 "$HOST_TEST_DIR/rpc.py" POST "/v1/sessions/$TERM_ID/cancel"
sleep 1
python3 "$HOST_TEST_DIR/rpc.py" GET "/v1/sessions/$TERM_ID"
```

预期输入/resize 成功，输出可见 `HOST_PTY_OK`；可能同时有提示符和终端回显。
最终 `state: canceled`。取消后不应继续接受输入。输出中看见输入回显本身不等于命令执行，
也应确认有命令实际生成的标记。

## 7. 验证认证和事件

```sh
curl --noproxy '*' -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:47831/v1/status
python3 "$HOST_TEST_DIR/rpc.py" GET '/v1/events?after=0'
```

预期匿名请求返回 `401`，认证 CLI 能查询事件。事件响应包含 `events`、`cursor`、
`resync_required`，应能找到本次会话的状态变化。较旧游标可能要求全量刷新，这不是记录损坏。
不要为测试把 token 写入命令行或发送到手机。

## 8. 验证 MCP 默认只读

```sh
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  | env -u NIMOTE_HOST_MCP_WRITE "$HOST_BIN" mcp
```

预期返回初始化结果和工具列表；默认只有 `host_capabilities`、`host_sessions`、
`host_output`，没有 `host_start`/`host_cancel`。需要单独验证写工具时，明确设置
`NIMOTE_HOST_MCP_WRITE=1` 后重复上述请求，预期多出两个写工具。本步骤没有调用写工具。

## 9. 测试 Host 重启后的记录保留

先创建一个长任务，然后停止 Host：

```sh
python3 "$HOST_TEST_DIR/rpc.py" POST /v1/sessions \
  '{"kind":"task","command":"sh","args":["-c","sleep 300"]}' > "$HOST_TEST_DIR/interrupted.json"
INTERRUPTED_ID="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$HOST_TEST_DIR/interrupted.json")"
"$HOST_BIN" stop
```

预期终端 A 的 Host 退出。确认 A 已返回提示符后，再在 A 执行 `"$HOST_BIN" start`。
终端 B 执行：

```sh
"$HOST_BIN" capabilities
python3 "$HOST_TEST_DIR/rpc.py" GET "/v1/sessions/$TASK_ID"
python3 "$HOST_TEST_DIR/rpc.py" GET "/v1/sessions/$INTERRUPTED_ID"
```

预期 Host ID 不变，旧成功任务仍为 `succeeded`，长任务为 `interrupted`。
这证明记录保留，不代表进程跨 Host/系统重启恢复。停止 Host 会终止该用户此 Host 托管的
所有活动会话，因此请在专用测试实例中执行本步骤。

## 10. 验证后台服务（可选，在目标实机执行）

先结束本次前台实例，再注册服务：

```sh
"$HOST_BIN" stop
# 等待终端 A 返回提示符；使用固定安装路径的 binary，不要使用即将删除的临时解压路径。
"$HOST_BIN" install
"$HOST_BIN" status
"$HOST_BIN" restart
"$HOST_BIN" status
```

预期 install/restart 后 status 最终 `running: true`。服务启动可能有短暂延迟，失败时等一秒
重试 status；不要重发任务创建请求。Linux 需 systemd user；macOS 需当前用户 GUI session。
配置、日志和注销后的运行边界见[部署指南](deployment.zh-CN.md)。

不用后台服务时跳过此步。不要移动已经注册的二进制；以后删除测试构建目录前先 uninstall。

## 11. 验证远端 SSH 断开后任务继续（可选）

先确保 Host 通过后台服务运行，或者保留运行前台 Host 的终端 A。**只断开请求终端 B，
不要断开承载前台 Host 的 A。**

1. 从另一台电脑执行 `ssh user@host`，用同一系统用户登录。
2. 设置该机器上的 `HOST_BIN`、`HOST_TEST_DIR` 为绝对路径，沿用步骤 4 的助手。
3. 用步骤 5 的方式创建 `sh -c 'sleep 20; printf SSH_RECONNECT_OK'` 任务，把响应保存为
   `reconnect.json` 并记下 ID。
4. 直接 `exit` 退出这条 SSH 连接；等 25 秒后重新登录。
5. 用保存的 ID 查询会话和输出，预期 `succeeded` 且输出含 `SSH_RECONNECT_OK`。

手机上的 SSH 客户端也可按这组步骤测试。这是 SSH/Host 级验证，不能当作 NimoteCode
Host 图形面板已集成的证据。断线不会停止任务，Host 重启则是另一个语义。

## 12. 验证真实 Agent（可选）

先运行 capabilities，确认 `tools` 有 `codex`、`claude` 或 `opencode`，并用同一 OS 用户
完成第三方 CLI 的登录。用下面请求代替步骤 6 的终端创建请求：

```json
{"kind":"agent","agent":"codex","args":["--version"]}
```

用助手实际发起请求并保存 ID：

```sh
python3 "$HOST_TEST_DIR/rpc.py" POST /v1/sessions \
  '{"kind":"agent","agent":"codex","args":["--version"]}' > "$HOST_TEST_DIR/agent.json"
AGENT_ID="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$HOST_TEST_DIR/agent.json")"
sleep 1
python3 "$HOST_TEST_DIR/rpc.py" GET "/v1/sessions/$AGENT_ID/output?offset=0" \
  | python3 -c 'import base64,json,sys; print(base64.b64decode(json.load(sys.stdin)["data"]).decode(errors="replace"))'
```

可将 agent 改为 `claude`/`opencode`。先检查版本输出，这是启动入口冒烟测试；真实对话还需
去掉 `--version`，在临时工作目录按原生 CLI 提示输入并读取输出。审批通过终端原生提示
处理，不会自动批准，也没有统一审批 RPC。真实对话可能使用第三方付费额度，按需执行。

## 13. 清理本次记录并退出

在终端 B 中，先确保本次会话都已结束。下面只删除本次临时目录保存过的 ID：

```sh
python3 - "$HOST_TEST_DIR" <<'PY'
import json, os, pathlib, subprocess, sys
for name in ["task.json", "failed.json", "terminal.json", "interrupted.json", "reconnect.json", "agent.json"]:
    path = pathlib.Path(sys.argv[1]) / name
    if not path.exists():
        continue
    session_id = json.loads(path.read_text())["id"]
    request = {"method":"DELETE", "path":"/v1/sessions/" + session_id}
    result = subprocess.run([os.environ["HOST_BIN"], "rpc"], input=json.dumps(request), text=True)
    if result.returncode:
        raise SystemExit("会话未结束或删除失败，请检查该 ID，不要清空整个状态目录")
PY
"$HOST_BIN" stop
```

若步骤 10 安装了服务，再执行 `"$HOST_BIN" uninstall`。卸载保留状态、凭据和历史记录。
确认无需保留本次 JSON 后，可手动删除 `HOST_TEST_DIR` 指向的临时目录。不要删除整个 Host
状态目录，也不要清空其他人的会话。记录测试平台、版本、通过步骤和失败现象，分享结果时
不附 token、原始状态目录或私有命令输出。

## Windows 调整说明

Windows 的部署和后台任务操作先按[Windows PowerShell 部署章节](deployment.zh-CN.md)
执行。步骤 4 中的 Python 助手也支持 Windows：保存为临时目录的 `rpc.py`，使用 Python 3
执行；设置环境变量的语法改为：

```powershell
$env:HOST_BIN = "$env:LOCALAPPDATA\NimoteHost\nimote-host.exe"
$env:HOST_TEST_DIR = Join-Path $env:TEMP ('nimote-host-test-' + [guid]::NewGuid())
New-Item -ItemType Directory -Path $env:HOST_TEST_DIR | Out-Null
& $env:HOST_BIN version
& $env:HOST_BIN start
```

在另一个 PowerShell 窗口中设置相同两个值，调用 `python "$env:HOST_TEST_DIR\rpc.py" ...`。
不要直接复制 Bash 的 heredoc、变量赋值和管道命令到 PowerShell。构造任务时用
`command: powershell.exe`，参数 `["-NoProfile", "-Command", "Write-Output HOST_TASK_OK; Start-Sleep 2"]`；
失败任务使用 `exit 7`，交互终端使用 `command: cmd.exe`，输入命令改为 `echo HOST_PTY_OK`。
JSON 示例中的参数必须写成合法的字符串数组。终端输入仍需 UTF-8 字节的 Base64 编码。
Windows 原生验收应记录 ConPTY 输入、输出、resize、取消以及 Task Scheduler 登录生命周期。

## 验收表

| 项目 | 通过标准 |
| --- | --- |
| 构建与版本 | 本机可执行，版本正确 |
| 状态与能力 | running 为 true，protocol 为 1 |
| 成功/失败任务 | succeeded/0 与 failed/7 |
| 输出恢复 | 正确解码，next_offset 无重复读取 |
| 终端 | 输入执行、resize 成功、取消有效 |
| 认证 | 无 token 返回 401 |
| MCP | 默认仅三个只读工具 |
| Host 重启 | Host ID/记录保留，运行中任务 interrupted |
| 后台服务（可选） | 安装、重启、卸载符合平台行为 |
| SSH 重连（可选） | 请求连接退出后任务完成、输出可恢复 |
| Agent（可选） | 版本冒烟与真实对话分开记录 |

## 本教程的核对记录

2026-11-13 在 Linux amd64 上，使用临时状态目录和前台 Host 实际执行了任务成功/失败、
输出与偏移读取、终端输入/resize/取消、匿名 401、MCP 默认只读、停止/重启后的 interrupted
状态及清理命令。后台服务安装、远端 SSH、真实 Agent 对话和 Windows/macOS 原生运行
未在本轮执行，仍应由测试者按对应步骤记录。临时状态目录仅用于文档验证，本教程默认
使用同一 OS 用户的正常状态目录；测试时不要影响已有托管任务。

实机结果另见 [Windows / WSL2 补测记录](records/host-win-wsl-adb-validation-2026-11-13.md)，包含复用手机保存连接的结果和已修复问题。
