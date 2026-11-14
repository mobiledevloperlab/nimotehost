// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPermissionContextRejectsForeignStaleIdleAndCanceled(t *testing.T) {
	for _, provider := range []string{"codex", "opencode", "claude"} {
		t.Run(provider, func(t *testing.T) {
			a := newStructuredAgent(provider, io.Discard, t.TempDir())
			a.thread = "thread"
			a.turn = "turn"
			a.busy = true
			method := "item/commandExecution/requestApproval"
			cases := []string{`{"threadId":"other"}`, `{"turnId":"old"}`, `{"threadId":4}`, `{"threadId":null}`, `[]`}
			good := `{"threadId":"thread","turnId":"turn"}`
			if a.acp != nil {
				method = "session/request_permission"
				cases = []string{`{"sessionId":"other"}`, `{}`, `{"sessionId":4}`}
				good = `{"sessionId":"thread"}`
			}
			for _, raw := range cases {
				_, err := a.handleAgentRPC(context.Background(), method, json.RawMessage(raw))
				if err == nil || len(a.rpc.permissions) != 0 || len(a.approvals) != 0 {
					t.Fatal("invalid context published", raw)
				}
			}
			a.busy = false
			if _, err := a.handleAgentRPC(context.Background(), method, json.RawMessage(good)); err == nil {
				t.Fatal("idle permission accepted")
			}
			a.busy = true
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := a.handleAgentRPC(ctx, method, json.RawMessage(good)); err == nil {
				t.Fatal("canceled permission accepted")
			}
		})
	}
}
func TestNativeDecisionsAndConcurrentApprovalConsumption(t *testing.T) {
	a := newStructuredAgent("codex", io.Discard, t.TempDir())
	a.input = discardAgentInput{}
	a.receiveCodex(codexFrame{ID: json.RawMessage(`42`), Method: "item/commandExecution/requestApproval", Params: json.RawMessage(`{"availableDecisions":["decline"]}`)})
	if err := a.message(AgentMessage{Action: "approval", RequestID: "deny", ApprovalID: "42", Decision: "accept"}); err == nil {
		t.Fatal("unoffered accept allowed")
	}
	if len(a.approvals) != 1 || queuedAgentOperations(a) != 0 {
		t.Fatal("rejected approval changed pending state")
	}
	for _, provider := range []string{"codex", "opencode", "claude"} {
		t.Run(provider, func(t *testing.T) {
			a := newStructuredAgent(provider, io.Discard, t.TempDir())
			a.input = discardAgentInput{}
			a.approvals["42"] = json.RawMessage(`42`)
			if a.acp != nil {
				a.acp.options["42"] = map[string]string{"allow_once": "once"}
			}
			var wg sync.WaitGroup
			var accepted atomic.Int32
			for i := range 32 {
				wg.Go(func() {
					if a.message(AgentMessage{Action: "approval", RequestID: fmt.Sprint(i), ApprovalID: "42", Decision: "accept"}) == nil {
						accepted.Add(1)
					}
				})
			}
			wg.Wait()
			if accepted.Load() != 1 || queuedAgentOperations(a) != 1 {
				t.Fatalf("duplicate native approval %d", accepted.Load())
			}
		})
	}
}
func TestTurnEndRejectsQueuedPermissionAndReleasesHandler(t *testing.T) {
	for _, provider := range []string{"codex", "opencode", "claude"} {
		t.Run(provider, func(t *testing.T) {
			a := newStructuredAgent(provider, io.Discard, t.TempDir())
			a.input = discardAgentInput{}
			a.thread = "thread"
			a.turn = "turn"
			a.busy = true
			method := "item/commandExecution/requestApproval"
			raw := json.RawMessage(`{"threadId":"thread","turnId":"turn","command":"pwd"}`)
			if a.acp != nil {
				method = "session/request_permission"
				raw = json.RawMessage(`{"sessionId":"thread","toolCall":{"toolCallId":"tool"},"options":[{"optionId":"once","kind":"allow_once"}]}`)
			}
			result := make(chan any, 1)
			go func() {
				value, err := a.handleAgentRPC(context.Background(), method, raw)
				if err != nil {
					result <- err
				} else {
					result <- value
				}
			}()
			var key string
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				a.mu.Lock()
				for id := range a.approvals {
					key = id
				}
				a.mu.Unlock()
				if key != "" {
					break
				}
				time.Sleep(time.Millisecond)
			}
			if key == "" {
				a.close()
				t.Fatal("permission not published")
			}
			if err := a.message(AgentMessage{Action: "approval", RequestID: "approve", ApprovalID: key, Decision: "accept"}); err != nil {
				a.close()
				t.Fatal(err)
			}
			// Dispatcher has not consumed the accept. Native turn termination wins.
			a.mu.Lock()
			if a.acp != nil {
				a.finishACP("end_turn")
			} else {
				a.receiveCodexLocked(codexFrame{Method: "turn/completed", Params: json.RawMessage(`{"turn":{"id":"turn","status":"completed"}}`)})
			}
			a.mu.Unlock()
			select {
			case value := <-result:
				encoded, _ := json.Marshal(value)
				if string(encoded) != `{"decision":"decline"}` && string(encoded) != `{"outcome":{"outcome":"cancelled"}}` {
					t.Fatalf("stale accept survived: %s", encoded)
				}
			case <-time.After(time.Second):
				a.close()
				t.Fatal("native permission handler leaked")
			}
			a.mu.Lock()
			count := len(a.rpc.permissions)
			a.mu.Unlock()
			if count != 0 {
				t.Fatal("permission handle leaked")
			}
		})
	}
}

func TestCoreRequestIDsBindMessageContentAcrossProviders(t *testing.T) {
	for _, provider := range []string{"codex", "opencode", "claude"} {
		t.Run(provider, func(t *testing.T) {
			a := newStructuredAgent(provider, io.Discard, t.TempDir())
			a.input = discardAgentInput{}
			a.thread = "thread"
			req := AgentMessage{Action: "prompt", RequestID: "same", Text: "first"}
			if err := a.message(req); err != nil {
				t.Fatal(err)
			}
			if err := a.message(req); err != nil || queuedAgentOperations(a) != 1 {
				t.Fatal("exact retry not deduplicated", err)
			}
			req.Text = "different"
			if err := a.message(req); err != errRequestConflict || queuedAgentOperations(a) != 1 {
				t.Fatal("changed content was acknowledged", err)
			}
			req.Action = "interrupt"
			if err := a.message(req); err != errRequestConflict {
				t.Fatal("request ID reused for another action", err)
			}
		})
	}
}
