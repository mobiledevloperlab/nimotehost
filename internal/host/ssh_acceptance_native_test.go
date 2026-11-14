// SPDX-License-Identifier: Apache-2.0
//go:build linux || windows

package host

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// An explicit native acceptance test. It starts a private loopback sshd using
// its own keys/configuration; it never edits the installed service or user keys.
func TestNativeRestrictedSSHAndRevocation(t *testing.T) {
	binary := os.Getenv("NIMOTE_TEST_SSH_HOST_BINARY")
	if binary == "" {
		t.Skip("explicit private native sshd/CLI acceptance")
	}
	sshd := filepath.Join(os.Getenv("WINDIR"), "System32", "OpenSSH", "sshd.exe")
	ssh := filepath.Join(filepath.Dir(sshd), "ssh.exe")
	keygen := filepath.Join(filepath.Dir(sshd), "ssh-keygen.exe")
	login := os.Getenv("USERNAME")
	var uid, gid int
	if runtime.GOOS == "linux" {
		login = os.Getenv("NIMOTE_TEST_SSH_LOGIN")
		account, err := user.Lookup(login)
		if err != nil || os.Geteuid() != 0 {
			t.Fatal("Linux private sshd acceptance requires root and an existing test login", err)
		}
		uid, err = strconv.Atoi(account.Uid)
		if err != nil || uid == 0 {
			t.Fatal("an unprivileged existing login is required", err)
		}
		gid, err = strconv.Atoi(account.Gid)
		if err != nil {
			t.Fatal(err)
		}
		sshd = os.Getenv("NIMOTE_TEST_SSHD_BINARY")
		if sshd == "" {
			sshd = "/usr/sbin/sshd"
		}
		ssh = "/usr/bin/ssh"
		keygen = "/usr/bin/ssh-keygen"
	}
	if login == "" || strings.ContainsAny(login, " \r\n") {
		t.Fatal("invalid test login")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	base := t.TempDir()
	if runtime.GOOS == "linux" {
		var err error
		base, err = os.MkdirTemp("/var/lib", "nimotehost-native-ssh-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(base); err != nil {
				t.Error(err)
			}
		})
		if err := os.Chmod(base, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if strings.ContainsAny(base, " \"\r\n") {
		t.Fatal("this Windows forced-command fixture requires a simple private scratch path")
	}
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	key := filepath.Join(base, "device_key")
	hostkey := filepath.Join(base, "host_key")
	for _, path := range []string{key, hostkey} {
		cmd := exec.CommandContext(ctx, keygen, "-q", "-t", "ed25519", "-N", "", "-f", path)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal("generate temporary SSH key", err, string(out))
		}
	}
	freePort := func() int {
		t.Helper()
		l, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		l.Close()
		return port
	}
	root := filepath.Join(base, "appdata")
	dir := filepath.Join(root, "nimotehost")
	config := Config{Port: freePort(), LogLevel: "error"}
	if err := SaveConfig(dir, config); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "linux" {
		if err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return os.Lchown(path, uid, gid)
		}); err != nil {
			t.Fatal(err)
		}
	}
	ownerCommand := func(callCtx context.Context, args ...string) *exec.Cmd {
		var cmd *exec.Cmd
		if runtime.GOOS == "linux" {
			cmd = exec.CommandContext(callCtx, "/usr/sbin/runuser", append([]string{"-u", login, "--", "/usr/bin/env", "XDG_CONFIG_HOME=" + root, "APPDATA=" + root, binary}, args...)...)
		} else {
			cmd = exec.CommandContext(callCtx, binary, args...)
		}
		cmd.Env = append(os.Environ(), "APPDATA="+root, "XDG_CONFIG_HOME="+root)
		return cmd
	}
	ownerCall := func(req RPCRequest) ([]byte, error) {
		data, _ := json.Marshal(req)
		callCtx, stop := context.WithTimeout(ctx, 8*time.Second)
		defer stop()
		return ownerCommand(callCtx, "rpc-encoded", base64.StdEncoding.EncodeToString(data)).CombinedOutput()
	}
	limits := sshAcceptanceLimits(t)
	hostLog := &agentDiagnostics{}
	hostProcess, _, err := startProcess(ownerCommand(ctx, "start"), false, hostLog, limits)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hostProcess.Kill(); _, _ = hostProcess.Wait(); _ = hostProcess.Close() }()
	deadline := time.Now().Add(8 * time.Second)
	for {
		if _, err := ownerCall(RPCRequest{Method: "GET", Path: "/v1/status"}); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("private Host not ready", hostLog.text())
		}
		time.Sleep(25 * time.Millisecond)
	}
	sessions := []Session{}
	command, arguments := "cmd.exe", []string{"/d", "/c", "ping -n 60 127.0.0.1 >nul"}
	if runtime.GOOS == "linux" {
		command, arguments = "/bin/sh", []string{"-c", "sleep 60"}
	}
	for range 2 {
		body, _ := json.Marshal(StartRequest{Kind: "task", Command: command, Args: arguments, Directory: workspace})
		data, err := ownerCall(RPCRequest{Method: "POST", Path: "/v1/sessions", Body: body})
		if err != nil {
			t.Fatal(err)
		}
		var record Session
		if json.Unmarshal(data, &record) != nil || record.ID == "" {
			t.Fatal("invalid session reply")
		}
		sessions = append(sessions, record)
	}
	data, err := ownerCommand(ctx, "devices", "add-scoped", "native SSH fixture", "session_observer", sessions[0].ID).CombinedOutput()
	var d Device
	if err != nil || json.Unmarshal(data, &d) != nil || d.ID == "" {
		t.Fatal("create scoped SSH device", err, string(data))
	}
	bridge := filepath.Join(base, "bridge.cmd")
	write(bridge, "@echo off\r\nset \"APPDATA="+root+"\"\r\n\""+binary+"\" device-bridge "+d.ID+"\r\n")
	if runtime.GOOS == "linux" {
		bridge = filepath.Join(base, "bridge.sh")
		write(bridge, "#!/bin/sh\nexport XDG_CONFIG_HOME='"+root+"'\nexec '"+binary+"' device-bridge "+d.ID+"\n")
		if err := os.Chmod(bridge, 0755); err != nil {
			t.Fatal(err)
		}
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	authorized := filepath.Join(base, "authorized_keys")
	write(authorized, "restrict,command=\""+filepath.ToSlash(bridge)+"\" "+strings.TrimSpace(string(pub))+"\n")
	if runtime.GOOS == "linux" {
		if err := os.Chmod(authorized, 0644); err != nil {
			t.Fatal(err)
		}
	}
	port := freePort()
	cfg := filepath.Join(base, "sshd_config")
	write(cfg, fmt.Sprintf("Port %d\nListenAddress 127.0.0.1\nHostKey %s\nPidFile %s\nAuthorizedKeysFile %s\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nPubkeyAuthentication yes\nStrictModes yes\nAllowUsers %s\nAllowTcpForwarding yes\nPermitTTY yes\nLogLevel DEBUG3\nSubsystem sftp internal-sftp\n", port, filepath.ToSlash(hostkey), filepath.ToSlash(filepath.Join(base, "sshd.pid")), filepath.ToSlash(authorized), login))
	if out, err := exec.CommandContext(ctx, sshd, "-t", "-f", cfg).CombinedOutput(); err != nil {
		t.Fatal("private sshd config rejected", err, string(out))
	}
	logs := &agentDiagnostics{}
	daemon := exec.Command(sshd, "-D", "-e", "-f", cfg)
	daemon.Dir = base
	process, _, err := startProcess(daemon, false, logs, limits)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Kill(); _, _ = process.Wait(); _ = process.Close() }()
	publicHost, err := os.ReadFile(hostkey + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(publicHost))
	if len(fields) < 2 {
		t.Fatal("invalid host public key")
	}
	known := filepath.Join(base, "known_hosts")
	write(known, fmt.Sprintf("[127.0.0.1]:%d %s %s\n", port, fields[0], fields[1]))
	nullConfig := "NUL"
	if runtime.GOOS == "linux" {
		nullConfig = "/dev/null"
	}
	common := []string{"-F", nullConfig, "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + known, "-o", "LogLevel=ERROR", "-o", "ConnectTimeout=3", "-i", key, "-p", strconv.Itoa(port)}
	run := func(extra []string, command string) ([]byte, error) {
		args := append([]string{}, common...)
		for _, flag := range extra {
			if flag == "-W" || flag == "-tt" {
				for i, value := range args {
					if value == "LogLevel=ERROR" {
						args[i] = "LogLevel=DEBUG1"
					}
				}
			}
		}
		args = append(args, extra...)
		args = append(args, login+"@127.0.0.1")
		if command != "" {
			args = append(args, command)
		}
		callCtx, stop := context.WithTimeout(ctx, 8*time.Second)
		defer stop()
		return exec.CommandContext(callCtx, ssh, args...).CombinedOutput()
	}
	var output []byte
	deadline = time.Now().Add(8 * time.Second)
	for {
		output, err = run(nil, "status")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("native key authentication/forced command failed: %v\n%s\nsshd: %s", err, output, logs.text())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !json.Valid(output) {
		t.Fatalf("forced status did not return Host JSON: %s", output)
	}
	rpc := func(path string) ([]byte, error) {
		data, _ := json.Marshal(RPCRequest{Method: "GET", Path: path})
		return run(nil, "rpc-encoded "+base64.StdEncoding.EncodeToString(data))
	}
	if output, err = rpc("/v1/sessions/" + sessions[0].ID); err != nil || !strings.Contains(string(output), sessions[0].ID) {
		t.Fatal("scoped SSH read failed", err, string(output))
	}
	if output, err = rpc("/v1/sessions/" + sessions[1].ID); err == nil {
		t.Fatal("SSH crossed session scope", string(output))
	}
	marker := filepath.Join(workspace, "ssh-must-not-write")
	shellCommand := "cmd.exe /c echo unauthorized > " + marker
	if runtime.GOOS == "linux" {
		shellCommand = "printf unauthorized > " + marker
	}
	if output, err = run(nil, shellCommand); err == nil || !strings.Contains(string(output), "device key permits Host") {
		t.Fatal("forced key executed a shell command", string(output))
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("SSH shell command changed files", err)
	}
	if output, err = run([]string{"-s"}, "sftp"); err == nil {
		t.Fatal("forced key opened SFTP", string(output))
	}
	if output, err = run([]string{"-W", config.Address()}, ""); err == nil || !strings.Contains(strings.ToLower(string(output)), "prohibited") {
		t.Fatal("restrict did not reject forwarding", err, string(output))
	}
	if output, _ = run([]string{"-tt"}, "status"); !strings.Contains(strings.ToLower(string(output)), "pty allocation request failed") {
		t.Fatal("restrict did not reject PTY", string(output))
	}
	if data, err := ownerCommand(ctx, "devices", "revoke", d.ID).CombinedOutput(); err != nil {
		t.Fatal("revoke scoped device", err, string(data))
	}
	if output, err = run(nil, "status"); err == nil {
		t.Fatal("revoked device still used SSH bridge", string(output))
	}
	if _, err := ownerCall(RPCRequest{Method: "GET", Path: "/v1/status"}); err != nil {
		t.Fatal("device revocation affected owner", err)
	}
	t.Log("real native SSH: forced command, scoped RPC, shell/SFTP/forwarding/PTY denial and persisted revocation passed")
}
