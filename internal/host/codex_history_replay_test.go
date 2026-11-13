// SPDX-License-Identifier: Apache-2.0
package host

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCodexResumePersistsPaginatedHistoryBeforeReady(t *testing.T) {
	a, out := testAgent()
	a.resumeConversation = "saved-thread"
	a.receive([]byte(`{"id":"host-thread","result":{"thread":{"id":"saved-thread"}}}`))
	frame := queuedFrame(t, a)
	params := frame["params"].(map[string]any)
	if frame["method"] != "thread/items/list" || params["threadId"] != "saved-thread" || params["sortDirection"] != "asc" {
		t.Fatal(frame)
	}
	if err := a.message(AgentMessage{Action: "prompt", RequestID: "early", Text: "continue"}); err == nil {
		t.Fatal("accepted prompt before history finished")
	}
	a.receive([]byte(`{"id":"host-history","result":{"data":[{"item":{"type":"userMessage","id":"u","content":[{"type":"text","text":"original request"}]}},{"item":{"type":"reasoning","id":"private","text":"hidden"}}],"nextCursor":"page-2"}}`))
	frame = queuedFrame(t, a)
	if frame["params"].(map[string]any)["cursor"] != "page-2" {
		t.Fatal(frame)
	}
	if a.thread != "" || strings.Contains(out.String(), `"type":"ready"`) {
		t.Fatal("ready before final history page")
	}
	a.receive([]byte(`{"id":"host-history","result":{"data":[{"item":{"type":"agentMessage","id":"a","text":"previous response"}},{"item":{"type":"commandExecution","id":"t","command":"pwd","status":"completed"}}],"nextCursor":null}}`))
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 || a.thread != "saved-thread" || a.busy || len(a.approvals) != 0 {
		t.Fatal(out.String())
	}
	for i, want := range []string{"history", "history", "history", "ready"} {
		var event map[string]any
		if json.Unmarshal([]byte(lines[i]), &event) != nil || event["type"] != want {
			t.Fatal(lines[i])
		}
	}
	if strings.Contains(out.String(), "hidden") || !strings.Contains(out.String(), "previous response") {
		t.Fatal(out.String())
	}
	if err := a.message(AgentMessage{Action: "prompt", RequestID: "next", Text: "continue"}); err != nil {
		t.Fatal(err)
	}
	if queuedFrame(t, a)["method"] != "turn/start" {
		t.Fatal("continuation was not dispatched")
	}
}

func TestCodexResumeHistoryErrorsDoNotReportReady(t *testing.T) {
	for _, response := range []string{
		`{"id":"host-history","error":{"code":-1,"message":"unavailable"}}`,
		`{"id":"host-history","result":{"data":"invalid"}}`,
		`{"id":"host-history","result":{}}`,
	} {
		a, out := testAgent()
		a.resumeConversation = "saved-thread"
		a.receive([]byte(`{"id":"host-thread","result":{"thread":{"id":"saved-thread"}}}`))
		queuedFrame(t, a)
		a.receive([]byte(response))
		select {
		case err := <-a.startup:
			if err == nil {
				t.Fatal("history failure reported successful startup")
			}
		default:
			t.Fatal("startup did not complete on history failure")
		}
		if strings.Contains(out.String(), `"type":"ready"`) {
			t.Fatal(out.String())
		}
	}
}

func TestCodexResumeEmptyHistoryAndRepeatedCursor(t *testing.T) {
	for _, repeated := range []bool{false, true} {
		a, out := testAgent()
		a.resumeConversation = "saved-thread"
		a.receive([]byte(`{"id":"host-thread","result":{"thread":{"id":"saved-thread"}}}`))
		queuedFrame(t, a)
		if repeated {
			page := []byte(`{"id":"host-history","result":{"data":[],"nextCursor":"same"}}`)
			a.receive(page)
			queuedFrame(t, a)
			a.receive(page)
		} else {
			a.receive([]byte(`{"id":"host-history","result":{"data":[],"nextCursor":null}}`))
		}
		select {
		case err := <-a.startup:
			if (err != nil) != repeated {
				t.Fatalf("repeated=%v startup error=%v", repeated, err)
			}
		default:
			t.Fatal("startup did not finish")
		}
		if strings.Contains(out.String(), `"type":"ready"`) == repeated {
			t.Fatal(out.String())
		}
	}
}
