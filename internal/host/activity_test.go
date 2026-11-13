// SPDX-License-Identifier: Apache-2.0
package host

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestActivityTaskReferencePersistsWithoutOutputCopy(t *testing.T) {
	m := newTestManager(t)
	req := childRequest(t, "output")
	req.Task = &TaskReference{ID: "build-task", Label: "Fix build"}
	req.RequestID = "task-linked-create"
	started, err := m.Start(req)
	if err != nil {
		t.Fatal(err)
	}
	started.Task.ID = "mutated"
	finished := awaitSession(t, m, started.ID)
	if finished.Task == nil || finished.Task.ID != "build-task" {
		t.Fatal("task reference is shared")
	}
	page, err := m.Activity(started.ID, 0)
	if err != nil || len(page.Events) != 2 || page.Events[0].Type != "session_started" || page.Events[1].Type != "session_finished" {
		t.Fatalf("%+v %v", page, err)
	}
	if page.Events[1].OutputOffset != finished.OutputBytes {
		t.Fatal("output reference was lost")
	}
	if _, err := m.Start(req); err != nil {
		t.Fatal(err)
	}
	replay, _ := m.Activity(started.ID, 0)
	if replay.Cursor != page.Cursor {
		t.Fatal("create retry duplicated history")
	}
	m.Close()
	recovered, err := NewManager(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	current, _ := recovered.Get(started.ID)
	if current.Task == nil || current.Task.ID != "build-task" {
		t.Fatal("task reference was not persisted")
	}
	page, err = recovered.Activity(started.ID, page.Cursor)
	if err != nil || len(page.Events) != 0 {
		t.Fatalf("%+v %v", page, err)
	}
	if err := recovered.Delete(started.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.dir, started.ID+".activity")); !os.IsNotExist(err) {
		t.Fatal("deleted history retained")
	}
}

func TestActivityRetentionAndConcurrentSequence(t *testing.T) {
	j := &activityJournal{path: filepath.Join(t.TempDir(), "events")}
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Go(func() {
			for n := 0; n < 70; n++ {
				if err := j.append("turn_started", "running", "", 0); err != nil {
					t.Error(err)
				}
			}
		})
	}
	workers.Wait()
	p, err := j.page(0)
	if err != nil || len(p.Events) != 256 || p.Cursor != 280 || !p.ResyncRequired {
		t.Fatalf("%+v %v", p, err)
	}
	for i := 1; i < len(p.Events); i++ {
		if p.Events[i].Sequence != p.Events[i-1].Sequence+1 {
			t.Fatal("sequence gap")
		}
	}
	recovered, err := loadActivity(j.path)
	if err != nil {
		t.Fatal(err)
	}
	p, _ = recovered.page(279)
	if p.ResyncRequired || len(p.Events) != 1 || p.Events[0].Sequence != 280 {
		t.Fatal("reconnect cursor failed")
	}
	p, _ = recovered.page(281)
	if !p.ResyncRequired {
		t.Fatal("future cursor accepted")
	}
}

func TestStructuredActivityStoresOnlyLifecycleReferences(t *testing.T) {
	j := &activityJournal{path: filepath.Join(t.TempDir(), "events")}
	item := &managed{activity: j}
	a := newStructuredAgent("codex", io.Discard, "/workspace")
	a.onActivity = item.agentActivity
	a.emit("user", "private prompt body")
	a.emit("text_delta", map[string]string{"text": "private output"})
	a.emit("tool", map[string]string{"output": "private tool output"})
	a.emit("approval", map[string]any{"id": "approval-1", "command": "private command"})
	a.onActivity("approval_decline_requested", map[string]string{"id": "approval-1"})
	a.emit("approval_resolved", "approval-1")
	a.emit("turn_completed", map[string]string{"status": "end_turn", "id": "turn-1"})
	p, err := j.page(0)
	if err != nil || len(p.Events) != 5 {
		t.Fatalf("%+v %v", p, err)
	}
	data, _ := os.ReadFile(j.path)
	if strings.Contains(string(data), "private") {
		t.Fatal("journal duplicated content")
	}
	if p.Events[2].Type != "approval_decline_requested" || p.Events[2].State != "" || p.Events[4].State != "completed" {
		t.Fatalf("%+v", p)
	}
}

func TestActivityEndpointAuthenticationAndHistoryFailure(t *testing.T) {
	m := newTestManager(t)
	started, err := m.Start(childRequest(t, "output"))
	if err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, started.ID)
	server := &Server{manager: m, token: strings.Repeat("a", 64)}
	path := "/v1/sessions/" + started.ID + "/activity"
	for _, sample := range []struct {
		token, query string
		status       int
	}{{"wrong", "", 401}, {server.token, "?after=-1", 400}, {server.token, "?after=0", 200}} {
		response := request(t, server, "GET", path+sample.query, "", sample.token, "1")
		if response.Code != sample.status {
			t.Fatalf("%d %s", response.Code, response.Body)
		}
	}
	j := m.sessions[started.ID].activity
	if err := os.Remove(j.path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(j.path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := j.append("ready", "idle", "", 0); err == nil {
		t.Fatal("expected storage failure")
	}
	response := request(t, server, "GET", path, "", server.token, "1")
	if response.Code != 500 {
		t.Fatal("history failure hidden")
	}
}

func TestCorruptActivityDoesNotDisableExistingHostSessions(t *testing.T) {
	m := newTestManager(t)
	s, err := m.Start(childRequest(t, "output"))
	if err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, s.ID)
	m.Close()
	if err := os.WriteFile(filepath.Join(m.dir, s.ID+".activity"), []byte("invalid journal"), 0600); err != nil {
		t.Fatal(err)
	}
	recovered, err := NewManager(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if _, err := recovered.Get(s.ID); err != nil {
		t.Fatal("journal corruption hid the session")
	}
	if _, err := recovered.Activity(s.ID, 0); err == nil {
		t.Fatal("corruption reported as complete history")
	}
}

func TestTaskReferenceValidationAndCrashBoundary(t *testing.T) {
	m := newTestManager(t)
	req := childRequest(t, "output")
	req.Task = &TaskReference{ID: strings.Repeat("x", 513)}
	if _, err := m.Start(req); err == nil {
		t.Fatal("invalid task reference accepted")
	}
	req.Task = &TaskReference{ID: "task"}
	s, err := m.Start(req)
	if err != nil {
		t.Fatal(err)
	}
	s = awaitSession(t, m, s.ID)
	m.Close()
	s.State = "running"
	s.Finished = nil
	if err := saveJSON(filepath.Join(m.dir, s.ID+".json"), s); err != nil {
		t.Fatal(err)
	}
	recovered, err := NewManager(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	p, err := recovered.Activity(s.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	last := p.Events[len(p.Events)-1]
	if last.Type != "session_interrupted" || last.State != "disconnected" {
		t.Fatalf("%+v", last)
	}
	encoded, _ := json.Marshal(last)
	if len(encoded) > 512 {
		t.Fatal("unbounded activity")
	}
}
