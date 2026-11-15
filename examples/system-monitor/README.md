# 系统信息监测 UI Demo

此示例替代原 Echo 和 HTTP Inspector 测试插件，使用公开 SDK 和 UI Contract 1.0。
源码沿用 `examples/` 目录；本机可安装包沿用
`~/Downloads/NimoteHost-Plugin-Demos/system-monitor/`。

打开 `system-monitor/snapshot` 后，可点击「采集本机快照」或「生成 500 行模拟压力数据」。
「采样设置」提供嵌套表单：模式、1–500 行上限、进程过滤、CPU/内存排序、采集范围、告警阈值。
字段省略时插件使用默认值。表单提交立即采样，不改写两个快捷按钮的预设参数。

- 总览：设备信息、指标表、行详情、折叠说明、条件显示的来源提示。
- 进程：搜索、数值排序、20 行分页、选中行的完整 JSON；模拟数据包含 Unicode、长名称、null。
- 网络与趋势：响应式双列、接口信息、60 条模拟历史样本及详情联动。
- 告警：采集进程超过阈值时列出；告警不受名称过滤与显示行数限制。
- 采样设置：嵌套对象、布尔值、枚举、数值范围和可选字段。
- 原始快照：折叠展示完整 JSON。

新采样保留之前选择的行对象，需重新选行查看新快照详情。真实模式不伪造历史数据；
空数组可用于测试空表状态。先生成模拟数据可测试跨页搜索、排序和窄屏/大字号布局。
当前协议没有图表、仪表盘卡片和定时调用组件，使用已支持的表格和详情展示。

macOS/Linux 通过有超时和输出上限的 `ps` / `df` 采集进程和根文件系统；所有平台
通过 Go runtime 和 net.Interfaces 采集系统身份及接口。其他平台明确提示进程/磁盘
不可用，模拟模式仍可使用。CPU 为 ps 的进程平均值，可超过 100%；进程内存占比
可能包含共享页，不是整机物理内存使用率。网络接口信息不代表吞吐率。

插件只读，不请求网络访问，不返回进程命令行参数或环境变量。需要
`process.same-user` 权限。权限并非操作系统沙箱。

```sh
go test ./examples/system-monitor
go build -o /tmp/system-monitor ./examples/system-monitor
go run ./tools/pluginpack -binary /tmp/system-monitor \
  -manifest examples/system-monitor/manifest.template.json \
  -out /tmp/system-monitor-bundle
```

打包目录必须不存在。将生成的 `manifest.json` 在 Host GUI 中手动安装，然后启用。
打包不会自动安装、启用或修改 Host。详见 [SDK 指南](../../docs/plugin-system/plugin-sdk.md)。
