// SPDX-License-Identifier: Apache-2.0
package host

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

type discardAgentInput struct{}

func (discardAgentInput) Write(p []byte) (int, error) { return len(p), nil }
func (discardAgentInput) Close() error                { return nil }
func testAgent() (*structuredAgent, *bytes.Buffer) {
	output := &bytes.Buffer{}
	a := newStructuredAgent("codex", output, "/workspace")
	a.input = discardAgentInput{}
	return a, output
}
func queuedFrame(t *testing.T, a *structuredAgent) map[string]any {
	t.Helper()
	select {
	case batch := <-a.rpc.queue:
		if len(batch) != 1 {
			t.Fatalf("expected single operation, got %v", batch)
		}
		frame := batch[0]
		if a.acp != nil {
			frame["jsonrpc"] = "2.0"
		}
		data, _ := json.Marshal(frame)
		var normalized map[string]any
		_ = json.Unmarshal(data, &normalized)
		return normalized
	default:
		t.Fatal("no queued RPC operation")
		return nil
	}
}
func TestCodexHandshakePromptDedupAndInterrupt(t *testing.T) {
	a, out := testAgent()
	if err := a.initialize(); err != nil {
		t.Fatal(err)
	}
	if queuedFrame(t, a)["method"] != "initialize" {
		t.Fatal("missing initialize")
	}
	a.receive([]byte(`{"id":"host-init","result":{}}`))
	if queuedFrame(t, a)["method"] != "initialized" {
		t.Fatal("missing initialized notification")
	}
	frame := queuedFrame(t, a)
	if frame["method"] != "thread/start" || frame["params"].(map[string]any)["cwd"] != "/workspace" {
		t.Fatal(frame)
	}
	a.receive([]byte(`{"id":"host-thread","result":{"thread":{"id":"thread-1"}}}`))
	if !strings.Contains(out.String(), `"type":"ready"`) {
		t.Fatal(out.String())
	}
	req := AgentMessage{Action: "prompt", RequestID: "request-1", Text: "hello"}
	if err := a.message(req); err != nil {
		t.Fatal(err)
	}
	frame = queuedFrame(t, a)
	if frame["params"].(map[string]any)["threadId"] != "thread-1" {
		t.Fatal(frame)
	}
	if err := a.message(req); err != nil {
		t.Fatal(err)
	}
	if len(a.rpc.queue) != 0 {
		t.Fatal("duplicate prompt was dispatched")
	}
	if err := a.message(AgentMessage{Action: "prompt", RequestID: "request-2", Text: "second"}); err == nil {
		t.Fatal("concurrent turn accepted")
	}
	a.receive([]byte(`{"method":"turn/started","params":{"turn":{"id":"turn-1"}}}`))
	if err := a.message(AgentMessage{Action: "interrupt", RequestID: "stop-1"}); err != nil {
		t.Fatal(err)
	}
	if queuedFrame(t, a)["params"].(map[string]any)["turnId"] != "turn-1" {
		t.Fatal("wrong turn")
	}
	a.receive([]byte(`{"method":"turn/completed","params":{"turn":{"id":"turn-1"}}}`))
	if a.busy || a.turn != "" {
		t.Fatal("turn remained active")
	}
}
func TestCodexApprovalsRejectArbitraryResponsesAndUnsupportedRequests(t *testing.T) {
	a, out := testAgent()
	a.receive([]byte(`{"id":42,"method":"item/commandExecution/requestApproval","params":{"command":"echo hello"}}`))
	if !strings.Contains(out.String(), `"type":"approval"`) {
		t.Fatal(out.String())
	}
	req := AgentMessage{Action: "approval", RequestID: "approval-1", ApprovalID: "42", Decision: "anything"}
	if a.message(req) == nil {
		t.Fatal("invalid decision accepted")
	}
	req.Decision = "accept"
	if err := a.message(req); err != nil {
		t.Fatal(err)
	}
	frame := queuedFrame(t, a)
	if frame["id"] != float64(42) || frame["result"].(map[string]any)["decision"] != "accept" {
		t.Fatal(frame)
	}
	if err := a.message(req); err != nil {
		t.Fatal("idempotent approval rejected", err)
	}
	req.RequestID = "approval-2"
	if a.message(req) == nil {
		t.Fatal("stale approval accepted")
	}
	a.receive([]byte(`{"id":"unknown","method":"tool/unknown","params":{}}`))
	if queuedFrame(t, a)["error"] == nil {
		t.Fatal("unsupported interaction must receive an RPC error")
	}
}
func TestCodexInputQueueBoundsAndTurnErrors(t *testing.T) {
	a, out := testAgent()
	a.thread = "thread-1"
	for i := 0; i < cap(a.rpc.queue); i++ {
		if err := a.send(map[string]any{"method": "test"}); err != nil {
			t.Fatal(err)
		}
	}
	if a.message(AgentMessage{Action: "prompt", RequestID: "retry", Text: "hello"}) == nil {
		t.Fatal("full queue accepted")
	}
	if a.busy || a.seen["retry"] {
		t.Fatal("rejected request mutated turn state")
	}
	<-a.rpc.queue
	if err := a.message(AgentMessage{Action: "prompt", RequestID: "retry", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	a.receive([]byte(`{"id":"host-2","error":{"code":-1,"message":"failure"}}`))
	if a.busy || !strings.Contains(out.String(), `"type":"turn_rejected"`) {
		t.Fatal(out.String())
	}
	a.close()
	if a.message(AgentMessage{Action: "interrupt", RequestID: "closed"}) == nil {
		t.Fatal("closed agent accepted input")
	}
}
func TestCodexJSONLFraming(t *testing.T) {
	out := &bytes.Buffer{}
	w := &agentLines{writer: out}
	for _, chunk := range []string{`{"method":"item/agentMessage/`, "delta\",\"params\":{\"delta\":\"\u4f60\u597d\"}}\n{\"method\":\"turn/completed\",\"params\":{}}\n"} {
		if _, err := io.WriteString(w, chunk); err != nil {
			t.Fatal(err)
		}
	}
	if bytes.Count(out.Bytes(), []byte("\n")) != 2 || !strings.Contains(out.String(), "\u4f60\u597d") {
		t.Fatal(out.String())
	}
	if _, err := w.Write(bytes.Repeat([]byte("a"), (2<<20)+1)); err == nil {
		t.Fatal("unbounded frame accepted")
	}
}

// These fixtures test Host projections without replacing the upstream SDK's
// own transport tests. Integration tests use real SDK connections and pipes.
func receiveAgentFrameForTest(a *structuredAgent, data []byte) {
	if a.acp == nil {
		var frame codexFrame
		if json.Unmarshal(data, &frame) == nil {
			a.receiveCodex(frame)
		}
		return
	}
	var frame struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	_ = json.Unmarshal(data, &frame)
	if frame.Method == "" {
		var id string
		_ = json.Unmarshal(frame.ID, &id)
		a.receiveACPResult(id, frame.Result, frame.Error)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	if frame.Method == "session/update" {
		a.updateACP(frame.Params)
	} else if frame.Method == "session/request_permission" {
		a.permissionACP(frame.ID, frame.Params)
	} else if len(frame.ID) > 0 {
		_ = a.send(map[string]any{"id": frame.ID, "error": map[string]any{"code": -32601}})
	}
}
func queuedAgentOperations(a *structuredAgent) int {
	if a.acp != nil {
		return len(a.rpc.queue)
	}
	return len(a.rpc.queue)
}

func (a *structuredAgent) receive(data []byte) { receiveAgentFrameForTest(a, data) }
