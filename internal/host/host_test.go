// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHostChild(t *testing.T) {
	args := os.Args
	for index, arg := range args {
		if arg == "--host-test-child" {
			switch args[index+1] {
			case "output":
				os.Stdout.Write([]byte{0, 'h', 'i', 255})
				time.Sleep(100 * time.Millisecond)
				os.Exit(0)
			case "sleep":
				time.Sleep(time.Minute)
				os.Exit(0)
			case "fail":
				os.Exit(7)
			}
		}
	}
}
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sessions")
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}
func childRequest(t *testing.T, mode string) StartRequest {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return StartRequest{Kind: "task", Command: exe, Args: []string{"-test.run=TestHostChild", "--", "--host-test-child", mode}, Directory: t.TempDir()}
}
func awaitSession(t *testing.T, m *Manager, id string) Session {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		item := m.sessions[id]
		done := item.done
		m.mu.Unlock()
		select {
		case <-done:
			s, err := m.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			return s
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("session did not finish")
	return Session{}
}
func TestTaskPersistsAndOutputCanResume(t *testing.T) {
	m := newTestManager(t)
	s, err := m.Start(childRequest(t, "output"))
	if err != nil {
		t.Fatal(err)
	}
	s = awaitSession(t, m, s.ID)
	if s.State != "succeeded" || s.ExitCode == nil || *s.ExitCode != 0 {
		t.Fatalf("%+v", s)
	}
	data, next, err := m.Output(s.ID, 0)
	if err != nil || !bytes.Equal(data, []byte{0, 'h', 'i', 255}) || next != 4 {
		t.Fatalf("output %v %d %v", data, next, err)
	}
	data, next, err = m.Output(s.ID, 2)
	if err != nil || !bytes.Equal(data, []byte{'i', 255}) || next != 4 {
		t.Fatal("resume failed")
	}
	m.Close()
	recovered, err := NewManager(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	got, err := recovered.Get(s.ID)
	if err != nil || got.State != "succeeded" {
		t.Fatalf("recovery %+v: %v", got, err)
	}
	if err := recovered.Delete(s.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteSessionRetriesCleanupAfterRestart(t *testing.T) {
	m := newTestManager(t)
	started, err := m.Start(childRequest(t, "output"))
	if err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, started.ID)

	blocked := filepath.Join(m.dir, started.ID+".summary")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(blocked, "entry")
	if err := os.WriteFile(child, []byte("obstruction"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(started.ID); err == nil {
		t.Fatal("delete succeeded with a nonempty auxiliary directory")
	}
	if _, err := m.Get(started.ID); err != nil {
		t.Fatalf("session disappeared after failed cleanup: %v", err)
	}
	m.Close()

	recovered, err := NewManager(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	if err := recovered.Delete(started.ID); err != nil {
		t.Fatalf("retry deletion: %v", err)
	}
	for _, ext := range []string{".json", ".ring", ".summary"} {
		if _, err := os.Stat(filepath.Join(m.dir, started.ID+ext)); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, got %v", ext, err)
		}
	}
}

func TestFailureCancellationAndRecovery(t *testing.T) {
	m := newTestManager(t)
	failed, err := m.Start(childRequest(t, "fail"))
	if err != nil {
		t.Fatal(err)
	}
	failed = awaitSession(t, m, failed.ID)
	if failed.State != "failed" || *failed.ExitCode != 7 {
		t.Fatalf("%+v", failed)
	}
	s, err := m.Start(childRequest(t, "sleep"))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(s.ID); err == nil {
		t.Fatal("deleted running session")
	}
	if err := m.Cancel(s.ID); err != nil {
		t.Fatal(err)
	}
	s = awaitSession(t, m, s.ID)
	if s.State != "canceled" {
		t.Fatalf("%+v", s)
	}
	// Simulate persisted intent surviving a crash; no stale PID is signaled.
	id, _ := randomID()
	if err := saveJSON(filepath.Join(m.dir, id+".json"), Session{ID: id, State: "running"}); err != nil {
		t.Fatal(err)
	}
	m.Close()
	recovered, err := NewManager(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	got, err := recovered.Get(id)
	if err != nil || got.State != "interrupted" || got.Finished == nil {
		t.Fatalf("%+v %v", got, err)
	}
}
func request(t *testing.T, server http.Handler, method, path, body, token, protocol string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Nimote-Host-Protocol", protocol)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	return w
}
func TestAuthenticationCompatibilityAndRequestBounds(t *testing.T) {
	m := newTestManager(t)
	server := &Server{manager: m, token: strings.Repeat("a", 64), hostID: "host-one"}
	for _, path := range []string{"/v1/capabilities", "/v1/status", "/v1/sessions", "/v1/events", "/v1/shutdown"} {
		w := request(t, server, "POST", path, "{}", "wrong", "1")
		if w.Code != 401 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	w := request(t, server, "GET", "/v1/capabilities", "", server.token, "99")
	if w.Code != 426 {
		t.Fatal(w.Code)
	}
	w = request(t, server, "GET", "/v1/capabilities", "", server.token, "1")
	var caps Capabilities
	if err := json.Unmarshal(w.Body.Bytes(), &caps); err != nil {
		t.Fatal(err)
	}
	if caps.HostID != "host-one" || caps.Features["public_transport"] || caps.Protocol != 1 {
		t.Fatalf("%+v", caps)
	}
	r := httptest.NewRequest("GET", "/v1/capabilities", nil)
	r.Header.Set("Origin", "https://example.org")
	w = httptest.NewRecorder()
	server.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	for _, body := range []string{`{} {}`, `{"unknown":true}`} {
		w = request(t, server, "POST", "/v1/sessions", body, server.token, "1")
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	w = request(t, server, "POST", "/v1/sessions", strings.Repeat("x", maxRequest+1), server.token, "1")
	if w.Code != 413 {
		t.Fatal(w.Code)
	}
	w = request(t, server, "GET", "/v1/sessions/../../token", "", server.token, "1")
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
func TestRPCDisconnectDoesNotCancelTask(t *testing.T) {
	m := newTestManager(t)
	api := &Server{manager: m, token: strings.Repeat("b", 64)}
	server := httptest.NewServer(api)
	req := childRequest(t, "output")
	data, _ := json.Marshal(req)
	r, _ := http.NewRequest("POST", server.URL+"/v1/sessions", bytes.NewReader(data))
	r.Header.Set("Authorization", "Bearer "+api.token)
	r.Header.Set("Nimote-Host-Protocol", "1")
	r.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	var s Session
	err = json.NewDecoder(response.Body).Decode(&s)
	response.Body.Close()
	server.Close()
	if err != nil {
		t.Fatal(err)
	}
	got := awaitSession(t, m, s.ID)
	if got.State != "succeeded" {
		t.Fatalf("%+v", got)
	}
	// A second device can read the same durable session and cursor.
	w := request(t, api, "GET", "/v1/sessions/"+s.ID+"/output?offset=2", "", api.token, "1")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}
func TestOutputBoundAndSecretPersistence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "host")
	if err := privateDir(dir); err != nil {
		t.Fatal(err)
	}
	first, err := loadSecret(dir, "token")
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadSecret(dir, "token")
	if err != nil || first != second {
		t.Fatal("credential changed")
	}
	f, err := os.CreateTemp(dir, "output")
	if err != nil {
		t.Fatal(err)
	}
	w := &outputWriter{file: f}
	block := bytes.Repeat([]byte("x"), 1<<20)
	for i := 0; i < 18; i++ {
		if n, err := w.Write(block); err != nil || n != len(block) {
			t.Fatal(n, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(f.Name())
	if info.Size() != maxOutput+ringHeader {
		t.Fatal(info.Size())
	}
}
func TestCallCannotRedirectToRemoteHost(t *testing.T) {
	for _, path := range []string{"http://example.org/", "//example.org/v1/status", "/v1/status\r\nX: y"} {
		if _, err := Call(context.Background(), t.TempDir(), RPCRequest{Method: "GET", Path: path}); err == nil {
			t.Fatal(path)
		}
	}
}
func TestOutputAfterEndIsEmpty(t *testing.T) {
	m := newTestManager(t)
	s, err := m.Start(childRequest(t, "output"))
	if err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, s.ID)
	data, next, err := m.Output(s.ID, 100)
	if err != nil || len(data) != 0 || next != 100 {
		t.Fatal(data, next, err)
	}
	if _, _, err := m.Output(s.ID, -1); err == nil {
		t.Fatal("negative offset accepted")
	}
}

var _ io.Writer = (*outputWriter)(nil)

func TestHostShutdownInterruptsActiveTask(t *testing.T) {
	m := newTestManager(t)
	s, err := m.Start(childRequest(t, "sleep"))
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
	got, err := m.Get(s.ID)
	if err != nil || got.State != "interrupted" || got.Finished == nil {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := m.Start(childRequest(t, "output")); err == nil {
		t.Fatal("start accepted during shutdown")
	}
}

func TestIdleShutdownProtectsTasksAndClosesAdmission(t *testing.T) {
	m := newTestManager(t)
	stopped := false
	server := &Server{manager: m, token: strings.Repeat("c", 64), shutdown: func() { stopped = true }}
	w := request(t, server, "POST", "/v1/shutdown-if-idle", "", "wrong", "1")
	if w.Code != 401 || stopped {
		t.Fatal("idle shutdown bypassed authentication")
	}
	s, err := m.Start(childRequest(t, "sleep"))
	if err != nil {
		t.Fatal(err)
	}
	w = request(t, server, "POST", "/v1/shutdown-if-idle", "", server.token, "1")
	if w.Code != 409 || stopped {
		t.Fatal("active task was interrupted")
	}
	s2, err := m.Start(childRequest(t, "output"))
	if err != nil {
		t.Fatal("rejected shutdown closed admission:", err)
	}
	awaitSession(t, m, s2.ID)
	if err := m.Cancel(s.ID); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, s.ID)
	w = request(t, server, "POST", "/v1/shutdown-if-idle", "", server.token, "1")
	if w.Code != 200 || !stopped {
		t.Fatalf("idle shutdown failed: %d", w.Code)
	}
	if _, err := m.Start(childRequest(t, "output")); err == nil {
		t.Fatal("new task admitted after idle shutdown")
	}
}

func TestIdleShutdownAndStartAreMutuallyExclusive(t *testing.T) {
	for attempt := 0; attempt < 8; attempt++ {
		m := newTestManager(t)
		req := childRequest(t, "sleep")
		start := make(chan struct{})
		type started struct {
			session Session
			err     error
		}
		sessions := make(chan started, 1)
		shutdown := make(chan error, 1)
		go func() { <-start; s, err := m.Start(req); sessions <- started{s, err} }()
		go func() { <-start; shutdown <- m.PrepareIdleShutdown() }()
		close(start)
		result := <-sessions
		shutdownErr := <-shutdown
		if (result.err == nil) == (shutdownErr == nil) {
			t.Fatalf("start=%v idle shutdown=%v; exactly one must succeed", result.err, shutdownErr)
		}
		if result.err == nil {
			if err := m.Cancel(result.session.ID); err != nil {
				t.Fatal(err)
			}
			awaitSession(t, m, result.session.ID)
		}
	}
}
