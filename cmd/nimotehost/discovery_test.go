// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEnsureSSHCommandIsDiscoverableAndIdempotent(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("checks the macOS zsh SSH environment")
	}
	home := t.TempDir()
	target := filepath.Join(home, ".local", "share", "nimotehost")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "nimotehost"), []byte("#!/bin/sh\nprintf 'ready\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	startup := filepath.Join(home, ".zshenv")
	if err := os.WriteFile(startup, []byte("export EXISTING=value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", "/bin/zsh")
	for range 2 {
		if err := ensureSSHCommand(home, target, "nimotehost"); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(startup)
	if err != nil || !strings.HasPrefix(string(data), "export EXISTING=value\n") || strings.Count(string(data), "# NimoteHost SSH command") != 1 {
		t.Fatalf("startup file: %q %v", data, err)
	}
	cmd := exec.Command("/bin/zsh", "-c", "command -v nimotehost && nimotehost")
	cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), filepath.Join(home, ".local", "bin", "nimotehost")) || !strings.HasSuffix(string(output), "ready\n") {
		t.Fatalf("SSH shell: %q %v; startup=%q", output, err, data)
	}
}

func TestEnsureSSHCommandPreservesExistingCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix command link")
	}
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(bin, "nimotehost")
	if err := os.WriteFile(command, []byte("existing"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", "/bin/zsh")
	if err := ensureSSHCommand(home, filepath.Join(home, "new"), "nimotehost"); err == nil {
		t.Fatal("replaced an existing command")
	}
	data, err := os.ReadFile(command)
	if err != nil || string(data) != "existing" {
		t.Fatalf("existing command changed: %q %v", data, err)
	}
}

func TestEnsureSSHCommandBashRunsBeforeNoninteractiveGuard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix bash startup")
	}
	home := t.TempDir()
	target := filepath.Join(home, ".local", "share", "nimotehost")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "nimotehost"), []byte("#!/bin/sh\necho ready\n"), 0755); err != nil {
		t.Fatal(err)
	}
	startup := filepath.Join(home, ".bashrc")
	original := "case $- in *i*) ;; *) return ;; esac\nexport EXISTING=value\n"
	if err := os.WriteFile(startup, []byte(original), 0640); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", "/bin/bash")
	for range 2 {
		if err := ensureSSHCommand(home, target, "nimotehost"); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(startup)
	if err != nil || !strings.HasSuffix(string(data), original) || strings.Count(string(data), "# NimoteHost SSH command") != 1 {
		t.Fatalf("bash startup: %q %v", data, err)
	}
	info, err := os.Stat(startup)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("bash startup mode: %v %v", info, err)
	}
	cmd := exec.Command("/bin/bash", "-c", "command -v nimotehost && nimotehost")
	cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin", "BASH_ENV=" + startup}
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), filepath.Join(home, ".local", "bin", "nimotehost")) || !strings.HasSuffix(string(output), "ready\n") {
		t.Fatalf("bash command: %q %v", output, err)
	}
}
