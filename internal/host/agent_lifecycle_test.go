// SPDX-License-Identifier: Apache-2.0
package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

type brokenAgentInput struct{ short bool }

func (w brokenAgentInput) Write(p []byte) (int, error) {
	if w.short {
		return len(p) - 1, nil
	}
	return 0, errors.New("broken stdin")
}
func (brokenAgentInput) Close() error { return nil }

func TestAgentWriteFailureStopsProcessAndRejectsOperations(t *testing.T) {
	for _, provider := range []string{"codex", "opencode", "claude"} {
		for _, short := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/short=%t", provider, short), func(t *testing.T) {
				a, out := testAgent()
				if provider != "codex" {
					a = newStructuredAgent(provider, out, "/workspace")
				}
				a.input = brokenAgentInput{short: short}
				a.thread = "thread"
				stops := 0
				a.stopProcess = func() error { stops++; return nil }
				if err := a.message(AgentMessage{Action: "prompt", RequestID: "p", Text: "hello"}); err != nil {
					t.Fatal(err)
				}
				a.writeLoop()
				if stops != 1 || !a.closed || a.busy || len(a.pending) != 0 {
					t.Fatalf("failure did not settle state: stops=%d closed=%t busy=%t", stops, a.closed, a.busy)
				}
				if err := a.send(map[string]string{"method": "late"}); err == nil {
					t.Fatal("dead writer accepted a frame")
				}
				if err := a.message(AgentMessage{Action: "prompt", RequestID: "next", Text: "hello"}); err == nil {
					t.Fatal("failed adapter accepted a prompt")
				}
				if !strings.Contains(out.String(), `"status":"failed"`) {
					t.Fatal("missing failed turn event", out.String())
				}
				a.close()
				a.fail(errors.New("late failure"))
				if stops != 1 {
					t.Fatal("failure cleanup repeated")
				}
			})
		}
	}
}

func TestRequestLimitAllowsApprovalAndInterrupt(t *testing.T) {
	for _, provider := range []string{"codex", "opencode", "claude"} {
		t.Run(provider, func(t *testing.T) {
			a := newStructuredAgent(provider, &strings.Builder{}, "/workspace")
			a.input = discardAgentInput{}
			a.thread, a.turn, a.busy = "thread", "turn", true
			for i := 0; i < 4096; i++ {
				a.seen[fmt.Sprint(i)] = true
			}
			a.approvals["42"] = json.RawMessage(`42`)
			if a.acp != nil {
				a.acp.options["42"] = map[string]string{"allow_once": "yes"}
			}
			for _, req := range []AgentMessage{
				{Action: "approval", RequestID: "approve", ApprovalID: "42", Decision: "accept"},
				{Action: "interrupt", RequestID: "stop"},
			} {
				if err := a.message(req); err != nil {
					t.Fatal(req.Action, err)
				}
				_ = queuedFrame(t, a)
				if err := a.message(req); err != nil || len(a.queue) != 0 {
					t.Fatal("control retry was not deduplicated", err)
				}
			}
			a.busy = false
			if err := a.message(AgentMessage{Action: "prompt", RequestID: "new", Text: "hello"}); err == nil {
				t.Fatal("new prompt bypassed request limit")
			}
		})
	}
}

func TestClosedAgentSnapshotAndLateEvents(t *testing.T) {
	for _, provider := range []string{"codex", "opencode", "claude"} {
		t.Run(provider, func(t *testing.T) {
			m := newTestManager(t)
			file, err := os.CreateTemp(t.TempDir(), "output")
			if err != nil {
				t.Fatal(err)
			}
			output := &outputWriter{file: file}
			defer output.Close()
			a := newStructuredAgent(provider, output, "/workspace")
			a.thread, a.turn, a.busy = "thread", "turn", true
			a.approvals["42"] = json.RawMessage(`42`)
			a.approvalDetails["42"] = map[string]any{"id": "42"}
			m.sessions["test"] = &managed{agent: a, output: output}
			a.close()
			if a.acp == nil {
				a.receive([]byte(`{"method":"turn/started","params":{"turn":{"id":"late"}}}`))
			} else {
				a.receive([]byte(`{"jsonrpc":"2.0","id":43,"method":"session/request_permission","params":{"sessionId":"thread","toolCall":{"toolCallId":"late"}}}`))
			}
			state, err := m.AgentSnapshot("test")
			if err != nil || state["ready"] != false || state["busy"] != false || len(state["approvals"].([]any)) != 0 {
				t.Fatal("stale closed snapshot", state, err)
			}
			data, _, _, err := m.OutputPage("test", 0)
			if err != nil || !strings.Contains(string(data), `"type":"approval_resolved"`) || !strings.Contains(string(data), `"type":"turn_completed"`) {
				t.Fatal("final events missing", string(data), err)
			}
		})
	}
}
