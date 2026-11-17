// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Shapes and paths follow the official V1 schema, not the V2 fixture.
type openCodeV1Fixture struct {
	mu                                                               sync.Mutex
	directory                                                        string
	pending, busy, offline, deleted, foreignMessage, losePromptReply bool
	approvals, prompts, aborts                                       int
	listCount                                                        int
	messageID                                                        string
}

func (f *openCodeV1Fixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u, p, ok := r.BasicAuth()
	if !ok || u != "opencode" || p != "fixture-password" {
		http.Error(w, "private", 401)
		return
	}
	if r.URL.Query().Get("directory") != f.directory {
		http.Error(w, "wrong workspace", 400)
		return
	}
	if r.URL.Path == "/event" {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.offline {
		http.Error(w, "private", 503)
		return
	}
	if f.deleted {
		http.NotFound(w, r)
		return
	}
	write := func(value any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(value)
	}
	session := func(id string) map[string]any {
		return map[string]any{"id": id, "directory": f.directory, "title": "native task", "time": map[string]int64{"created": 1, "updated": 2}}
	}
	switch r.Method + " " + r.URL.Path {
	case "GET /session":
		rows := []any{session("ses_original")}
		for i := 1; i < f.listCount; i++ {
			rows = append(rows, session(fmt.Sprint("ses_", i)))
		}
		write(rows)
	case "GET /session/ses_original":
		write(session("ses_original"))
	case "GET /session/status":
		statuses := map[string]any{}
		if f.busy {
			statuses["ses_original"] = map[string]string{"type": "busy"}
		}
		write(statuses)
	case "GET /permission":
		rows := []any{map[string]any{"id": "per_foreign", "sessionID": "ses_other", "permission": "bash", "patterns": []string{"other"}}}
		if f.pending {
			rows = append(rows, map[string]any{"id": "per_native", "sessionID": "ses_original", "permission": "bash", "patterns": []string{"pwd"}})
		}
		write(rows)
	case "GET /session/ses_original/message":
		sid := "ses_original"
		if f.foreignMessage {
			sid = "ses_other"
		}
		write([]any{map[string]any{"info": map[string]string{"id": "msg_existing", "sessionID": sid, "role": "assistant"}, "parts": []any{
			map[string]any{"id": "part_text", "sessionID": sid, "messageID": "msg_existing", "type": "text", "text": "original response"},
			map[string]any{"id": "part_tool", "sessionID": sid, "messageID": "msg_existing", "type": "tool", "callID": "call_original", "tool": "bash", "state": map[string]any{"status": "completed", "input": map[string]string{"command": "pwd"}, "output": "workspace"}},
		}}})
	case "POST /permission/per_native/reply":
		var body struct {
			Reply string `json:"reply"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Reply != "once" || !f.pending {
			http.Error(w, "stale", 409)
			return
		}
		f.pending = false
		f.approvals++
		write(true)
	case "POST /session/ses_original/abort":
		f.busy = false
		f.aborts++
		write(true)
	case "POST /session/ses_original/prompt_async":
		var body struct {
			MessageID string `json:"messageID"`
			Parts     []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"parts"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || !strings.HasPrefix(body.MessageID, "msg_") || len(body.Parts) != 1 || body.Parts[0].Type != "text" || body.Parts[0].Text != "continue original" {
			http.Error(w, "invalid prompt", 400)
			return
		}
		f.prompts++
		f.messageID = body.MessageID
		f.busy = true
		if f.losePromptReply {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = connection.Close()
			}
			return
		}
		w.WriteHeader(204)
	default:
		http.NotFound(w, r)
	}
}

func newOpenCodeV1Fixture(t *testing.T) (*openCodeV1Fixture, ExternalAttachment) {
	t.Helper()
	f := &openCodeV1Fixture{directory: t.TempDir(), pending: true, busy: true, listCount: 1}
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	return f, ExternalAttachment{ExternalConnection: ExternalConnection{Transport: "opencode-http-v1", Endpoint: server.URL}, Password: "fixture-password"}
}

func TestOpenCodeV1OriginalSessionApprovalPromptAndReconnect(t *testing.T) {
	f, c := newOpenCodeV1Fixture(t)
	m := newTestManager(t)
	s, err := m.Start(StartRequest{Kind: "agent", Agent: "opencode", Interaction: "auto", Directory: f.directory, RequestID: "attach-v1", Resume: &AgentResume{ConversationID: "ses_original"}, Attach: &c})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := m.AgentSnapshot(s.ID)
	if err != nil || snapshot["status"] != "needs_approval" || len(snapshot["approvals"].([]any)) != 1 {
		t.Fatal(snapshot, err)
	}
	approval := AgentMessage{Action: "approval", RequestID: "approve-v1", ApprovalID: "per_native", Decision: "accept"}
	for range 2 {
		if err := m.AgentMessage(s.ID, approval); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.AgentMessage(s.ID, AgentMessage{Action: "interrupt", RequestID: "interrupt-v1"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.losePromptReply = true
	f.mu.Unlock()
	prompt := AgentMessage{Action: "prompt", RequestID: "prompt-v1", Text: "continue original"}
	for range 2 {
		if err := m.AgentMessage(s.ID, prompt); err != errRequestUncertain {
			t.Fatal("uncertain write retried or hidden", err)
		}
	}
	f.mu.Lock()
	f.offline = true
	f.mu.Unlock()
	waitExternalCondition(t, func() bool { state, _ := m.AgentSnapshot(s.ID); return state["status"] == "disconnected" })
	f.mu.Lock()
	f.offline = false
	f.mu.Unlock()
	waitExternalCondition(t, func() bool {
		state, _ := m.AgentSnapshot(s.ID)
		return state["status"] == "running" && state["ready"] == true
	})
	if err := m.Cancel(s.ID); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.approvals != 1 || f.aborts != 1 || f.prompts != 1 || !f.busy || f.messageID == "" {
		t.Fatal("native lifecycle mismatch", f.approvals, f.aborts, f.prompts, f.busy)
	}
}

func TestOpenCodeV1DiscoveryPaginationAndIdentityFailures(t *testing.T) {
	f, c := newOpenCodeV1Fixture(t)
	f.mu.Lock()
	f.listCount = 51
	f.mu.Unlock()
	page, err := listOpenCodeSessions(context.Background(), c, f.directory, "")
	if err != nil || len(page.Items) != 50 || page.NextCursor == nil || page.Items[0]["execution_status"] != "running" {
		t.Fatal(page, err)
	}
	next, err := listOpenCodeSessions(context.Background(), c, f.directory, *page.NextCursor)
	if err != nil || len(next.Items) != 1 || next.NextCursor != nil {
		t.Fatal(next, err)
	}
	if _, err := listOpenCodeSessions(context.Background(), c, f.directory, "ses_deleted"); err == nil {
		t.Fatal("expired discovery cursor accepted")
	}
	for _, mode := range []string{"foreign-message", "deleted", "wrong-password"} {
		t.Run(mode, func(t *testing.T) {
			f.mu.Lock()
			f.foreignMessage = mode == "foreign-message"
			f.deleted = mode == "deleted"
			f.mu.Unlock()
			connection := c
			if mode == "wrong-password" {
				connection.Password = "wrong"
			}
			m := newTestManager(t)
			if _, err := m.Start(StartRequest{Kind: "agent", Agent: "opencode", Interaction: "auto", Directory: f.directory, RequestID: mode, Resume: &AgentResume{ConversationID: "ses_original"}, Attach: &connection}); err == nil {
				t.Fatal("invalid external session accepted")
			}
		})
	}
}

func TestOpenCodeV1TranscriptBoundariesAndToolCorrelation(t *testing.T) {
	for _, mode := range []string{"messages", "foreign-part", "oversized", "unknown-status"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if mode == "unknown-status" {
					_ = json.NewEncoder(w).Encode(map[string]any{"ses_original": map[string]string{"type": "unrecognized"}})
					return
				}
				count := 51
				if mode == "oversized" {
					count = maxHistoryUpdates + 1
				}
				rows := make([]any, 0, count)
				for i := range count {
					id := fmt.Sprint("msg_", i)
					sid := "ses_original"
					if mode == "foreign-part" {
						sid = "ses_foreign"
					}
					rows = append(rows, map[string]any{
						"info":  map[string]string{"id": id, "sessionID": "ses_original", "role": "assistant"},
						"parts": []any{map[string]any{"type": "tool", "sessionID": sid, "messageID": id, "callID": fmt.Sprint("call_", i), "tool": "bash", "state": map[string]any{"status": "completed", "input": map[string]string{"command": "pwd"}, "output": "native output"}}},
					})
				}
				_ = json.NewEncoder(w).Encode(rows)
			}))
			defer server.Close()
			c := newOpenCodeHTTP(ExternalAttachment{ExternalConnection: ExternalConnection{Transport: "opencode-http-v1", Endpoint: server.URL}})
			defer c.client.CloseIdleConnections()
			var page struct {
				Data []openCodeMessage `json:"data"`
			}
			path := "/api/session/ses_original/message?limit=50&order=asc"
			if mode == "unknown-status" {
				path = "/api/session/active"
			}
			err := c.request(context.Background(), "GET", path, nil, &page)
			if mode != "messages" {
				if err == nil {
					t.Fatal("incompatible or truncated V1 snapshot accepted")
				}
				return
			}
			if err != nil || len(page.Data) != 51 {
				t.Fatal("V1 transcript was truncated to a V2 page", len(page.Data), err)
			}
			var tool struct {
				ID    string `json:"id"`
				State struct {
					Content string `json:"content"`
				} `json:"state"`
			}
			if json.Unmarshal(page.Data[0].Content[0], &tool) != nil || tool.ID != "call_0" || tool.State.Content != "native output" || page.Data[0].ID != "msg_0" {
				t.Fatal("native tool/message association lost", tool)
			}
		})
	}
}

func TestOpenCodeV1NativeBooleanRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "false")
	}))
	defer server.Close()
	c := newOpenCodeHTTP(ExternalAttachment{ExternalConnection: ExternalConnection{Transport: "opencode-http-v1", Endpoint: server.URL}})
	defer c.client.CloseIdleConnections()
	for _, path := range []string{"/api/session/ses_original/interrupt?resume=false", "/api/session/ses_original/permission/per_native/reply"} {
		if err := c.request(context.Background(), "POST", path, map[string]string{"decision": "once"}, nil); err == nil {
			t.Fatal("native false response was reported as a successful operation", path)
		}
	}
}
