// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func toolConfigFixture(t *testing.T, dir string, c ToolEnvironmentConfig) {
	t.Helper()
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tools.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func installToolEnvironmentFixture(t *testing.T, c ToolEnvironmentConfig, base []string) *toolEnvironment {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := privateDir(dir); err != nil {
		t.Fatal(err)
	}
	toolConfigFixture(t, dir, c)
	r := &toolEnvironment{dir: dir, base: base}
	if err := r.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	previous := activeToolEnvironment.Swap(r)
	t.Cleanup(func() { activeToolEnvironment.Store(previous) })
	return r
}

func toolExecutableFixture(t *testing.T, dir, name, script string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		// Discovery-only fixtures. Native executable/session coverage uses the
		// compiled test binary in the Windows-specific tests.
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestToolDiscoveryOverridesAndRuntimeEnvironment(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "tools with spaces")
	codex := toolExecutableFixture(t, bin, "codex", "#!/bin/sh\nexit 0\n")
	base := []string{"PATH=" + t.TempDir(), "HOME=" + t.TempDir(), "HOST_SECRET=retained"}
	installToolEnvironmentFixture(t, ToolEnvironmentConfig{DisableShell: true, SearchPaths: []string{bin}}, base)
	info := capabilities("fixture").Agents["codex"]
	if !info.Installed || !info.CLIDiscovered || info.Path != codex || info.PathSource != "configured" {
		t.Fatalf("discovery: %+v", info)
	}
	cmd := exec.Command("codex", "--version")
	cmd.Env = append(base, "TERM=test-terminal")
	adapter, _ := lookupAgentAdapter("codex")
	if err := adapter.ConfigureCommand(cmd); err != nil {
		t.Fatal(err)
	}
	if cmd.Err != nil || cmd.Path != info.Path || strings.Join(cmd.Args[1:], " ") != "app-server" {
		t.Fatalf("command: %+v", cmd)
	}
	if environmentValue(cmd.Env, "TERM") != "test-terminal" || environmentValue(cmd.Env, "HOST_SECRET") != "retained" {
		t.Fatal("lost caller environment")
	}
	if paths := filepath.SplitList(environmentValue(cmd.Env, "PATH")); len(paths) == 0 || paths[0] != bin {
		t.Fatal("missing CLI runtime directory", paths)
	}
	if environmentValue(base, "PATH") == environmentValue(cmd.Env, "PATH") {
		t.Fatal("discovery did not supply the execution PATH")
	}
}

func TestToolDiscoveryRefreshAndInvalidOverride(t *testing.T) {
	bin := t.TempDir()
	r := installToolEnvironmentFixture(t, ToolEnvironmentConfig{DisableShell: true, SearchPaths: []string{bin}}, []string{"PATH=" + t.TempDir()})
	if _, _, err := resolveTool("nimote-new-tool-fixture", false); err == nil {
		t.Fatal("fixture already exists")
	}
	path := toolExecutableFixture(t, bin, "nimote-new-tool-fixture", "#!/bin/sh\nexit 0\n")
	if got, _, err := resolveTool("nimote-new-tool-fixture", false); err != nil || got != path {
		t.Fatal("new install was negatively cached", got, err)
	}
	other := t.TempDir()
	newPath := toolExecutableFixture(t, other, "nimote-moved-tool-fixture", "#!/bin/sh\nexit 0\n")
	toolConfigFixture(t, r.dir, ToolEnvironmentConfig{DisableShell: true, SearchPaths: []string{other}})
	cmd := exec.Command("nimote-moved-tool-fixture")
	if err := configureToolCommand(cmd, "nimote-moved-tool-fixture"); err != nil || cmd.Path != newPath {
		t.Fatal("launch did not refresh", cmd.Path, err)
	}
	missing := filepath.Join(t.TempDir(), "missing-codex")
	toolConfigFixture(t, r.dir, ToolEnvironmentConfig{DisableShell: true, Executables: map[string]string{"codex": missing}, SearchPaths: []string{bin}})
	toolExecutableFixture(t, bin, "codex", "#!/bin/sh\nexit 0\n")
	if err := r.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	info := installedAgents()["codex"]
	if info.Installed || info.PathSource != "configured" || !strings.Contains(info.DiscoveryError, "configured executable") {
		t.Fatalf("override silently fell back: %+v", info)
	}
	before := r.snapshot.Load()
	toolConfigFixture(t, r.dir, ToolEnvironmentConfig{SearchPaths: []string{"relative"}})
	if err := r.refresh(context.Background()); err == nil || r.snapshot.Load() != before {
		t.Fatal("invalid refresh replaced working snapshot")
	}
}

func TestToolDiscoveryRejectsRelativeSearchPathsAndPrivateConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := privateDir(dir); err != nil {
		t.Fatal(err)
	}
	for _, config := range []ToolEnvironmentConfig{
		{SearchPaths: []string{"."}}, {SearchPaths: []string{""}},
		{SearchPaths: []string{filepath.Join(dir, "bad\npath")}},
		{Executables: map[string]string{"codex": "relative"}},
		{Executables: map[string]string{"../codex": filepath.Join(dir, "codex")}},
	} {
		toolConfigFixture(t, dir, config)
		if _, err := readToolEnvironmentConfig(dir); err == nil {
			t.Fatalf("accepted invalid config: %+v", config)
		}
	}
	if runtime.GOOS != "windows" {
		toolConfigFixture(t, dir, ToolEnvironmentConfig{})
		if err := os.Chmod(filepath.Join(dir, "tools.json"), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := readToolEnvironmentConfig(dir); err == nil {
			t.Fatal("accepted public tool configuration")
		}
	}
}

func TestToolEnvironmentOwnerRefreshAndConcurrentReads(t *testing.T) {
	r := installToolEnvironmentFixture(t, ToolEnvironmentConfig{DisableShell: true}, os.Environ())
	s := &Server{dir: r.dir, token: "fixture-token"}
	request := func(method, path, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Nimote-Host-Protocol", "1")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		return w
	}
	for _, path := range []string{"/v1/tools", "/v1/tools/refresh", "/v1/agents"} {
		if w := request(http.MethodGet, path, "bad"); w.Code != 401 {
			t.Fatal(path, w.Code)
		}
		if observerAllowed("GET", path) {
			t.Fatal("tool diagnostics exposed to device observer", path)
		}
	}
	w := request("GET", "/v1/tools", s.token)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var status map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if _, exposed := status["environment"]; exposed {
		t.Fatal("tool diagnostics exposed the raw environment", w.Body.String())
	}
	if w := request("POST", "/v1/tools/refresh", s.token); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("GET", "/v1/tools/refresh", s.token); w.Code != 405 {
		t.Fatal(w.Code)
	}
	r.mu.Lock()
	w = request("POST", "/v1/tools/refresh", s.token)
	r.mu.Unlock()
	if w.Code != http.StatusConflict {
		t.Fatal("concurrent refresh must not queue shell processes", w.Code)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				capabilities("fixture")
				cmd := exec.Command("git")
				cmd.Env = toolEnvironmentFor(cmd, "")
			}
		}()
	}
	for range 4 {
		if err := r.refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}
