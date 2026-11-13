// SPDX-License-Identifier: Apache-2.0
package host

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func testACP() (*structuredAgent, *bytes.Buffer) {
	out := &bytes.Buffer{}
	a := newStructuredAgent("opencode", out, "/workspace")
	a.input = discardAgentInput{}
	return a, out
}
func readyACP(t *testing.T, a *structuredAgent) {
	t.Helper()
	if err := a.initialize(); err != nil {
		t.Fatal(err)
	}
	init := queuedFrame(t, a)
	if init["jsonrpc"] != "2.0" || init["params"].(map[string]any)["protocolVersion"] != float64(1) {
		t.Fatal(init)
	}
	receiveAgentFrameForTest(a, []byte(`{"jsonrpc":"2.0","id":"host-init","result":{"protocolVersion":1,"agentCapabilities":{"future":true}}}`))
	session := queuedFrame(t, a)
	if session["method"] != "session/new" || session["params"].(map[string]any)["cwd"] != "/workspace" {
		t.Fatal(session)
	}
	receiveAgentFrameForTest(a, []byte(`{"jsonrpc":"2.0","id":"host-session","result":{"sessionId":"acp-session","futureField":true}}`))
	if err := <-a.startup; err != nil {
		t.Fatal(err)
	}
}
func TestACPVersionNegotiationAndUnknownMethods(t *testing.T) {
	for _, version := range []string{`2`, `null`, `"1"`} {
		a, _ := testACP()
		receiveAgentFrameForTest(a, []byte(`{"jsonrpc":"2.0","id":"host-init","result":{"protocolVersion":`+version+`}}`))
		if err := <-a.startup; err == nil {
			t.Fatal("unsupported version accepted:", version)
		}
	}
	a, out := testACP()
	readyACP(t, a)
	before := out.Len()
	receiveAgentFrameForTest(a, []byte(`{"jsonrpc":"2.0","method":"future/notification","params":{"new":"shape"}}`))
	if out.Len() != before {
		t.Fatal("unknown optional notification was not ignored")
	}
	receiveAgentFrameForTest(a, []byte(`{"jsonrpc":"2.0","id":7,"method":"fs/read_text_file","params":{}}`))
	if queuedFrame(t, a)["error"] == nil {
		t.Fatal("unsupported capability was accepted")
	}
}
func TestACPPromptUpdatesAndCompletion(t *testing.T) {
	a, out := testACP()
	readyACP(t, a)
	req := AgentMessage{Action: "prompt", RequestID: "prompt-1", Text: "hello"}
	if err := a.message(req); err != nil {
		t.Fatal(err)
	}
	frame := queuedFrame(t, a)
	if frame["method"] != "session/prompt" {
		t.Fatal(frame)
	}
	if err := a.message(req); err != nil || len(a.rpc.queue) != 0 {
		t.Fatal("duplicate turn dispatched", err)
	}
	receiveAgentFrameForTest(a, []byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"another-session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"wrong session"}}}}`))
	receiveAgentFrameForTest(a, []byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"acp-session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello ACP"},"future":true}}}`))
	receiveAgentFrameForTest(a, []byte(`{"jsonrpc":"2.0","id":"host-1","result":{"stopReason":"end_turn"}}`))
	if a.busy || !strings.Contains(out.String(), "hello ACP") || strings.Contains(out.String(), "wrong session") || strings.Contains(out.String(), "session/update") {
		t.Fatal(out.String())
	}
}
func TestACPPermissionsUseOnceAndCancellationRespondsToPendingRequests(t *testing.T) {
	a, out := testACP()
	readyACP(t, a)
	if err := a.message(AgentMessage{Action: "prompt", RequestID: "p", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	_ = queuedFrame(t, a)
	permission := `{"jsonrpc":"2.0","id":42,"method":"session/request_permission","params":{"sessionId":"acp-session","toolCall":{"toolCallId":"tool-1","title":"Edit source","kind":"edit","content":[{"type":"diff","path":"main.go","oldText":"old","newText":"new"}]},"options":[{"optionId":"once-id","kind":"allow_once"},{"optionId":"always-id","kind":"allow_always"},{"optionId":"no-id","kind":"reject_once"}]}}`
	receiveAgentFrameForTest(a, []byte(permission))
	if !strings.Contains(out.String(), "main.go") {
		t.Fatal("permission lost file preview", out.String())
	}
	if err := a.message(AgentMessage{Action: "approval", RequestID: "yes", ApprovalID: "42", Decision: "accept"}); err != nil {
		t.Fatal(err)
	}
	response := queuedFrame(t, a)
	outcome := response["result"].(map[string]any)["outcome"].(map[string]any)
	if outcome["optionId"] != "once-id" {
		t.Fatal("approval scope expanded", response)
	}
	receiveAgentFrameForTest(a, []byte(permission))
	if err := a.message(AgentMessage{Action: "interrupt", RequestID: "stop"}); err != nil {
		t.Fatal(err)
	}
	batch := <-a.rpc.queue
	if len(batch) != 2 || batch[0]["method"] != "session/cancel" || batch[1]["result"] == nil {
		t.Fatalf("cancel operations: %v", batch)
	}
	if !a.busy || len(a.approvals) != 0 {
		t.Fatal("cancellation must await prompt result and resolve pending approvals")
	}
	receiveAgentFrameForTest(a, []byte(permission))
	if queuedFrame(t, a)["result"] == nil {
		t.Fatal("new permission during cancel remained pending")
	}
	receiveAgentFrameForTest(a, []byte(`{"jsonrpc":"2.0","id":"host-1","result":{"stopReason":"cancelled"}}`))
	if a.busy {
		t.Fatal("cancelled prompt remained active")
	}
}
func TestACPDoesNotConvertAlwaysPermissionToOnce(t *testing.T) {
	a, _ := testACP()
	readyACP(t, a)
	receiveAgentFrameForTest(a, []byte(`{"jsonrpc":"2.0","id":"approval","method":"session/request_permission","params":{"sessionId":"acp-session","toolCall":{"toolCallId":"tool","title":"tool"},"options":[{"optionId":"always","kind":"allow_always"}]}}`))
	if err := a.message(AgentMessage{Action: "approval", RequestID: "yes", ApprovalID: `"approval"`, Decision: "accept"}); err == nil {
		t.Fatal("always permission was granted through one-time UI")
	}
	if err := a.message(AgentMessage{Action: "approval", RequestID: "no", ApprovalID: `"approval"`, Decision: "decline"}); err != nil {
		t.Fatal(err)
	}
	response := queuedFrame(t, a)
	body, _ := json.Marshal(response)
	if !strings.Contains(string(body), "cancelled") {
		t.Fatal(response)
	}
}
func TestACPToolPartialUpdatesPreserveDescription(t *testing.T) {
	a, out := testACP()
	readyACP(t, a)
	receiveAgentFrameForTest(a, []byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"acp-session","update":{"sessionUpdate":"tool_call","toolCallId":"tool","title":"Read source","kind":"read","rawInput":"freeform input"}}}`))
	receiveAgentFrameForTest(a, []byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"acp-session","update":{"sessionUpdate":"tool_call_update","toolCallId":"tool","status":"completed","content":[{"type":"content","content":{"type":"text","text":"file contents"}}]}}}`))
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	last := lines[len(lines)-1]
	if !strings.Contains(last, "Read source") || !strings.Contains(last, "file contents") || !strings.Contains(last, `"status":"completed"`) {
		t.Fatal(last)
	}
}
