// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"encoding/json"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/mobiledevloperlab/nimote-host/internal/desktop"
	"github.com/mobiledevloperlab/nimote-host/internal/host"
	"net"
	"os/exec"
	"path/filepath"
	"reflect"
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
	// Switching the display language must rebuild only the view, not restart
	// Host or interrupt its active process.
	var navigation *widget.List
	visitObjects(first.Content(), func(obj fyne.CanvasObject) {
		if tabs, ok := obj.(*widget.List); ok && tabs.Length() == len(navigationKeys) {
			navigation = tabs
		}
	})
	if navigation == nil {
		t.Fatal("main navigation is missing")
	}
	navigation.Select(3)
	var languageSelect *widget.Select
	visitObjects(first.Content(), func(obj fyne.CanvasObject) {
		if selectBox, ok := obj.(*widget.Select); ok && reflect.DeepEqual(selectBox.Options, languageNames) {
			languageSelect = selectBox
		}
	})
	if languageSelect == nil {
		t.Fatal("language control is missing")
	}
	languageSelect.SetSelectedIndex(2)
	if a.Preferences().String("language") != "es" {
		t.Fatal("language preference did not persist")
	}
	afterLanguage, err := host.Call(ctx, dir, host.RPCRequest{Method: "GET", Path: "/v1/status"})
	if err != nil {
		t.Fatal(err)
	}
	var identity struct {
		ID string `json:"host_id"`
	}
	if err := json.Unmarshal(afterLanguage, &identity); err != nil || identity.ID == "" {
		t.Fatal("invalid Host identity", err)
	}
	first.Hide()
	first.Show()
	first.Close()
	cleanup()
	second, cleanup := newManagerWindow(a, m, false)
	second.Show()
	second.Close()
	cleanup()
	a.Quit()
	afterQuit, err := host.Call(ctx, dir, host.RPCRequest{Method: "GET", Path: "/v1/status"})
	if err != nil {
		t.Fatal(err)
	}
	var sameIdentity struct {
		ID string `json:"host_id"`
	}
	if json.Unmarshal(afterQuit, &sameIdentity) != nil || sameIdentity.ID != identity.ID {
		t.Fatal("GUI lifecycle replaced the Host identity")
	}
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
