// SPDX-License-Identifier: Apache-2.0
// System Monitor exercises the public declarative UI with bounded read-only snapshots.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	sdk "github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk"
)

type options struct {
	Mode     string `json:"mode"`
	Limit    int    `json:"limit"`
	Filter   string `json:"filter"`
	Sort     string `json:"sort"`
	Sections struct {
		Processes bool `json:"processes"`
		Network   bool `json:"network"`
	} `json:"sections"`
	Thresholds struct {
		CPU    float64 `json:"cpu"`
		Memory float64 `json:"memory"`
	} `json:"thresholds"`
}
type row = map[string]any
type snapshot struct {
	Mode        string   `json:"mode"`
	CapturedAt  string   `json:"captured_at"`
	Summary     row      `json:"summary"`
	Metrics     []row    `json:"metrics"`
	Processes   []row    `json:"processes"`
	Network     []row    `json:"network"`
	History     []row    `json:"history"`
	Alerts      []row    `json:"alerts"`
	Warnings    []string `json:"warnings"`
	HasWarnings bool     `json:"has_warnings"`
}

// Neither command arguments nor paths come from UI input. Limit output even on busy hosts.
type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 256<<10 {
		return 0, errors.New("command output limit")
	}
	return b.Buffer.Write(p)
}
func command(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	var out boundedBuffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	err := cmd.Run()
	return out.String(), err
}
func defaults() options {
	var o options
	o.Mode = "live"
	o.Limit = 100
	o.Sort = "cpu"
	o.Sections.Processes = true
	o.Sections.Network = true
	o.Thresholds.CPU = 80
	o.Thresholds.Memory = 80
	return o
}
func decode(raw json.RawMessage) (options, error) {
	o := defaults()
	if b := bytes.TrimSpace(raw); len(b) == 0 || b[0] != '{' {
		return o, errors.New("expected options object")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&o); err != nil {
		return o, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return o, errors.New("trailing input")
	}
	if o.Mode != "live" && o.Mode != "demo" || o.Limit < 1 || o.Limit > 500 || utf8.RuneCountInString(o.Filter) > 128 || o.Sort != "cpu" && o.Sort != "memory" || o.Thresholds.CPU < 1 || o.Thresholds.CPU > 100 || o.Thresholds.Memory < 1 || o.Thresholds.Memory > 100 {
		return o, errors.New("invalid options")
	}
	return o, nil
}
func parseProcesses(raw string) []row {
	rows := []row{}
	for _, line := range strings.Split(raw, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		cpu, e2 := strconv.ParseFloat(f[1], 64)
		mem, e3 := strconv.ParseFloat(f[2], 64)
		rss, e4 := strconv.ParseInt(f[3], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || pid < 1 || rss < 0 {
			continue
		}
		name := strings.Join(f[4:], " ")
		if len(name) > 256 {
			name = strings.ToValidUTF8(name[:256], "")
		}
		rows = append(rows, row{"pid": pid, "name": name, "cpu": cpu, "memory": mem, "rss_mib": float64(rss) / 1024})
	}
	return rows
}
func collect(ctx context.Context, o options) snapshot {
	s := snapshot{Mode: o.Mode, CapturedAt: time.Now().UTC().Format(time.RFC3339), Summary: row{"os": runtime.GOOS, "architecture": runtime.GOARCH, "logical_cpus": runtime.NumCPU()}, Metrics: []row{}, Processes: []row{}, Network: []row{}, History: []row{}, Alerts: []row{}, Warnings: []string{}}
	if o.Mode == "demo" {
		demo(&s, o)
	} else {
		s.Summary["hostname"], _ = os.Hostname()
		if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
			if raw, err := command(ctx, "ps", "-axo", "pid=,pcpu=,pmem=,rss=,comm="); err == nil {
				rows := parseProcesses(raw)
				cpu, mem := 0.0, 0.0
				for _, r := range rows {
					cpu += r["cpu"].(float64)
					mem += r["memory"].(float64)
				}
				s.Metrics = append(s.Metrics, row{"metric": "进程 CPU 合计", "value": cpu, "unit": "%（可超过 100）", "scope": "ps 平均值，非瞬时整机使用率"}, row{"metric": "进程内存占比合计", "value": mem, "unit": "%", "scope": "可能包含共享页，非物理内存使用率"})
				if o.Sections.Processes {
					s.Processes = rows
				}
			} else {
				s.Warnings = append(s.Warnings, "进程采集不可用：ps 失败或输出超限")
			}
			if raw, err := command(ctx, "df", "-Pk", "/"); err == nil {
				lines := strings.Split(strings.TrimSpace(raw), "\n")
				if len(lines) > 1 {
					f := strings.Fields(lines[len(lines)-1])
					if len(f) >= 6 {
						s.Metrics = append(s.Metrics, row{"metric": "根文件系统容量", "value": f[1], "unit": "KiB", "scope": f[len(f)-1]}, row{"metric": "根文件系统可用", "value": f[3], "unit": "KiB", "scope": f[4] + " 已用"})
					}
				}
			} else {
				s.Warnings = append(s.Warnings, "磁盘容量采集不可用")
			}
		} else {
			s.Warnings = append(s.Warnings, "此平台仅提供系统身份和网络接口；进程及磁盘采集尚未实现")
		}
		if o.Sections.Network {
			interfaces, err := net.Interfaces()
			if err != nil {
				s.Warnings = append(s.Warnings, "网络接口采集不可用")
			} else {
				for _, n := range interfaces[:min(len(interfaces), 1000)] {
					s.Network = append(s.Network, row{"name": n.Name, "index": n.Index, "mtu": n.MTU, "flags": n.Flags.String()})
				}
			}
		}
		s.Warnings = append(s.Warnings, "实时模式按按钮手动采样；历史趋势仅在模拟模式提供。网络表为接口信息，不包含吞吐率。")
	}
	filtered := []row{}
	for _, r := range s.Processes {
		if strings.Contains(strings.ToLower(r["name"].(string)), strings.ToLower(o.Filter)) {
			filtered = append(filtered, r)
		}
		if len(s.Alerts) < 500 && (r["cpu"].(float64) >= o.Thresholds.CPU || r["memory"].(float64) >= o.Thresholds.Memory) {
			s.Alerts = append(s.Alerts, row{"severity": "warning", "source": r["name"], "message": "进程超过 CPU / 内存阈值", "pid": r["pid"]})
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i][o.Sort].(float64) > filtered[j][o.Sort].(float64) })
	s.Summary["matched_processes"] = len(filtered)
	if len(filtered) > o.Limit {
		filtered = filtered[:o.Limit]
	}
	s.Processes = filtered
	s.Summary["displayed_processes"] = len(filtered)
	s.Summary["alert_count"] = len(s.Alerts)
	s.HasWarnings = len(s.Warnings) > 0
	return s
}
func demo(s *snapshot, o options) {
	s.Summary["hostname"] = "demo-workstation（模拟）"
	s.Summary["logical_cpus"] = 16
	s.Warnings = append(s.Warnings, "模拟数据：用于 UI 测试，与当前设备负载无关。包含长文本、Unicode、空值和 500 行分页数据。")
	s.Metrics = []row{{"metric": "CPU 使用率", "value": 67.4, "unit": "%", "scope": "模拟整机"}, {"metric": "内存使用率", "value": 82.1, "unit": "%", "scope": "模拟 32 GiB"}, {"metric": "磁盘使用率", "value": 71.8, "unit": "%", "scope": "模拟 1 TiB"}, {"metric": "网络接收", "value": 128.5, "unit": "MiB/s", "scope": "模拟接口"}}
	if o.Sections.Processes {
		for i := 0; i < 500; i++ {
			name := []string{"WindowServer", "编译工作进程 / build-worker", "数据库 · analytics", "renderer-with-a-very-long-name-to-test-narrow-layout", "idle-service"}[i%5]
			s.Processes = append(s.Processes, row{"pid": 1000 + i, "name": fmt.Sprintf("%s-%03d", name, i), "cpu": float64((i*37)%1000) / 10, "memory": float64((i*13)%950) / 10, "rss_mib": float64((i * 71) % 8192), "owner": "demo", "note": nil})
		}
	}
	if o.Sections.Network {
		s.Network = []row{{"name": "en0 · 模拟", "index": 1, "mtu": 1500, "flags": "up|broadcast|multicast"}, {"name": "lo0 · 模拟", "index": 2, "mtu": 16384, "flags": "up|loopback"}}
	}
	for i := 0; i < 60; i++ {
		s.History = append(s.History, row{"sample": i + 1, "cpu": float64(30 + (i*17)%65), "memory": float64(55 + (i*7)%40), "rx_mib_s": float64((i * 23) % 150), "source": "模拟趋势"})
	}
}
func handle(ctx context.Context, name string, raw json.RawMessage, progress func(float64)) (any, error) {
	if name != "snapshot" {
		return nil, errors.New("unknown capability")
	}
	o, err := decode(raw)
	if err != nil {
		return nil, err
	}
	progress(0)
	s := collect(ctx, o)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	progress(1)
	return s, nil
}
func main() {
	if sdk.Serve(context.Background(), os.Stdin, os.Stdout, handle) != nil {
		os.Exit(1)
	}
}
