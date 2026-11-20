// SPDX-License-Identifier: Apache-2.0
//go:build windows

package host

import (
	"os"
	"path/filepath"
	"testing"
)

func TestToolWindowsCommandOverridesAreCaseInsensitive(t *testing.T) {
	bin := t.TempDir()
	path := toolExecutableFixture(t, bin, "codex", "discovery-only")
	r := installToolEnvironmentFixture(t, ToolEnvironmentConfig{DisableShell: true, Executables: map[string]string{"CoDeX": path}}, os.Environ())
	for _, name := range []string{"codex", "CoDeX", "CODEX"} {
		got, source, err := r.snapshot.Load().resolve(name)
		if err != nil || got != path || source != "configured" {
			t.Fatal(name, got, source, err)
		}
	}
	toolConfigFixture(t, r.dir, ToolEnvironmentConfig{Executables: map[string]string{"codex": path, "CODEX": path}})
	if err := r.refresh(t.Context()); err == nil {
		t.Fatal("ambiguous case-insensitive override accepted")
	}
}

func TestToolWindowsBatchLauncherAndCurrentDirectory(t *testing.T) {
	bin := t.TempDir()
	path := filepath.Join(bin, "nimote-batch-fixture.cmd")
	if err := os.WriteFile(path, []byte("@echo off\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r := installToolEnvironmentFixture(t, ToolEnvironmentConfig{DisableShell: true, SearchPaths: []string{bin}}, os.Environ())
	got, _, err := r.snapshot.Load().resolve("nimote-batch-fixture")
	if err != nil || got != path {
		t.Fatal("npm-style CMD launcher not found", got, err)
	}
	workspace := t.TempDir()
	t.Chdir(workspace)
	toolExecutableFixture(t, workspace, "nimote-workspace-shadow-fixture", "discovery-only")
	// Relative/empty PATH entries must not become ambient workspace discovery.
	r.base = []string{"PATH=.;"}
	if err := r.refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.snapshot.Load().resolve("nimote-workspace-shadow-fixture"); err == nil {
		t.Fatal("workspace executable shadowed installed tools")
	}
}
