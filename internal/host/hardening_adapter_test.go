// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAdapterDescriptorsAndMissingInstallations(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for name, version := range map[string]int{"codex": 0, "opencode": 1, "claude": 2} {
		d, ok := lookupAgentAdapter(name)
		if !ok || d.ProtocolVersion() != version {
			t.Fatal(name, d)
		}
		if !supportsStructuredAgent(name) {
			t.Fatal("existing adapter disappeared")
		}
		if err := d.ConfigureCommand(exec.Command("missing")); err == nil {
			t.Fatal("missing adapter reported installed")
		}
	}
	if _, ok := lookupAgentAdapter("unknown"); ok || supportsStructuredAgent("unknown") {
		t.Fatal("unknown adapter accepted")
	}
	caps := capabilities("host")
	if !caps.Features["device_session_acl_v1"] || !caps.Features["resource_limits_v1"] || caps.Features["hard_cpu_limit"] || caps.Features["hard_memory_limit"] {
		t.Fatal("incorrect capability declaration")
	}
	for name, info := range InspectAgents(context.Background()) {
		if info.Installed || info.ProbeStatus != "not_installed" {
			t.Fatal(name, info)
		}
	}
}
func TestAdapterVersionProbeIsBoundedAndExplicit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture; Windows backend has native process tests")
	}
	dir := t.TempDir()
	for name, script := range map[string]string{"codex": "#!/bin/sh\nprintf 'codex-test 1.2.3\\n'\n", "opencode": "#!/bin/sh\nwhile :; do sleep 1; done\n", "claude-agent-acp": "#!/bin/sh\nexit 7\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if info := capabilities("host").Agents["codex"]; info.ProbeStatus != "not_probed" || info.Version != "" {
		t.Fatal("poll executed agent", info)
	}
	got := InspectAgents(context.Background())
	if got["codex"].Version != "codex-test 1.2.3" || got["codex"].ProbeStatus != "ok" || got["opencode"].ProbeStatus != "timeout" || got["claude"].ProbeStatus != "failed" {
		t.Fatal(got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, info := range InspectAgents(ctx) {
		if info.ProbeStatus != "canceled" {
			t.Fatal(name, info)
		}
	}
}
