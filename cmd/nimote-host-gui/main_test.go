// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"fyne.io/fyne/v2/test"
	"github.com/mobiledevloperlab/nimote-host/internal/desktop"
	"github.com/mobiledevloperlab/nimote-host/internal/host"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestWindowCloseReopenAndQuitPreserveHostTask(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "nimote-host")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "../../cmd/nimote-host")
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("CLI build: %s %v", b, err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("APPDATA", home)
	t.Setenv("USERPROFILE", home)
	dir, err := host.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	if err = host.SaveConfig(dir, host.Config{Port: port, LogLevel: "info"}); err != nil {
		t.Fatal(err)
	}
	m := &desktop.Manager{Dir: dir, Binary: binary}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = m.Change(ctx, "start"); err != nil {
		t.Fatal(err)
	}
	defer m.Change(context.Background(), "stop")
	command := "/bin/sh"
	args := []string{"-c", "sleep 60"}
	if runtime.GOOS == "windows" {
		command = "cmd.exe"
		args = []string{"/c", "ping -n 60 127.0.0.1 >NUL"}
	}
	body, _ := json.Marshal(host.StartRequest{Kind: "task", Command: command, Args: args, Directory: home})
	raw, err := host.Call(ctx, dir, host.RPCRequest{Method: "POST", Path: "/v1/sessions", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	var session host.Session
	if err = json.Unmarshal(raw, &session); err != nil {
		t.Fatal(err)
	}
	a := test.NewApp()
	defer a.Quit()
	first, cleanup := newManagerWindow(a, m, false)
	first.Show()
	first.Hide()
	first.Show()
	first.Close()
	cleanup()
	second, cleanup := newManagerWindow(a, m, false)
	second.Show()
	second.Close()
	cleanup()
	a.Quit()
	s, err := m.Status(ctx)
	if err != nil || s.State != "Running" {
		t.Fatalf("GUI lifetime killed Host: %v %v", s, err)
	}
	raw, err = host.Call(ctx, dir, host.RPCRequest{Method: "GET", Path: "/v1/sessions/" + session.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &session); err != nil || session.State != "running" {
		t.Fatalf("GUI lifetime interrupted task: %s %v", raw, err)
	}
}
