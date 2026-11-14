// SPDX-License-Identifier: Apache-2.0
//go:build linux

package host

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestIdentityProbeChild(t *testing.T) {
	var args []string
	for i, arg := range os.Args {
		if arg == "--identity-probe" {
			args = os.Args[i+1:]
		}
	}
	if len(args) != 3 {
		return
	}
	uid, _ := strconv.Atoi(args[0])
	owner, _ := strconv.Atoi(args[1])
	fail := func() { os.Exit(9) }
	if os.Getuid() != uid || os.Geteuid() != uid || os.Getgid() != uid || os.Getenv("NIMOTE_ROOT_SENTINEL") != "" {
		fail()
	}
	groups, err := os.Getgroups()
	if err != nil || len(groups) != 0 {
		fail()
	}
	n, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	if err != nil || n != 1 {
		fail()
	}
	if _, err := os.ReadFile(args[2]); !errors.Is(err, os.ErrPermission) {
		fail()
	}
	if err := syscall.Kill(owner, 0); !errors.Is(err, syscall.EPERM) {
		fail()
	}
	if _, _, errno := unix.Syscall(unix.SYS_IO_URING_SETUP, 0, 0, 0); errno != unix.EPERM {
		fail()
	}
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err == nil {
		unix.Close(fd)
		fail()
	}
	fd, err = unix.Socket(unix.AF_INET, unix.SOCK_STREAM, 0)
	if err != nil {
		fail()
	}
	unix.Close(fd)
	os.Exit(0)
}
func rootScopeFixture(t *testing.T) string {
	t.Helper()
	id, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("/sys/fs/cgroup", "nimotehost-test-"+id)
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal("root containment acceptance requires writable cgroup v2 delegation", err)
	}
	t.Cleanup(func() {
		_ = os.WriteFile(filepath.Join(root, "cgroup.kill"), []byte("1"), 0600)
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), "nimotehost-") {
				_ = os.Remove(filepath.Join(root, e.Name()))
			}
		}
		if err := os.Remove(root); err != nil {
			t.Error("test cgroup cleanup failed", err)
		}
	})
	for key, value := range map[string]string{"cgroup.subtree_control": "+memory +pids", "memory.max": "1073741824", "pids.max": "256"} {
		if err := os.WriteFile(filepath.Join(root, key), []byte(value), 0600); err != nil {
			t.Fatal("required temporary cgroup controllers unavailable", err)
		}
	}
	return root
}
func TestRootIsolatedExecutionBoundary(t *testing.T) {
	if os.Getenv("NIMOTE_TEST_ROOT_ISOLATION") != "1" {
		t.Skip("explicit root-only cgroup/UID acceptance; no default system changes")
	}
	if os.Geteuid() != 0 {
		t.Fatal("root-only acceptance was requested without root")
	}
	if os.Getenv("NIMOTE_TEST_ROOT_STAGE") != "1" {
		base, err := os.MkdirTemp("/tmp", "nimotehost-root-helper-")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(base)
		if err := os.Chmod(base, 0755); err != nil {
			t.Fatal(err)
		}
		source, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		in, err := os.Open(source)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		target := filepath.Join(base, "host-test-helper")
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(out, in)
		closeErr := out.Close()
		if err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
		cmd := exec.Command(target, "-test.run=^TestRootIsolatedExecutionBoundary$", "-test.v", "-test.timeout=3m")
		cmd.Env = append(os.Environ(), "NIMOTE_TEST_ROOT_STAGE=1")
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("root boundary acceptance: %v\n%s", err, data)
		}
		t.Log("dedicated UID, no-new-privileges, broker isolation, cgroup revocation and cross-session denial passed")
		return
	}
	delegation := rootScopeFixture(t)
	base, err := os.MkdirTemp("/tmp", "nimotehost-root-scope-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	if err := os.Chmod(base, 0755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "host")
	if err := privateDir(dir); err != nil {
		t.Fatal(err)
	}
	owner, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(dir, "token")
	if err := os.WriteFile(secret, []byte(owner), 0600); err != nil {
		t.Fatal(err)
	}
	limits := DefaultResourceLimits()
	limits.CgroupRoot = delegation
	limits.MemoryBytes = 256 << 20
	limits.MaxProcesses = 64
	missingCPU := limits
	missingCPU.CPUPercent = 10
	if err := missingCPU.Validate(); err == nil || resourceEnforcement(limits).HardCPU {
		t.Fatal("undelegated CPU controller reported as available", err)
	}
	if !resourceEnforcement(limits).IndependentIdentity {
		t.Fatal("configured private backend not detected")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Public execution is required, public replacement must always be rejected.
	if err := os.Chmod(executable, 0777); err != nil {
		t.Fatal(err)
	}
	rejected := validateIdentityBackend(limits)
	restoreErr := os.Chmod(executable, 0755)
	if rejected == nil || restoreErr != nil {
		t.Fatal("writable guardian accepted", rejected, restoreErr)
	}
	m, err := NewManagerWithLimits(filepath.Join(dir, "sessions"), limits)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	api := &Server{dir: dir, manager: m, token: owner}
	t.Setenv("NIMOTE_ROOT_SENTINEL", "must-not-enter-worker")
	uid := uint32(1500000000 + time.Now().UnixNano()%100000000)
	makeSession := func(n int) Session {
		identity := ExecutionIdentity{UID: uid + uint32(n), GID: uid + uint32(n)}
		workspace := filepath.Join(base, "workspace-"+strconv.Itoa(n))
		if err := os.Mkdir(workspace, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(workspace, int(identity.UID), int(identity.GID)); err != nil {
			t.Fatal(err)
		}
		s, err := m.Start(StartRequest{Kind: "terminal", Command: "/bin/sh", Args: []string{"-i"}, Directory: workspace, Identity: &identity})
		if err != nil {
			t.Fatal("isolated spawn failed", err)
		}
		return s
	}
	first, second := makeSession(0), makeSession(1)
	for _, record := range []Session{first, second} {
		scope, err := loadSessionScope(m.dir, record.ID)
		if err != nil || scope == nil {
			t.Fatal("missing ownership evidence", err)
		}
		for name, want := range map[string]string{"memory.max": "268435456", "memory.swap.max": "0", "memory.oom.group": "1", "pids.max": "64"} {
			value, err := os.ReadFile(filepath.Join(delegation, scope.Name, name))
			if err != nil || strings.TrimSpace(string(value)) != want {
				t.Fatal("kernel quota missing", name, err)
			}
		}
	}
	a, err := CreateControllerDevice(dir, "isolated A", []string{first.ID})
	if err != nil {
		t.Fatal(err)
	}
	b, err := CreateControllerDevice(dir, "isolated B", []string{second.ID})
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := deviceCredential(DeviceContext(context.Background(), a.ID), dir)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := deviceCredential(DeviceContext(context.Background(), b.ID), dir)
	if err != nil {
		t.Fatal(err)
	}
	body := func(text, id string) string {
		raw, _ := json.Marshal(map[string]any{"data": []byte(text), "request_id": id})
		return string(raw)
	}
	executable, _ = os.Executable()
	command := executable + " -test.run=^TestIdentityProbeChild$ -- --identity-probe " + strconv.FormatUint(uint64(uid), 10) + " " + strconv.Itoa(os.Getpid()) + " " + secret + " && printf ok > boundary-ok\n"
	path := "/v1/sessions/" + first.ID + "/input"
	if w := deviceRequest(api, token, "POST", path, body(command, "probe")); w.Code != 200 {
		t.Fatal("controller could not use isolated terminal", w.Code, w.Body.String())
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if raw, _ := os.ReadFile(filepath.Join(first.Directory, "boundary-ok")); string(raw) == "ok" {
			break
		}
		if time.Now().After(deadline) {
			data, _, _ := m.Output(first.ID, 0)
			t.Fatal("native isolation probe did not succeed", string(data))
		}
		time.Sleep(25 * time.Millisecond)
	}
	for _, request := range []struct{ method, path string }{{"POST", "/v1/sessions"}, {"POST", "/v1/shutdown"}, {"GET", "/v1/agent-history"}, {"POST", "/v1/sessions/" + second.ID + "/input"}, {"GET", "/v1/sessions/" + second.ID + "/output"}} {
		if w := deviceRequest(api, token, request.method, request.path, `{}`); w.Code != 403 {
			t.Fatal("controller escaped its authority", request, w.Code)
		}
	}
	escaped := "setsid /bin/sh -c 'while :; do printf x >> escaped-heartbeat; sleep 0.02; done' >/dev/null 2>&1 &\n"
	if w := deviceRequest(api, token, "POST", path, body(escaped, "escape")); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		if stat, err := os.Stat(filepath.Join(first.Directory, "escaped-heartbeat")); err == nil && stat.Size() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("escaped-process fixture did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := RevokeDevice(dir, a.ID); err != nil {
		t.Fatal(err)
	}
	if w := deviceRequest(api, token, "POST", path, body(command, "probe")); w.Code != 401 {
		t.Fatal("revoked controller replayed a receipt", w.Code)
	}
	stopped := awaitSession(t, m, first.ID)
	if !strings.Contains(stopped.Error, "revoked") {
		t.Fatal("revocation did not terminate owned cgroup", stopped)
	}
	before, _ := os.Stat(filepath.Join(first.Directory, "escaped-heartbeat"))
	time.Sleep(150 * time.Millisecond)
	after, _ := os.Stat(filepath.Join(first.Directory, "escaped-heartbeat"))
	if before == nil || after == nil || before.Size() != after.Size() {
		t.Fatal("setsid descendant survived controller revocation")
	}
	if w := deviceRequest(api, other, "GET", "/v1/sessions/"+second.ID, ""); w.Code != 200 {
		t.Fatal("revocation affected another controller", w.Code)
	}
	if _, err := CreateControllerDevice(dir, "reuse", []string{first.ID}); err == nil {
		t.Fatal("controller UID reassigned after revocation")
	}
}
