// SPDX-License-Identifier: Apache-2.0
//go:build !windows

package host

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

func platformRegisteredTool(string) string { return "" }

func platformToolDirectories(base []string) []string {
	home := environmentValue(base, "HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	paths := []string{"/usr/local/bin", "/usr/local/sbin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"}
	if runtime.GOOS == "darwin" {
		paths = append([]string{"/opt/homebrew/bin", "/opt/homebrew/sbin", "/opt/local/bin", "/opt/local/sbin"}, paths...)
	}
	if filepath.IsAbs(home) {
		for _, dir := range []string{".local/bin", "bin", ".cargo/bin", ".volta/bin", ".asdf/shims", ".local/share/mise/shims", ".bun/bin", ".local/share/pnpm"} {
			paths = append(paths, filepath.Join(home, dir))
		}
	}
	for _, entry := range []struct{ key, suffix string }{{"VOLTA_HOME", "bin"}, {"ASDF_DATA_DIR", "shims"}, {"MISE_DATA_DIR", "shims"}, {"CARGO_HOME", "bin"}, {"BUN_INSTALL", "bin"}, {"PNPM_HOME", ""}} {
		if root := environmentValue(base, entry.key); filepath.IsAbs(root) {
			paths = append(paths, filepath.Join(root, entry.suffix))
		}
	}
	return paths
}

func accountToolShell(ctx context.Context, base []string) string {
	// Prefer the account database: SHELL can be missing or inherited from a
	// different launcher. Do not accept workspace-local or relative shells.
	current, err := user.Current()
	if err == nil {
		if data, err := os.ReadFile("/etc/passwd"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				fields := strings.Split(line, ":")
				if len(fields) >= 7 && fields[2] == current.Uid && validToolPath(fields[6]) {
					return fields[6]
				}
			}
		}
		if runtime.GOOS == "darwin" {
			probeCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			cmd := exec.CommandContext(probeCtx, "/usr/bin/dscl", ".", "-read", "/Users/"+current.Username, "UserShell")
			var out evidenceBuffer
			out.limit = 4096
			cmd.Stdout, cmd.Stderr = &out, io.Discard
			cmd.WaitDelay = 100 * time.Millisecond
			if cmd.Run() == nil {
				_, shell, ok := strings.Cut(strings.TrimSpace(out.String()), "UserShell:")
				if ok && validToolPath(strings.TrimSpace(shell)) {
					return strings.TrimSpace(shell)
				}
			}
		}
	}
	if shell := environmentValue(base, "SHELL"); validToolPath(shell) {
		return shell
	}
	if runtime.GOOS == "darwin" {
		return "/bin/zsh"
	}
	return "/bin/sh"
}

func probeToolEnvironment(ctx context.Context, base []string, disabled bool) ([]string, string) {
	if disabled {
		return nil, "disabled"
	}
	shell := accountToolShell(ctx, base)
	return probeToolShell(ctx, base, shell)
}

func probeToolShell(ctx context.Context, base []string, shell string) ([]string, string) {
	// A random marker excludes banners and startup diagnostics. env is invoked
	// through an absolute path; no tool names or user arguments enter shell code.
	marker := "nimotehost-" + rand.Text()
	script := "printf '\\000" + marker + "\\000'; /usr/bin/env -0"
	var args []string
	switch filepath.Base(shell) {
	case "bash", "zsh", "fish", "ksh":
		args = []string{"-i", "-l", "-c", script}
	case "sh", "dash":
		args = []string{"-l", "-c", script}
	default:
		return nil, "unsupported_shell"
	}
	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Env = base
	if home := environmentValue(base, "HOME"); filepath.IsAbs(home) {
		cmd.Dir = home
	}
	cmd.Stdin, cmd.Stderr = nil, io.Discard
	var output evidenceBuffer
	output.limit = 256 << 10
	cmd.Stdout = &output
	cmd.WaitDelay = 100 * time.Millisecond
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
	if err := cmd.Start(); err != nil {
		return nil, "failed"
	}
	err := cmd.Wait()
	// Startup scripts can leave background children holding output pipes even
	// after the shell exits. Clean the discovery group on every exit path.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "timeout"
		}
		return nil, "failed"
	}
	_, data, ok := bytes.Cut(output.Bytes(), []byte("\x00"+marker+"\x00"))
	if !ok || len(data) == 0 || data[len(data)-1] != 0 {
		return nil, "invalid_output"
	}
	var env []string
	for _, entry := range bytes.Split(data[:len(data)-1], []byte{0}) {
		key, value, ok := strings.Cut(string(entry), "=")
		if !ok || strings.ContainsAny(key, "\r\n") {
			continue
		}
		if key == "PATH" {
			env = append(env, key+"="+value)
			continue
		}
		for _, allowed := range toolRuntimeVariables {
			if key == allowed {
				env = append(env, key+"="+value)
				break
			}
		}
	}
	if environmentValue(env, "PATH") == "" {
		return nil, "invalid_output"
	}
	return env, "ok"
}
