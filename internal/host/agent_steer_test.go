// SPDX-License-Identifier: Apache-2.0
package host

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestCodexSteerTargetsActiveTurnAndDeduplicates(t *testing.T) {
	a, out := testAgent()
	a.thread, a.turn, a.busy = "thread", "turn", true
	a.publishControls()
	if !a.controls.Steer {
		t.Fatal("steering not advertised")
	}
	req := AgentMessage{Action: "steer", RequestID: "followup", Text: "Also check B", TurnID: "turn"}
	if err := a.message(req); err != nil {
		t.Fatal(err)
	}
	f := queuedFrame(t, a)
	p := f["params"].(map[string]any)
	if f["method"] != "turn/steer" || p["threadId"] != "thread" || p["expectedTurnId"] != "turn" || p["input"].([]any)[0].(map[string]any)["text"] != req.Text {
		t.Fatal(f)
	}
	if err := a.message(req); err != nil || len(a.rpc.queue) != 0 {
		t.Fatalf("duplicate dispatched: %v", err)
	}
	if strings.Contains(out.String(), "user_followup") {
		t.Fatal("unconfirmed message displayed as accepted")
	}
	a.receive([]byte(fmt.Sprintf(`{"id":%q,"result":{"turnId":"turn"}}`, f["id"])))
	if !a.busy || a.turn != "turn" || !strings.Contains(out.String(), `"type":"user_followup"`) || len(a.steers) != 0 {
		t.Fatalf("invalid acknowledgement: %s", out)
	}
}

func TestCodexSteerRejectionAndLateAcknowledgementPreserveState(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		t.Run(fmt.Sprint(rejected), func(t *testing.T) {
			a, out := testAgent()
			a.thread, a.turn, a.busy = "thread", "turn", true
			if err := a.message(AgentMessage{Action: "steer", RequestID: "followup", Text: "B", TurnID: "turn"}); err != nil {
				t.Fatal(err)
			}
			f := queuedFrame(t, a)
			a.receive([]byte(`{"method":"turn/completed","params":{"turn":{"id":"turn","status":"completed"}}}`))
			// A reply to the old steer must not affect a newer turn either.
			a.turn, a.busy = "new-turn", true
			response := map[string]any{"id": f["id"], "result": map[string]string{"turnId": "turn"}}
			kind := "user_followup"
			if rejected {
				response = map[string]any{"id": f["id"], "error": map[string]any{"code": -1, "message": "turn ended"}}
				kind = "steer_rejected"
			}
			b, _ := json.Marshal(response)
			a.receive(b)
			if !a.busy || a.turn != "new-turn" || a.lastTurnStatus != "completed" || !strings.Contains(out.String(), `"type":"`+kind+`"`) {
				t.Fatalf("steering reply changed active turn: %s", out)
			}
		})
	}
}

func TestSteerRejectsUnavailableTurnsAndACP(t *testing.T) {
	for _, state := range []string{"idle", "starting", "stale", "empty", "configuring", "compacting", "closed", "blank", "oversized", "acp", "full"} {
		t.Run(state, func(t *testing.T) {
			a, _ := testAgent()
			a.thread, a.turn, a.busy = "thread", "turn", true
			req := AgentMessage{Action: "steer", RequestID: "followup", Text: "B", TurnID: "turn"}
			switch state {
			case "idle":
				a.busy = false
			case "starting":
				a.turn = ""
			case "stale":
				req.TurnID = "old"
			case "empty":
				req.TurnID = ""
			case "configuring":
				a.configuring = true
			case "compacting":
				a.compacting = true
			case "closed":
				a.close()
			case "blank":
				req.Text = "  "
			case "oversized":
				req.Text = strings.Repeat("x", 256<<10+1)
			case "acp":
				a, _ = testACP()
				a.thread, a.turn, a.busy = "thread", "turn", true
			case "full":
				for i := 0; i < cap(a.rpc.queue); i++ {
					_ = a.send(map[string]any{"method": "test"})
				}
			}
			if err := a.message(req); err == nil || a.seen[req.RequestID] || len(a.steers) != 0 {
				t.Fatalf("invalid steering accepted or mutated state: %v", err)
			}
		})
	}
}

func TestCodexSteerCloseReportsUnconfirmedMessages(t *testing.T) {
	a, out := testAgent()
	a.thread, a.turn, a.busy = "thread", "turn", true
	if err := a.message(AgentMessage{Action: "steer", RequestID: "followup", Text: "B", TurnID: "turn"}); err != nil {
		t.Fatal(err)
	}
	a.close()
	if len(a.steers) != 0 || !strings.Contains(out.String(), `"type":"steer_rejected"`) {
		t.Fatal(out.String())
	}
}

func TestCodexSteerPendingLimit(t *testing.T) {
	a, _ := testAgent()
	a.thread, a.turn, a.busy = "thread", "turn", true
	for i := range 16 {
		if err := a.message(AgentMessage{Action: "steer", RequestID: fmt.Sprint(i), Text: "B", TurnID: "turn"}); err != nil {
			t.Fatal(err)
		}
		_ = queuedFrame(t, a)
	}
	if err := a.message(AgentMessage{Action: "steer", RequestID: "overflow", Text: "B", TurnID: "turn"}); err == nil || a.seen["overflow"] {
		t.Fatal("pending steering limit not enforced")
	}
}

func TestCodexSteerHTTPReceiptAndSnapshot(t *testing.T) {
	m := newTestManager(t)
	f, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	output := &outputWriter{file: f}
	defer output.Close()
	a := newStructuredAgent("codex", output, t.TempDir())
	a.input = discardAgentInput{}
	a.thread, a.turn, a.busy = "thread", "turn", true
	id := strings.Repeat("b", 64)
	done := make(chan struct{})
	close(done)
	m.sessions[id] = &managed{session: Session{ID: id, Kind: "agent", Interaction: "structured", State: "running"}, agent: a, process: receiptProcess{}, output: output, done: done}
	server := &Server{manager: m, token: strings.Repeat("a", 64)}
	path := "/v1/sessions/" + id
	snapshot := request(t, server, "GET", path+"/agent-state", "", server.token, "1")
	if snapshot.Code != 200 || !strings.Contains(snapshot.Body.String(), `"can_steer":true`) || !strings.Contains(snapshot.Body.String(), `"turn_id":"turn"`) {
		t.Fatal(snapshot.Body.String())
	}
	body := `{"action":"steer","text":"Also check B","turn_id":"turn","request_id":"followup"}`
	for range 2 {
		response := request(t, server, "POST", path+"/agent-message", body, server.token, "1")
		if response.Code != 200 {
			t.Fatalf("%d: %s", response.Code, response.Body)
		}
	}
	_ = queuedFrame(t, a)
	if len(a.rpc.queue) != 0 {
		t.Fatal("HTTP retry dispatched twice")
	}
	conflict := request(t, server, "POST", path+"/agent-message", strings.Replace(body, "Also check B", "different message", 1), server.token, "1")
	if conflict.Code != 409 {
		t.Fatalf("conflicting retry accepted: %d", conflict.Code)
	}
	a.receive([]byte(`{"method":"turn/completed","params":{"turn":{"id":"turn","status":"completed"}}}`))
	stale := request(t, server, "POST", path+"/agent-message", strings.Replace(body, "followup", "new-request", 1), server.token, "1")
	if stale.Code != 400 {
		t.Fatalf("stale steering accepted: %d %s", stale.Code, stale.Body)
	}
}
