// SPDX-License-Identifier: Apache-2.0
package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func deviceRequest(api *Server, token, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Nimote-Host-Protocol", "1")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	return w
}
func TestScopedDevicesFilterEverySessionRead(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	first, err := m.Start(childRequest(t, "output"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Start(childRequest(t, "output"))
	if err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, first.ID)
	awaitSession(t, m, second.ID)
	for _, role := range []string{"session_observer", "session_operator"} {
		t.Run(role, func(t *testing.T) {
			d, err := CreateScopedDevice(dir, "phone", role, []string{first.ID})
			if err != nil {
				t.Fatal(err)
			}
			token, _, err := deviceCredential(DeviceContext(context.Background(), d.ID), dir)
			if err != nil {
				t.Fatal(err)
			}
			api := &Server{dir: dir, manager: m, token: "owner"}
			for _, path := range []string{"/v1/sessions", "/v1/events", "/v1/sessions/" + first.ID, "/v1/sessions/" + first.ID + "/activity", "/v1/sessions/" + first.ID + "/output"} {
				w := deviceRequest(api, token, "GET", path, "")
				if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte(second.ID)) {
					t.Fatalf("scope leak %s: %d %s", path, w.Code, w.Body.String())
				}
			}
			for _, action := range []string{"", "/output", "/activity", "/agent-state"} {
				if w := deviceRequest(api, token, "GET", "/v1/sessions/"+second.ID+action, ""); w.Code != 403 {
					t.Fatalf("cross-session read: %d", w.Code)
				}
			}
			for _, req := range []struct{ method, path, body string }{
				{"POST", "/v1/sessions", `{}`}, {"GET", "/v1/agent-history", ""}, {"POST", "/v1/shutdown", ""},
				{"DELETE", "/v1/sessions/" + first.ID, ""}, {"POST", "/v1/sessions/" + first.ID + "/input", `{"data":"eA=="}`},
				{"POST", "/v1/sessions/" + second.ID + "/cancel", `{"request_id":"deny"}`},
				{"POST", "/v1/sessions/" + first.ID + "/agent-message", `{"action":"approval","request_id":"deny","approval_id":"42","decision":"accept"}`},
				{"POST", "/v1/sessions/" + first.ID + "/agent-message", `{"action":"prompt","request_id":"deny","text":"hello"}`},
				{"POST", "/v1/sessions/" + first.ID + "/agent-message", `{"action":"configure","request_id":"deny","option_id":"__execution_policy","value":"auto"}`},
			} {
				if w := deviceRequest(api, token, req.method, req.path, req.body); w.Code != 403 {
					t.Fatalf("privilege escape %+v: %d", req, w.Code)
				}
			}
			w := deviceRequest(api, token, "GET", "/v1/status", "")
			if bytes.Contains(w.Body.Bytes(), []byte("request_storage")) || bytes.Contains(w.Body.Bytes(), []byte("resources")) {
				t.Fatal("global resource metadata exposed")
			}
			if err := RevokeDevice(dir, d.ID); err != nil {
				t.Fatal(err)
			}
			if w := deviceRequest(api, token, "GET", "/v1/sessions", ""); w.Code != 401 {
				t.Fatal("revoked scoped identity accepted")
			}
		})
	}
}

