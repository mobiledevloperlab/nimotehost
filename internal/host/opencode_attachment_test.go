// SPDX-License-Identifier: Apache-2.0
package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type openCodeFixture struct {
	mu              sync.Mutex
	directory       string
	busy            bool
	pending         bool
	offline         bool
	foreign         bool
	deleteSession   bool
	losePromptReply bool
	prompts         int
	approvals       int
	interrupts      int
	messages        []map[string]any
}

func (f *openCodeFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	username, password, ok := r.BasicAuth()
	if !ok || username != "opencode" || password != "fixture-password" {
		http.Error(w, "secret error must not escape", 401)
		return
	}
	if r.URL.Path == "/api/event" {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.offline {
		http.Error(w, "sensitive fixture error", 503)
		return
	}
	if f.deleteSession {
		http.NotFound(w, r)
		return
	}
	write := func(value any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(value)
	}
	session := map[string]any{"id": "ses_original", "title": "Original task", "location": map[string]string{"directory": f.directory}, "time": map[string]int64{"created": 1, "updated": 2}}
	switch r.Method + " " + r.URL.Path {
	case "GET /api/session":
		write(map[string]any{"data": []any{session}, "cursor": map[string]any{}})
	case "GET /api/session/ses_original":
		write(map[string]any{"data": session})
	case "GET /api/session/active":
		active := map[string]any{}
		if f.busy {
			active["ses_original"] = map[string]string{"type": "running"}
		}
		write(map[string]any{"data": active})
	case "GET /api/session/ses_original/permission":
		pending := []any{}
		if f.pending {
			sessionID := "ses_original"
			if f.foreign {
				sessionID = "ses_foreign"
			}
			pending = append(pending, map[string]any{"id": "per_native", "sessionID": sessionID, "action": "execute", "resources": []string{"pwd"}})
		}
		write(map[string]any{"data": pending})
	case "GET /api/session/ses_original/message":
		messages := f.messages
		if messages == nil {
			messages = []map[string]any{}
		}
		write(map[string]any{"data": messages, "cursor": map[string]any{}})
	case "POST /api/session/ses_original/permission/per_native/reply":
		var body struct {
			Decision string `json:"decision"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !f.pending || body.Decision != "once" {
			http.Error(w, "stale or unsafe permission", 409)
			return
		}
		f.pending = false
		f.approvals++
		w.WriteHeader(204)
	case "POST /api/session/ses_original/prompt":
		var body struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if f.busy || !strings.HasPrefix(body.ID, "msg_") || body.Text == "" {
			http.Error(w, "bad prompt", 409)
			return
		}
		f.prompts++
		f.busy = true
		if f.losePromptReply {
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		write(map[string]any{"data": map[string]string{"id": body.ID}})
	case "POST /api/session/ses_original/interrupt":
		f.busy = false
		f.interrupts++
		w.WriteHeader(204)
	default:
		http.Error(w, "unsupported fixture route", 404)
	}
}

func newOpenCodeFixture(t *testing.T) (*openCodeFixture, ExternalAttachment) {
	t.Helper()
	f := &openCodeFixture{directory: t.TempDir(), busy: true, pending: true, messages: []map[string]any{{"id": "msg_existing", "type": "user", "text": "original task"}}}
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	return f, ExternalAttachment{ExternalConnection: ExternalConnection{Transport: "opencode-http-v2", Endpoint: server.URL}, Password: "fixture-password"}
}
func attachOpenCodeFixture(t *testing.T, m *Manager, f *openCodeFixture, c ExternalAttachment, key string) Session {
	t.Helper()
	s, err := m.Start(StartRequest{Kind: "agent", Agent: "opencode", Interaction: "auto", Directory: f.directory, RequestID: key, Resume: &AgentResume{ConversationID: "ses_original"}, Attach: &c})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func waitExternalCondition(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("external condition timed out")
}

func TestOpenCodeAttachOriginalApprovalReconnectAndDetach(t *testing.T) {
	f, c := newOpenCodeFixture(t)
	m := newTestManager(t)
	s := attachOpenCodeFixture(t, m, f, c, "attach")
	if s.ConversationID != "ses_original" || s.External == nil {
		t.Fatal(s)
	}
	snapshot, err := m.AgentSnapshot(s.ID)
	if err != nil || snapshot["status"] != "needs_approval" || len(snapshot["approvals"].([]any)) != 1 {
		t.Fatal(snapshot, err)
	}
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for i := range 16 {
		wg.Go(func() {
			if m.AgentMessage(s.ID, AgentMessage{Action: "approval", RequestID: fmt.Sprint("approve-", i), ApprovalID: "per_native", Decision: "accept"}) == nil {
				accepted.Add(1)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatal("duplicate native approvals", accepted.Load())
	}
	// Reading an unchanged snapshot must not append duplicate messages/events.
	m.mu.Lock()
	p := m.sessions[s.ID].agent.openCode
	out := m.sessions[s.ID].output
	m.mu.Unlock()
	p.control.Lock()
	if err := p.snapshot(); err != nil {
		t.Fatal(err)
	}
	before, _ := out.stats()
	if err := p.snapshot(); err != nil {
		t.Fatal(err)
	}
	after, _ := out.stats()
	p.control.Unlock()
	if before != after {
		t.Fatal("unchanged upstream snapshot appended duplicate events")
	}
	f.mu.Lock()
	f.offline = true
	f.mu.Unlock()
	p.wake <- struct{}{}
	waitExternalCondition(t, func() bool {
		snap, _ := m.AgentSnapshot(s.ID)
		return snap["status"] == "disconnected" && snap["ready"] == false
	})
	f.mu.Lock()
	f.offline = false
	f.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
	waitExternalCondition(t, func() bool {
		snap, _ := m.AgentSnapshot(s.ID)
		return snap["status"] == "running" && snap["ready"] == true
	})
	if err := m.Cancel(s.ID); err != nil {
		t.Fatal(err)
	}
	waitExternalCondition(t, func() bool { record, _ := m.Get(s.ID); return record.Finished != nil })
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.busy || f.interrupts != 0 || f.approvals != 1 || f.prompts != 0 {
		t.Fatal("detach changed external task", f.busy, f.interrupts, f.approvals, f.prompts)
	}
}

func TestOpenCodeAttachHostRestartRetainsNativePendingAndNoCredentials(t *testing.T) {
	f, c := newOpenCodeFixture(t)
	m := newTestManager(t)
	s := attachOpenCodeFixture(t, m, f, c, "first")
	data, err := os.ReadFile(filepath.Join(m.dir, s.ID+".json"))
	if err != nil || strings.Contains(string(data), c.Password) {
		t.Fatal("credential persisted", err)
	}
	m.Close()
	f.mu.Lock()
	pending, busy, interrupts := f.pending, f.busy, f.interrupts
	f.mu.Unlock()
	if !pending || !busy || interrupts != 0 {
		t.Fatal("Host shutdown changed external task")
	}
	restored, err := NewManager(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	old, err := restored.Get(s.ID)
	if err != nil || old.State != "interrupted" {
		t.Fatal(old, err)
	}
	newSession := attachOpenCodeFixture(t, restored, f, c, "second")
	if newSession.ID == s.ID || newSession.ConversationID != s.ConversationID {
		t.Fatal("reconnect replaced native session")
	}
	snap, err := restored.AgentSnapshot(newSession.ID)
	if err != nil || snap["status"] != "needs_approval" {
		t.Fatal(snap, err)
	}
}

func TestOpenCodeAttachUncertainPromptNeverRepeats(t *testing.T) {
	f, c := newOpenCodeFixture(t)
	f.busy = false
	f.pending = false
	f.losePromptReply = true
	m := newTestManager(t)
	s := attachOpenCodeFixture(t, m, f, c, "attach")
	req := AgentMessage{Action: "prompt", RequestID: "original-request", Text: "continue"}
	for range 2 {
		if err := m.AgentMessage(s.ID, req); !errors.Is(err, errRequestUncertain) {
			t.Fatal("lost acknowledgement not retained", err)
		}
	}
	req.Text = "different"
	if err := m.AgentMessage(s.ID, req); !errors.Is(err, errRequestConflict) {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.prompts != 1 {
		t.Fatal("uncertain prompt repeated", f.prompts)
	}
}

func TestExternalAttachmentRejectsUnsafeEndpointsAndRestrictions(t *testing.T) {
	for _, endpoint := range []string{"http://example.com:4096", "http://localhost:4096", "http://127.0.0.1:80", "http://127.0.0.1:4096/path", "http://user:password@127.0.0.1:4096", "http://127.0.0.1:4096?token=secret", "https://127.0.0.1:4096"} {
		for _, transport := range []string{"opencode-http-v1", "opencode-http-v2"} {
			if err := (ExternalAttachment{ExternalConnection: ExternalConnection{Transport: transport, Endpoint: endpoint}}).validate("opencode"); err == nil {
				t.Fatal("unsafe endpoint accepted", transport, endpoint)
			}
		}
	}
	f, c := newOpenCodeFixture(t)
	req := StartRequest{Kind: "agent", Agent: "opencode", Interaction: "structured", Directory: f.directory, Resume: &AgentResume{ConversationID: "ses_original"}, Attach: &c}
	for _, policy := range []string{"smart", "auto"} {
		req.ExecutionPolicy = policy
		if err := resolveCommand(&req); err == nil {
			t.Fatal("external auto approval policy accepted")
		}
	}
	req.ExecutionPolicy = ""
	req.Agent = "claude"
	if err := resolveCommand(&req); err == nil {
		t.Fatal("unsupported Claude attachment silently started")
	}
}

func TestOpenCodeAttachRejectsForeignApprovalDeletedSessionAndRedirect(t *testing.T) {
	for _, variant := range []string{"foreign", "deleted", "auth", "large-message-id"} {
		t.Run(variant, func(t *testing.T) {
			f, c := newOpenCodeFixture(t)
			if variant == "foreign" {
				f.foreign = true
			}
			if variant == "deleted" {
				f.deleteSession = true
			}
			if variant == "auth" {
				c.Password = "wrong"
			}
			if variant == "large-message-id" {
				f.messages[0]["id"] = strings.Repeat("x", maxHistoryItemBytes+1)
			}
			m := newTestManager(t)
			_, err := m.Start(StartRequest{Kind: "agent", Agent: "opencode", Interaction: "auto", Directory: f.directory, RequestID: "attach", Resume: &AgentResume{ConversationID: "ses_original"}, Attach: &c})
			if err == nil || strings.Contains(err.Error(), "sensitive") || strings.Contains(err.Error(), "secret") {
				t.Fatal(err)
			}
		})
	}
	var leaked atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer redirect.Close()
	c := ExternalAttachment{ExternalConnection: ExternalConnection{Transport: "opencode-http-v2", Endpoint: redirect.URL}, Password: "fixture-password"}
	if _, err := listAttachedSessions(t.Context(), "opencode", "", "", c); err == nil || leaked.Load() != 0 {
		t.Fatal("redirect followed or credentials leaked", err)
	}
}

func TestAttachedHistoryHTTPAuthorizationAndReconnectionLink(t *testing.T) {
	f, c := newOpenCodeFixture(t)
	m := newTestManager(t)
	s := attachOpenCodeFixture(t, m, f, c, "attach-http")
	api := &Server{token: "owner", manager: m}
	data, _ := json.Marshal(map[string]any{"agent": "opencode", "directory": f.directory, "connection": c})
	if reply := deviceRequest(api, "wrong", "POST", "/v1/agent-history", string(data)); reply.Code != 401 {
		t.Fatal(reply.Code)
	}
	reply := deviceRequest(api, "owner", "POST", "/v1/agent-history", string(data))
	var page codexHistoryPage
	if reply.Code != 200 || json.Unmarshal(reply.Body.Bytes(), &page) != nil || len(page.Items) != 1 {
		t.Fatal(reply.Code, reply.Body.String())
	}
	item := page.Items[0]
	if item["host_session_id"] != s.ID || item["execution_status"] != "needs_approval" || item["live_control"] != "supported" || strings.Contains(reply.Body.String(), c.Password) {
		t.Fatal("live discovery lost original host link or exposed credentials")
	}
	data, _ = json.Marshal(map[string]any{"agent": "claude", "connection": c})
	if reply := deviceRequest(api, "owner", "POST", "/v1/agent-history", string(data)); reply.Code != 400 || !strings.Contains(reply.Body.String(), "unsupported") {
		t.Fatal("unsupported Claude control misrepresented", reply.Code, reply.Body.String())
	}
}
