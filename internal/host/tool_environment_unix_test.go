// SPDX-License-Identifier: Apache-2.0
//go:build !windows

package host

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestToolShellLoginEnvironmentAndOutputIsolation(t *testing.T) {
	for _, fixture := range []struct{ shell, profile string }{{"/bin/bash", ".bash_profile"}, {"/bin/zsh", ".zprofile"}} {
		t.Run(filepath.Base(fixture.shell), func(t *testing.T) {
			if _, err := os.Stat(fixture.shell); err != nil {
				t.Skip("shell not installed")
			}
			home := t.TempDir()
			bin := filepath.Join(home, "custom tools")
			if err := os.Mkdir(bin, 0700); err != nil {
				t.Fatal(err)
			}
			profile := "printf 'startup banner\\n'\nexport PATH='" + bin + ":/usr/bin:/bin'\nexport VOLTA_HOME='" + home + "/volta'\nexport PRIVATE_TOKEN=do-not-import\n"
			if err := os.WriteFile(filepath.Join(home, fixture.profile), []byte(profile), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			env, status := probeToolShell(ctx, []string{"HOME=" + home, "PATH=/usr/bin:/bin"}, fixture.shell)
			if status != "ok" || !strings.HasPrefix(environmentValue(env, "PATH"), bin+":") {
				t.Fatal(status, env)
			}
			if environmentValue(env, "VOLTA_HOME") != home+"/volta" || environmentValue(env, "PRIVATE_TOKEN") != "" {
				t.Fatal("shell selectors/secrets", env)
			}
		})
	}
}

func TestToolShellTimeoutAndBoundedOutput(t *testing.T) {
	for _, profile := range []string{"/bin/sleep 30\n", "/usr/bin/yes startup-output\n"} {
		home := t.TempDir()
		if err := os.WriteFile(filepath.Join(home, ".bash_profile"), []byte(profile), 0600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		started := time.Now()
		_, status := probeToolShell(ctx, []string{"HOME=" + home, "PATH=/usr/bin:/bin"}, "/bin/bash")
		cancel()
		if status == "ok" || time.Since(started) > 2*time.Second {
			t.Fatal("shell discovery was not bounded", status, time.Since(started))
		}
	}
}

func TestToolEnvironmentTaskAndVersionUseDiscoveredRuntime(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "node installation")
	toolExecutableFixture(t, bin, "node", "#!/bin/sh\nprintf 'fixture-node-runtime'\n")
	codex := toolExecutableFixture(t, bin, "codex", "#!/usr/bin/env node\n")
	r := installToolEnvironmentFixture(t, ToolEnvironmentConfig{DisableShell: true, Executables: map[string]string{"codex": codex}}, []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir()})
	got := InspectAgents(context.Background())["codex"]
	if !got.Installed || got.ProbeStatus != "ok" || got.Version != "fixture-node-runtime" {
		t.Fatalf("version did not inherit discovered runtime: %+v", got)
	}
	m := newTestManager(t)
	session, err := m.Start(StartRequest{Kind: "task", Command: "codex", Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if got := awaitSession(t, m, session.ID); got.State != "succeeded" {
		t.Fatal(got)
	}
	data, _, err := m.Output(session.ID, 0)
	if err != nil || string(data) != "fixture-node-runtime" {
		t.Fatal(string(data), err)
	}
	// Explicit ./ paths must still be resolved relative to the session workspace.
	workspace := t.TempDir()
	toolExecutableFixture(t, workspace, "local-task", "#!/bin/sh\nprintf workspace-task\n")
	session, err = m.Start(StartRequest{Kind: "task", Command: "./local-task", Directory: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if got := awaitSession(t, m, session.ID); got.State != "succeeded" {
		t.Fatal(got)
	}
	// Refresh changes future launches only, never the already-started environment.
	before := exec.Command("codex")
	if err := configureToolCommand(before, "codex"); err != nil {
		t.Fatal(err)
	}
	oldPath := environmentValue(before.Env, "PATH")
	toolConfigFixture(t, r.dir, ToolEnvironmentConfig{DisableShell: true, SearchPaths: []string{t.TempDir()}})
	if err := r.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if environmentValue(before.Env, "PATH") != oldPath {
		t.Fatal("refresh changed running command environment")
	}
}

func TestToolEnvironmentIsolatedAgentDoesNotImportOwnerPaths(t *testing.T) {
	bin := t.TempDir()
	owner := toolExecutableFixture(t, bin, "codex", "#!/bin/sh\nexit 0\n")
	installToolEnvironmentFixture(t, ToolEnvironmentConfig{DisableShell: true, Executables: map[string]string{"codex": owner}}, []string{"PATH=" + bin, "VOLTA_HOME=owner-private"})
	inherited := t.TempDir()
	legacy := toolExecutableFixture(t, inherited, "codex", "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", inherited)
	cmd := exec.Command("codex")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/isolated"}
	agent := &structuredAgent{provider: "codex", isolated: true}
	if err := configureStructuredCommand(cmd, agent); err != nil {
		t.Fatal(err)
	}
	if cmd.Path != legacy || environmentValue(cmd.Env, "PATH") != "/usr/bin:/bin" || environmentValue(cmd.Env, "VOLTA_HOME") != "" {
		t.Fatal("isolated agent inherited owner environment", cmd.Path, cmd.Env)
	}
}

func TestToolEnvironmentPreservesRuntimeOrderUnlessExplicitlyPinned(t *testing.T) {
	preferred, later := t.TempDir(), t.TempDir()
	toolExecutableFixture(t, preferred, "node", "#!/bin/sh\nexit 0\n")
	cli := toolExecutableFixture(t, later, "nimote-runtime-order-fixture", "#!/bin/sh\nexit 0\n")
	r := installToolEnvironmentFixture(t, ToolEnvironmentConfig{DisableShell: true, SearchPaths: []string{preferred, later}}, []string{"PATH=/usr/bin:/bin"})
	cmd := exec.Command("nimote-runtime-order-fixture")
	if err := configureToolCommand(cmd, "nimote-runtime-order-fixture"); err != nil {
		t.Fatal(err)
	}
	if first := filepath.SplitList(environmentValue(cmd.Env, "PATH"))[0]; first != preferred {
		t.Fatal("lookup changed user runtime precedence", first)
	}
	toolConfigFixture(t, r.dir, ToolEnvironmentConfig{DisableShell: true, SearchPaths: []string{preferred, later}, Executables: map[string]string{"nimote-runtime-order-fixture": cli}})
	if err := r.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("nimote-runtime-order-fixture")
	if err := configureToolCommand(cmd, "nimote-runtime-order-fixture"); err != nil {
		t.Fatal(err)
	}
	if first := filepath.SplitList(environmentValue(cmd.Env, "PATH"))[0]; first != later {
		t.Fatal("explicit executable pin did not select sibling runtime", first)
	}
}
