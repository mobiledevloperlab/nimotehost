// SPDX-License-Identifier: Apache-2.0
package host

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCodexStableEventsTolerateAdditiveUpgrades(t *testing.T) {
	a, out := testAgent()
	a.receive([]byte(`{"id":"host-init","result":{"userAgent":"codex/999.0","futureCapability":{"enabled":true}},"futureEnvelope":123}`))
	_ = queuedFrame(t, a)
	_ = queuedFrame(t, a)
	a.receive([]byte(`{"id":"host-thread","result":{"thread":{"id":"future-thread","futureField":[1,2,3]},"futureResult":true}}`))
	if err := <-a.startup; err != nil {
		t.Fatal(err)
	}
	before := out.Len()
	// Unknown notifications must be ignored even when their payload shape bears
	// no relationship to known Codex events.
	a.receive([]byte(`{"method":"future/notification","params":{"item":"new type"}}`))
	if out.Len() != before {
		t.Fatal("optional unknown event broke the client contract")
	}
	a.receive([]byte(`{"method":"item/agentMessage/delta","params":{"itemId":"a","delta":"hello","futureField":{"x":true}}}`))
	a.receive([]byte(`{"method":"item/completed","params":{"item":{"id":"cmd","type":"commandExecution","command":"echo hi","status":"completed","aggregatedOutput":"hi","futureState":"anything"}}}`))
	if strings.Contains(out.String(), "item/agentMessage") || strings.Contains(out.String(), "futureField") {
		t.Fatal("provider fields leaked to stable client events")
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var frame struct {
		Type string         `json:"type"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Type != "tool" || frame.Data["kind"] != "command" || frame.Data["output"] != "hi" {
		t.Fatal(frame)
	}
}

func TestFileApprovalKeepsChangesAndRequestedRoot(t *testing.T) {
	a, _ := testAgent()
	a.receive([]byte(`{"method":"item/started","params":{"item":{"id":"file-1","type":"fileChange","status":"inProgress","changes":[{"path":"main.go","diff":"+hello"}]}}}`))
	a.receive([]byte(`{"id":9,"method":"item/fileChange/requestApproval","params":{"itemId":"file-1","grantRoot":"/workspace","reason":"write files"}}`))
	approval := a.approvalDetails["9"]
	changes := approval["changes"].([]map[string]string)
	if len(changes) != 1 || changes[0]["path"] != "main.go" || approval["grant_root"] != "/workspace" {
		t.Fatal(approval)
	}
	// Path-keyed provider changes normalize to the same array contract.
	fromMap := normalizeChanges(json.RawMessage(`{"main.go":{"diff":"+hello"}}`))
	if len(fromMap) != 1 || fromMap[0]["diff"] != "+hello" {
		t.Fatal(fromMap)
	}
}

func TestCodexServerResolvedApproval(t *testing.T) {
	for _, id := range []string{`42`, `"approval-42"`} {
		t.Run(id, func(t *testing.T) {
			a, out := testAgent()
			a.receive([]byte(`{"id":` + id + `,"method":"item/commandExecution/requestApproval","params":{"command":"echo hello"}}`))
			resolved := []byte(`{"method":"serverRequest/resolved","params":{"requestId":` + id + `}}`)
			a.receive(resolved)
			if len(a.approvals) != 0 || len(a.approvalDetails) != 0 || !strings.Contains(out.String(), `"type":"approval_resolved"`) {
				t.Fatal("resolved approval remains pending", out.String())
			}
			before := out.Len()
			a.receive(resolved)
			if out.Len() != before {
				t.Fatal("duplicate resolution emitted an event")
			}
			if err := a.message(AgentMessage{Action: "approval", RequestID: "late", ApprovalID: id, Decision: "accept"}); err == nil {
				t.Fatal("resolved approval accepted")
			}
		})
	}
}
