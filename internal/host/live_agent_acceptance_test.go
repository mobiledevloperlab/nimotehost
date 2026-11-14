// SPDX-License-Identifier: Apache-2.0
package host

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in: the installed provider and isolated credentials/model config
// are supplied by the operator. No account, install or model call in normal CI.
func TestLiveAgentApprovalDeclineAndConversationResume(t *testing.T) {
	provider := os.Getenv("NIMOTE_TEST_LIVE_AGENT")
	if provider == "" {
		t.Skip("set NIMOTE_TEST_LIVE_AGENT with an installed provider and isolated read-only approval configuration")
	}
	if !supportsStructuredAgent(provider) {
		t.Fatal("unsupported live provider")
	}
	m := newTestManager(t)
	workspace := t.TempDir()
	api := &Server{manager: m, token: "acceptance-owner"}
	start := func(conversation string) Session {
		req := StartRequest{Kind: "agent", Agent: provider, Interaction: "structured", ExecutionPolicy: "confirm", Directory: workspace}
		if conversation != "" {
			req.Resume = &AgentResume{ConversationID: conversation}
		}
		record, err := m.Start(req)
		if err != nil {
			t.Fatal("live provider startup failed", err)
		}
		m.mu.Lock()
		agent := m.sessions[record.ID].agent
		m.mu.Unlock()
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case startupErr := <-agent.startup:
				if startupErr != nil {
					t.Fatal("live handshake rejected", startupErr, agent.diagnostics.text())
				}
			default:
			}
			snapshot, err := m.AgentSnapshot(record.ID)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot["ready"] == true {
				ready, err := m.Get(record.ID)
				if err != nil {
					t.Fatal(err)
				}
				return ready
			}
			if snapshot["status"] == "disconnected" {
				t.Fatal("live provider disconnected during startup")
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("live provider did not become ready", agent.diagnostics.text())
		return Session{}
	}
	send := func(id string, req AgentMessage) {
		body, _ := json.Marshal(req)
		w := deviceRequest(api, "acceptance-owner", "POST", "/v1/sessions/"+id+"/agent-message", string(body))
		if w.Code != 200 {
			t.Fatalf("live semantic operation failed HTTP %d: %s", w.Code, w.Body.String())
		}
	}
	turn := func(record Session, prompt, decision string) int {
		requestID, err := NewRequestID()
		if err != nil {
			t.Fatal(err)
		}
		send(record.ID, AgentMessage{Action: "prompt", RequestID: requestID, Text: prompt})
		approvals := 0
		seen := map[string]bool{}
		deadline := time.Now().Add(120 * time.Second)
		for time.Now().Before(deadline) {
			state, err := m.AgentSnapshot(record.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, raw := range state["approvals"].([]any) {
				detail := raw.(map[string]any)
				id, _ := detail["id"].(string)
				if id == "" || seen[id] {
					continue
				}
				seen[id] = true
				approvals++
				key, err := NewRequestID()
				if err != nil {
					t.Fatal(err)
				}
				req := AgentMessage{Action: "approval", RequestID: key, ApprovalID: id, Decision: decision}
				send(record.ID, req)
				send(record.ID, req) // Same durable HTTP receipt, one native reply.
			}
			if state["busy"] == false {
				if state["status"] == "failed" || state["status"] == "disconnected" {
					data, _, _ := m.Output(record.ID, 0)
					t.Fatal("live turn failed", state["status"], string(data))
				}
				return approvals
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("live turn exceeded acceptance budget")
		return 0
	}
	first := start("")
	marker, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	prompt := fmt.Sprintf("Remember the marker %s for this conversation. Create approved.txt containing exactly that marker. Request native permission when needed. Do not access outside this workspace. Reply DONE when finished.", marker)
	approved := turn(first, prompt, "accept")
	data, err := os.ReadFile(filepath.Join(workspace, "approved.txt"))
	if err != nil || strings.TrimSpace(string(data)) != marker {
		t.Fatal("real Agent did not create approved output", err)
	}
	if approved == 0 {
		t.Fatal("live configuration did not exercise native approvals; use read-only/request-approval provider configuration")
	}
	conversation := first.ConversationID
	if conversation == "" {
		t.Fatal("native conversation identity missing")
	}
	if err := m.Cancel(first.ID); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, first.ID)
	if err := os.Remove(filepath.Join(workspace, "approved.txt")); err != nil {
		t.Fatal(err)
	}
	resumed := start(conversation)
	if resumed.ID == first.ID || resumed.ConversationID != conversation {
		t.Fatal("native conversation was silently replaced")
	}
	turn(resumed, "Using the marker remembered from the previous turn, create resumed.txt containing exactly that marker. Do not read any files or use discovery commands to find it. Request native permission when needed. Reply DONE.", "accept")
	data, err = os.ReadFile(filepath.Join(workspace, "resumed.txt"))
	if err != nil || strings.TrimSpace(string(data)) != marker {
		t.Fatal("native resumed context did not preserve marker", err)
	}
	declined := turn(resumed, "Create denied.txt containing DENIED. If permission is declined, stop immediately and do not retry or use another tool to create it.", "decline")
	if declined == 0 {
		t.Fatal("decline did not exercise a native permission request")
	}
	if _, err := os.Stat(filepath.Join(workspace, "denied.txt")); !os.IsNotExist(err) {
		t.Fatal("declined work created its file", err)
	}
	t.Logf("live provider=%s: approved=%d declined=%d; native conversation resume preserved context", provider, approved, declined)
}