func TestScopedOperatorCancellationAndDecline(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	s, err := m.Start(childRequest(t, "sleep"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := CreateScopedDevice(dir, "operator", "session_operator", []string{s.ID})
	if err != nil {
		t.Fatal(err)
	}
	token, _, _ := deviceCredential(DeviceContext(context.Background(), d.ID), dir)
	api := &Server{dir: dir, manager: m, token: "owner"}
	path := "/v1/sessions/" + s.ID + "/cancel"
	if w := deviceRequest(api, token, "POST", path, `{}`); w.Code != 400 {
		t.Fatal("unkeyed scoped mutation accepted")
	}
	for range 2 {
		if w := deviceRequest(api, token, "POST", path, `{"request_id":"cancel"}`); w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	if got := awaitSession(t, m, s.ID); got.State != "canceled" {
		t.Fatal(got)
	}
	// A scoped operator can decline a pending native request, never accept it.
	f, err := os.CreateTemp(m.dir, "agent-output")
	if err != nil {
		t.Fatal(err)
	}
	out := &outputWriter{file: f}
	defer out.Close()
	a := newStructuredAgent("codex", out, t.TempDir())
	a.input = discardAgentInput{}
	a.thread = "thread"
	a.busy = true
	a.approvals["42"] = json.RawMessage(`42`)
	m.mu.Lock()
	item := m.sessions[s.ID]
	item.agent = a
	item.process = &permissionTestProcess{}
	item.output = out
	m.mu.Unlock()
	// Restore the synthetic handle before Manager.Close attempts real lifecycle teardown.
	defer func() { m.mu.Lock(); item.process = nil; m.mu.Unlock() }()
	body := `{"action":"approval","request_id":"decline","approval_id":"42","decision":"decline"}`
	w := deviceRequest(api, token, "POST", "/v1/sessions/"+s.ID+"/agent-message", body)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if len(a.approvals) != 0 || queuedAgentOperations(a) != 1 {
		t.Fatal("decline not queued exactly once")
	}
	w = deviceRequest(api, token, "POST", "/v1/sessions/"+s.ID+"/agent-message", body)
	if w.Code != 200 || queuedAgentOperations(a) != 1 {
		t.Fatal("duplicate decline dispatched")
	}
}

type permissionTestProcess struct{}

func (*permissionTestProcess) Wait() (int, error) { return 0, nil }
func (*permissionTestProcess) Kill() error        { return nil }
func (*permissionTestProcess) Close() error       { return nil }

func TestScopedDeviceValidationAndDurableMutationPrincipals(t *testing.T) {
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	for _, scope := range [][]string{nil, {"../token"}, {strings.Repeat("a", 64), strings.Repeat("a", 64)}} {
		if _, err := CreateScopedDevice(dir, "bad", "session_operator", scope); err == nil {
			t.Fatal("invalid scope accepted")
		}
	}
	if _, err := CreateScopedDevice(dir, "bad", "owner", []string{strings.Repeat("a", 64)}); err == nil {
		t.Fatal("owner device created")
	}
	m := newTestManager(t)
	count := 0
	for _, principal := range []string{"device/" + strings.Repeat("a", 64) + "/op", "device/" + strings.Repeat("b", 64) + "/op", "op"} {
		for range 2 {
			if _, err := runOnce(m, "same-request", principal, "payload", func() (int, error) { count++; return count, nil }); err != nil {
				t.Fatal(err)
			}
		}
	}
	if count != 3 {
		t.Fatal("mutation principals were not isolated", count)
	}
	m.Close()
	reloaded, err := NewManager(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	if value, err := runOnce(reloaded, "same-request", "device/"+strings.Repeat("a", 64)+"/op", "payload", func() (int, error) { t.Fatal("replayed after restart"); return 0, nil }); err != nil || value != 1 {
		t.Fatal(value, err)
	}
}
func TestConcurrentScopedRevocationFailsClosed(t *testing.T) {
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	d, err := CreateScopedDevice(dir, "phone", "session_observer", []string{strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	token, _, _ := deviceCredential(DeviceContext(context.Background(), d.ID), dir)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 20 {
				authenticatedDevice(dir, token)
			}
		})
	}
	if err := RevokeDevice(dir, d.ID); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if authenticateDevice(dir, token) {
		t.Fatal("revocation lost")
	}
	// A corrupt scope cannot turn into an unrestricted observer.
	p := filepath.Join(dir, "devices", d.ID+".json")
	if err := os.WriteFile(p, []byte(fmt.Sprintf(`{"id":%q,"role":"session_operator","credential_hash":%q}`, d.ID, strings.Repeat("a", 64))), 0600); err != nil {
		t.Fatal(err)
	}
	if authenticateDevice(dir, token) {
		t.Fatal("corrupt scoped record accepted")
	}
}

func TestMaximumSessionScopeFitsBoundedDeviceRecord(t *testing.T) {
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	scope := make([]string, 256)
	for i := range scope {
		scope[i] = fmt.Sprintf("%064x", i)
	}
	d, err := CreateScopedDevice(dir, "maximum scope", "session_operator", scope)
	if err != nil {
		t.Fatal(err)
	}
	record, err := readDevice(dir, d.ID)
	if err != nil || len(record.Sessions) != 256 {
		t.Fatal("valid maximum scope cannot be read", err)
	}
	token, _, err := deviceCredential(DeviceContext(context.Background(), d.ID), dir)
	if err != nil || !authenticateDevice(dir, token) {
		t.Fatal("valid maximum scope cannot authenticate", err)
	}
	if _, err := CreateScopedDevice(dir, "overflow", "session_operator", append(scope, strings.Repeat("f", 64))); err == nil {
		t.Fatal("oversized scope accepted")
	}
	path := filepath.Join(dir, "devices", d.ID+".json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(original) <= 16<<10 {
		t.Fatal("fixture did not exercise extended record budget")
	}
	if err := os.WriteFile(path, append(original, bytes.Repeat([]byte(" "), 32<<10)...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readDevice(dir, d.ID); err == nil {
		t.Fatal("oversized private record accepted")
	}
}
