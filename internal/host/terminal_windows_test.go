// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

//go:build windows

package host

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowsConPTYReconnect(t *testing.T) {
	if !terminalSupported {
		t.Skip("ConPTY requires Windows 10 1809+")
	}
	m := newTestManager(t)
	s, err := m.Start(StartRequest{Kind: "terminal", Command: "cmd.exe", Args: []string{"/q", "/k"}, Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Resize(s.ID, 30, 100); err != nil {
		t.Fatal(err)
	}
	// A remote service commonly starts with redirected/closed standard input.
	// Keep the shell idle first: it must remain attached to ConPTY until input.
	time.Sleep(1200 * time.Millisecond)
	if got, err := m.Get(s.ID); err != nil || got.State != "running" {
		t.Fatalf("idle ConPTY exited: %+v %v", got, err)
	}
	if err := m.Input(s.ID, []byte("echo host-reconnected\r\nexit\r\n")); err != nil {
		t.Fatal(err)
	}
	got := awaitSession(t, m, s.ID)
	if got.State != "succeeded" {
		t.Fatalf("%+v", got)
	}
	data, _, err := m.Output(s.ID, 0)
	if err != nil || !strings.Contains(string(data), "host-reconnected") {
		t.Fatalf("%q %v", data, err)
	}
}

func TestWindowsBatchLauncher(t *testing.T) {
	m := newTestManager(t)
	dir := filepath.Join(t.TempDir(), "with space")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "adapter.cmd")
	if err := os.WriteFile(path, []byte("@echo wrapper:%~1\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := m.Start(StartRequest{Kind: "task", Command: path, Args: []string{"hello world"}, Directory: dir})
	if err != nil {
		t.Fatal(err)
	}
	got := awaitSession(t, m, s.ID)
	if got.State != "succeeded" {
		t.Fatalf("%+v", got)
	}
	data, _, err := m.Output(s.ID, 0)
	if err != nil || !strings.Contains(string(data), "wrapper:hello world") {
		t.Fatalf("%q %v", data, err)
	}
	if _, _, err := windowsCommand(exec.Command(path, "x&echo unexpected")); err == nil {
		t.Fatal("shell expansion in batch argv accepted")
	}
}
