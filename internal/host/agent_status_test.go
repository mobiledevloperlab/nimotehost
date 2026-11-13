// SPDX-License-Identifier: Apache-2.0
package host

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentStatusUsesStructuredEvidence(t *testing.T) {
	a := newStructuredAgent("codex", io.Discard, "/workspace")
	check := func(want string) {
		t.Helper()
		if got := a.statusLocked(); got != want {
			t.Fatalf("status %q, want %q", got, want)
		}
	}
	check("unknown")
	a.thread = "thread"
	check("idle")
	a.busy = true
	check("running")
	a.approvals["permission"] = json.RawMessage(`"permission"`)
	check("needs_approval")
	clear(a.approvals)
	a.receiveCodex(codexFrame{Method: "turn/completed", Params: json.RawMessage(`{"turn":{"status":"completed"}}`)})
	check("completed")
	a.receiveCodex(codexFrame{Method: "turn/started", Params: json.RawMessage(`{"turn":{"id":"next"}}`)})
	check("running")
	a.receiveCodex(codexFrame{Method: "turn/completed", Params: json.RawMessage(`{"turn":{"status":"failed"}}`)})
	check("failed")
	a.close()
	check("disconnected")
}

func TestTerminalAgentActivityIsUnknown(t *testing.T) {
	for _, state := range []struct{ process, want string }{{"running", "unknown"}, {"succeeded", "completed"}, {"failed", "failed"}, {"interrupted", "disconnected"}, {"canceled", "unknown"}} {
		if got := sessionAgentStatus(Session{Kind: "agent", State: state.process}, nil); got != state.want {
			t.Fatalf("%s: got %s", state.process, got)
		}
	}
}

func TestACPStatusDistinguishesSuccessFromCancellation(t *testing.T) {
	a := newStructuredAgent("opencode", io.Discard, "/workspace")
	a.thread = "thread"
	for _, status := range []struct{ wire, want string }{{"end_turn", "completed"}, {"cancelled", "idle"}, {"failed", "failed"}, {"future_reason", "unknown"}} {
		a.finishACP(status.wire)
		if got := a.statusLocked(); got != status.want {
			t.Fatalf("%s: %s", status.wire, got)
		}
	}
}

func TestCleanProcessExitDoesNotProveStructuredTurnCompletion(t *testing.T) {
	a := newStructuredAgent("codex", io.Discard, "/workspace")
	record := Session{Kind: "agent", State: "succeeded"}
	if got := sessionAgentStatus(record, a); got != "disconnected" {
		t.Fatalf("unconfirmed turn: %s", got)
	}
	a.lastTurnStatus = "end_turn"
	if got := sessionAgentStatus(record, a); got != "completed" {
		t.Fatalf("completed turn: %s", got)
	}
	a.lastTurnStatus = "failed"
	if got := sessionAgentStatus(record, a); got != "failed" {
		t.Fatalf("failed turn: %s", got)
	}
}

func TestRestoredStructuredStatusRequiresPersistedEvidence(t *testing.T) {
	record := Session{Kind: "agent", Interaction: "structured", State: "succeeded"}
	if got := sessionAgentStatus(record, nil); got != "unknown" {
		t.Fatalf("legacy record fabricated completion: %s", got)
	}
	for _, status := range []string{"completed", "failed", "disconnected"} {
		record.AgentStatus = status
		if got := sessionAgentStatus(record, nil); got != status {
			t.Fatalf("persisted %s became %s", status, got)
		}
	}
	record.AgentStatus = "running"
	if got := sessionAgentStatus(record, nil); got != "unknown" {
		t.Fatal("stopped process restored as running")
	}
}

func TestManagerPersistsFinalStructuredEvidenceAcrossRestart(t *testing.T) {
	for _, sample := range []struct{ last, want string }{{"completed", "completed"}, {"failed", "failed"}, {"", "disconnected"}} {
		m := newTestManager(t)
		id := strings.Repeat("a", 64)
		f, err := os.Create(filepath.Join(m.dir, id+".ring"))
		if err != nil {
			t.Fatal(err)
		}
		a := newStructuredAgent("codex", io.Discard, m.dir)
		a.lastTurnStatus = sample.last
		a.busy = sample.last == ""
		item := &managed{session: Session{ID: id, Kind: "agent", Interaction: "structured", State: "running"},
			agent: a, output: &outputWriter{file: f}, process: receiptProcess{}, done: make(chan struct{})}
		m.sessions[id] = item
		m.wait(item)
		m.Close()
		recovered, err := NewManager(m.dir)
		if err != nil {
			t.Fatal(err)
		}
		record, err := recovered.Get(id)
		if err != nil || record.AgentStatus != sample.want {
			t.Fatalf("%q: %+v %v", sample.last, record, err)
		}
		recovered.Close()
	}
}
