# System Monitor UI demo

This example replaces the previous Echo and HTTP Inspector demos and uses the public SDK and UI Contract 1.0. Source lives under `examples/`; the existing local installable demo location is `~/Downloads/NimoteHost-Plugin-Demos/system-monitor/`.

Open `system-monitor/snapshot`, then collect a local snapshot or generate 500 simulated rows. Sampling settings provide nested controls for mode, 1–500 rows, process filter, CPU/memory sorting, collection sections and alert thresholds. Omitted fields use defaults; submitting settings samples immediately without changing the quick-button presets.

- Overview: device information, metrics, selected-row details, collapsible notes and conditional source hints.
- Processes: search, numeric sorting, 20-row pages and complete selected-row JSON. Demo data exercises Unicode, long names and nulls.
- Network and trends: responsive columns, interface information, 60 simulated trend samples and linked details.
- Alerts: collected processes exceeding thresholds, independent of name filtering and displayed-row limits.
- Sampling settings: nested objects, booleans, enums, numeric bounds and optional fields.
- Raw snapshot: collapsible complete JSON.

New samples preserve the previously selected row object; select again to inspect new data. Live mode does not fabricate history. Empty arrays exercise empty tables; demo mode tests cross-page search/sorting and narrow/large-text layouts. Charts, dashboard cards and timed calls are not protocol components; tables and details use supported controls.

macOS/Linux collect processes and the root filesystem using time/output-bounded `ps` and `df`. All platforms use Go runtime and `net.Interfaces` for system identity/interfaces. Other platforms report unavailable process/disk collection while retaining demo mode. CPU is average process CPU from ps and may exceed 100%; process memory percentages may include shared pages and are not system physical-memory utilization. Interface information is not throughput.

The plugin is read-only, requests no network grant, and returns no process command arguments or environment variables. `process.same-user` is required and does not provide an OS sandbox.

```sh
go test ./examples/system-monitor
go build -o /tmp/system-monitor ./examples/system-monitor
go run ./tools/pluginpack -binary /tmp/system-monitor \
  -manifest examples/system-monitor/manifest.template.json \
  -out /tmp/system-monitor-bundle
```

The output directory must not exist. Manually install the generated `manifest.json` in Host GUI, then enable it. Packaging does not install, enable or modify the running Host. See the [SDK guide](../../docs/plugin-system/plugin-sdk.md).
