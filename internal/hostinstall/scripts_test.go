// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package hostinstall

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("NIMOTEHOST_INSTALL_TEST_HELPER") == "1" {
		if len(os.Args) > 1 && os.Args[1] == "run" {
			fmt.Fprintln(os.Stderr, "build fixture failure")
			os.Exit(23)
		}
		fmt.Println(strings.Join(os.Args[1:], "\n"))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// Exercise the real entrypoint with a native executable and paths containing
// spaces, without installing a service or relying on Go on the end user's PATH.
func TestDistributionInstallerInvokesSetup(t *testing.T) {
	if runtime.GOOS != "windows" && os.Geteuid() == 0 {
		t.Skip("installer deliberately rejects root")
	}
	root := filepath.Join(t.TempDir(), "bundle with spaces")
	scripts := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scripts, 0755); err != nil {
		t.Fatal(err)
	}
	binary := "nimotehost"
	script := "install.sh"
	shell := "sh"
	if runtime.GOOS == "windows" {
		binary += ".exe"
		script = "install.ps1"
		shell = "powershell.exe"
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, binary), data, 0755); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join("..", "..", "scripts", script))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SHA256SUMS"), []byte("fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(scripts, script)
	if err := os.WriteFile(scriptPath, data, 0755); err != nil {
		t.Fatal(err)
	}
	args := []string{scriptPath}
	if runtime.GOOS == "windows" {
		args = []string{"-NoProfile", "-NonInteractive", "-File", scriptPath}
	}
	cmd := exec.Command(shell, args...)
	cmd.Env = append(os.Environ(), "NIMOTEHOST_INSTALL_TEST_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "setup" {
		t.Fatalf("installer failed: %v\n%s", err, output)
	}
	args = append(args, "--help")
	if runtime.GOOS == "windows" {
		args[len(args)-1] = "-Help"
	}
	output, err = exec.Command(shell, args...).CombinedOutput()
	if err != nil || !strings.Contains(string(output), "Usage:") {
		t.Fatalf("help failed: %v\n%s", err, output)
	}
	// A source checkout may contain a standalone `go build` binary. It must still
	// build a full distribution, and a build failure must never start installation.
	if err := os.Remove(filepath.Join(root, "SHA256SUMS")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tools", "install"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tools", "install", "main.go"), []byte("fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	goName := "go"
	if runtime.GOOS == "windows" {
		goName += ".exe"
	}
	fakeBin := filepath.Join(root, "fake-bin")
	if err := os.Mkdir(fakeBin, 0755); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, goName), data, 0755); err != nil {
		t.Fatal(err)
	}
	args = args[:len(args)-1] // Remove the help argument.
	cmd = exec.Command(shell, args...)
	cmd.Env = append(os.Environ(), "NIMOTEHOST_INSTALL_TEST_HELPER=1")
	for i, value := range cmd.Env {
		if strings.EqualFold(strings.SplitN(value, "=", 2)[0], "PATH") {
			cmd.Env[i] = "PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")
		}
	}
	output, err = cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "build fixture failure") {
		t.Fatalf("source build failure did not stop setup: %v\n%s", err, output)
	}

}
