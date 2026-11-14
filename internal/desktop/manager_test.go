// SPDX-License-Identifier: Apache-2.0
package desktop

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/mobiledevloperlab/nimotehost/internal/host"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// This helper represents a GUI process that starts Host and then exits completely.
func TestManagerProcess(t *testing.T) {
	if os.Getenv("NIMOTE_DESKTOP_TEST_PROCESS") != "1" {
		return
	}
	m := &Manager{Dir: os.Getenv("NIMOTE_DESKTOP_TEST_DIR"), Binary: os.Getenv("NIMOTE_DESKTOP_TEST_BINARY")}
	if err := m.Change(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
}
func TestIndependentHostLifecycle(t *testing.T) {
	if os.Getenv("NIMOTE_DESKTOP_TEST_PROCESS") == "1" {
		return
	}
	binary := filepath.Join(t.TempDir(), "nimotehost")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", binary, "../../cmd/nimotehost")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v %s", err, b)
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
	if runtime.GOOS == "windows" {
		// Exercise GUI start and restart with login startup registered, without
		// touching the real user's scheduled task.
		if err = os.WriteFile(filepath.Join(dir, "service.xml"), []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	m := &Manager{Dir: dir, Binary: binary}
	defer func() { _ = m.Change(context.Background(), "stop") }()
	helper := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestManagerProcess$")
	helper.Env = append(os.Environ(), "NIMOTE_DESKTOP_TEST_PROCESS=1", "NIMOTE_DESKTOP_TEST_DIR="+dir, "NIMOTE_DESKTOP_TEST_BINARY="+binary)
	if b, err := helper.CombinedOutput(); err != nil {
		t.Fatalf("manager start: %v %s", err, b)
	}
	// Helper has exited; a fresh manager must still attach to the same Host.
	other := &Manager{Dir: dir, Binary: binary}
	s, err := other.Status(ctx)
	if err != nil || s.State != "Running" {
		t.Fatalf("Host died with manager: %v %v", s, err)
	}
	device, err := host.CreateDevice(dir, "restricted CLI integration")
	if err != nil {
		t.Fatal(err)
	}
	bridge := func(request string) error {
		encoded := base64.StdEncoding.EncodeToString([]byte(request))
		cmd := exec.CommandContext(ctx, binary, "device-bridge", device.ID)
		cmd.Env = append(os.Environ(), "SSH_ORIGINAL_COMMAND=nimotehost rpc-encoded "+encoded)
		return cmd.Run()
	}
	if err = bridge(`{"method":"GET","path":"/v1/status"}`); err != nil {
		t.Fatalf("restricted key bridge: %v", err)
	}
	if bridge(`{"method":"POST","path":"/v1/shutdown"}`) == nil {
		t.Fatal("restricted bridge stopped Host")
	}
	if err = host.RevokeDevice(dir, device.ID); err != nil {
		t.Fatal(err)
	}
	if bridge(`{"method":"GET","path":"/v1/status"}`) == nil {
		t.Fatal("revoked bridge still accepted")
	}
	before, err := host.Call(ctx, dir, host.RPCRequest{Method: "GET", Path: "/v1/status"})
	if err != nil {
		t.Fatal(err)
	}

	tokenPath := filepath.Join(dir, "token")
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(tokenPath, []byte(strings.Repeat("0", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	_, authErr := other.Status(ctx)
	startErr := other.Change(ctx, "start")
	restoreErr := os.WriteFile(tokenPath, token, 0600)
	if restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if authErr == nil || startErr == nil {
		t.Fatal("authentication failure was treated as stopped")
	}
	if err = os.Remove(tokenPath); err != nil {
		t.Fatal(err)
	}
	_, missingCredentialErr := other.Status(ctx)
	restoreErr = os.WriteFile(tokenPath, token, 0600)
	if restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if missingCredentialErr == nil {
		t.Fatal("missing credential for live Host was treated as stopped")
	}

	if err = other.Change(ctx, "start"); err != nil {
		t.Fatal(err)
	}
	after, _ := host.Call(ctx, dir, host.RPCRequest{Method: "GET", Path: "/v1/status"})
	var beforeStatus, afterStatus map[string]json.RawMessage
	if json.Unmarshal(before, &beforeStatus) != nil || json.Unmarshal(after, &afterStatus) != nil || len(beforeStatus["instance_id"]) <= 2 {
		t.Fatal("missing daemon instance identity")
	}
	// Free disk space changes independently of Host identity. All stable fields,
	// including a random per-daemon instance ID, must remain identical.
	delete(beforeStatus, "resources")
	delete(afterStatus, "resources")
	beforeStable, _ := json.Marshal(beforeStatus)
	afterStable, _ := json.Marshal(afterStatus)
	if string(beforeStable) != string(afterStable) {
		t.Fatal("start replaced running Host")
	}
	if _, err = other.Sessions(ctx); err != nil {
		t.Fatal(err)
	}
	if err = other.SaveConfig(host.Config{Port: port, LogLevel: "warn"}); err == nil {
		t.Fatal("changed running config")
	}
	// A second CLI daemon must fail without disrupting the first.
	duplicate := exec.CommandContext(ctx, binary, "start")
	if duplicate.Run() == nil {
		t.Fatal("duplicate daemon accepted")
	}
	if _, err = other.Status(ctx); err != nil {
		t.Fatal(err)
	}
	if err = other.Change(ctx, "restart"); err != nil {
		t.Fatal(err)
	}
	restarted, err := host.Call(ctx, dir, host.RPCRequest{Method: "GET", Path: "/v1/status"})
	if err != nil {
		t.Fatal(err)
	}
	var restartedStatus map[string]json.RawMessage
	if json.Unmarshal(restarted, &restartedStatus) != nil || string(restartedStatus["instance_id"]) == string(beforeStatus["instance_id"]) {
		t.Fatal("restart did not create a new daemon instance")
	}
	if err = other.Change(ctx, "stop"); err != nil {
		t.Fatal(err)
	}
	if s, err = other.Status(ctx); err != nil || s.State != "Stopped" {
		t.Fatalf("stop: %v %v", s, err)
	}
	if err = other.SaveConfig(host.Config{Port: port, LogLevel: "warn"}); err != nil {
		t.Fatal(err)
	}
	c, _ := host.ReadConfig(dir)
	if c.LogLevel != "warn" {
		t.Fatal("config not shared")
	}
	// Foreign listener or broken IPC must produce an explicit readiness error.
	blocker, err := net.Listen("tcp4", c.Address())
	if err != nil {
		t.Fatal(err)
	}
	if err = other.Change(ctx, "start"); err == nil {
		blocker.Close()
		t.Fatal("occupied port accepted")
	}
	blocker.Close()
}

func TestUnavailableDistinguishesRefusedFromOtherFailures(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	conn, err := net.DialTimeout("tcp4", address, time.Second)
	if conn != nil {
		conn.Close()
		t.Fatal("unexpected listener")
	}
	if !Unavailable(err) {
		t.Fatalf("refused socket not recognized: %v", err)
	}
	for _, err := range []error{context.DeadlineExceeded, os.ErrPermission} {
		if Unavailable(err) {
			t.Fatalf("treated failure as stopped: %v", err)
		}
	}
}
