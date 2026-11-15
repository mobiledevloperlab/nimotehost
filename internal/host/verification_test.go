// SPDX-License-Identifier: Apache-2.0
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

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %s %v", args, b, e)
	}
	return strings.TrimSpace(string(b))
}
func testRepo(t *testing.T) string {
	t.Helper()
	// Git evidence re-executes this test binary as its process supervisor.
	// Keep race detection enabled without adding a one-second exit delay to
	// every Git command and exhausting the production evidence deadlines.
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	if _, e := exec.LookPath("git"); e != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	gitTest(t, dir, "init")
	gitTest(t, dir, "config", "user.email", "test@example.invalid")
	gitTest(t, dir, "config", "user.name", "Test")
	if e := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("before\n"), 0600); e != nil {
		t.Fatal(e)
	}
	gitTest(t, dir, "add", "file.txt")
	gitTest(t, dir, "commit", "-m", "initial")
	return dir
}
func TestVerificationRequiresActualTestExecutionAndVersion(t *testing.T) {
	m := newTestManager(t)
	req := childRequest(t, "output")
	req.Directory = testRepo(t)
	s, e := m.Start(req)
	if e != nil {
		t.Fatal(e)
	}
	awaitSession(t, m, s.ID)
	ctx := context.Background()
	r, e := m.Verification(ctx, s.ID)
	if e != nil || r.Status != "not_run" || r.Stale || len(r.Tests) != 0 || r.Version.Head == "" {
		t.Fatalf("%+v %v", r, e)
	}
	test := childRequest(t, "output")
	r, e = m.StartVerification(ctx, s.ID, []TestCommand{{Command: test.Command, Args: test.Args}})
	if e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r, e = m.Verification(ctx, s.ID)
		if e != nil {
			t.Fatal(e)
		}
		if r.Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r.Status != "passed" || r.Stale || r.Tests[0].ExitCode == nil || *r.Tests[0].ExitCode != 0 || r.Tests[0].SessionID == "" || r.Tests[0].Started == nil || r.Tests[0].Finished == nil {
		t.Fatalf("%+v", r)
	}
	if e = os.WriteFile(filepath.Join(req.Directory, "new.txt"), []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	r, e = m.Verification(ctx, s.ID)
	if e != nil || !r.Stale {
		t.Fatalf("%+v %v", r, e)
	}
	m.Close()
	recovered, e := NewManager(m.dir)
	if e != nil {
		t.Fatal(e)
	}
	defer recovered.Close()
	r, e = recovered.Verification(ctx, s.ID)
	if e != nil || len(r.Tests) != 1 || !r.Stale {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestVerificationFailureAndUnknownVersion(t *testing.T) {
	m := newTestManager(t)
	req := childRequest(t, "output")
	req.Directory = testRepo(t)
	s, e := m.Start(req)
	if e != nil {
		t.Fatal(e)
	}
	awaitSession(t, m, s.ID)
	test := childRequest(t, "fail")
	_, e = m.StartVerification(context.Background(), s.ID, []TestCommand{{Command: test.Command, Args: test.Args}})
	if e != nil {
		t.Fatal(e)
	}
	var r VerificationReport
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		r, e = m.Verification(context.Background(), s.ID)
		if e != nil {
			t.Fatal(e)
		}
		if r.Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r.Status != "failed" || r.Tests[0].ExitCode == nil || *r.Tests[0].ExitCode != 7 {
		t.Fatal(r)
	}
	v, _ := captureCode(context.Background(), t.TempDir())
	if v.Fingerprint != "" || v.Unknown == "" {
		t.Fatal(v)
	}
	if (Device{Role: "observer"}).allows("POST", "/v1/sessions/"+s.ID+"/verification") {
		t.Fatal("observer executed a command")
	}
	if !(Device{Role: "session_observer", Sessions: []string{s.ID}}).allows("GET", "/v1/sessions/"+s.ID+"/verification") {
		t.Fatal("scoped read denied")
	}
}

func TestVerificationDoesNotUseHostIdentityForIsolatedSessions(t *testing.T) {
	m := newTestManager(t)
	req := childRequest(t, "output")
	s, e := m.Start(req)
	if e != nil {
		t.Fatal(e)
	}
	awaitSession(t, m, s.ID)
	m.mu.Lock()
	m.sessions[s.ID].scope = &sessionScope{Identity: ExecutionIdentity{UID: 1000001, GID: 1000001}}
	m.mu.Unlock()
	r, e := m.CaptureVerification(context.Background(), s.ID)
	if e != nil || r.Version.Fingerprint != "" || r.Version.Unknown == "" {
		t.Fatal(r, e)
	}
	count := len(m.List(0))
	if _, e = m.StartVerification(context.Background(), s.ID, []TestCommand{{Command: req.Command, Args: req.Args}}); e == nil || len(m.List(0)) != count {
		t.Fatal("isolated task executed tests as Host")
	}
}
