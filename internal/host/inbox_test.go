// SPDX-License-Identifier: Apache-2.0
package host

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInboxPersistenceAndRevision(t *testing.T) {
	m := newTestManager(t)
	req := childRequest(t, "fail")
	req.Task = &TaskReference{ID: "inbox-task", Label: "Example"}
	s, err := m.Start(req)
	if err != nil {
		t.Fatal(err)
	}
	s = awaitSession(t, m, s.ID)
	page, err := m.Inbox(nil)
	if err != nil || page.Counts["failed"] != 1 {
		t.Fatalf("%+v %v", page, err)
	}
	rev := page.Items[0].Revision
	if err = m.ReviewInbox(s.ID, rev, "handled"); err != nil {
		t.Fatal(err)
	}
	m.Close()
	recovered, err := NewManager(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	page, err = recovered.Inbox(nil)
	if err != nil || page.Counts["handled"] != 1 {
		t.Fatalf("%+v %v", page, err)
	}
	if err = recovered.ReviewInbox(s.ID, rev, "archived"); err != nil {
		t.Fatal(err)
	}
	page, _ = recovered.Inbox(nil)
	if !page.Items[0].Archived || page.Counts["archived"] != 1 {
		t.Fatal(page)
	}
	recovered.mu.Lock()
	recovered.sessions[s.ID].session.Revision++
	recovered.mu.Unlock()
	page, _ = recovered.Inbox(nil)
	if page.Items[0].Archived || page.Counts["failed"] != 1 {
		t.Fatal(page)
	}
	if err = recovered.ReviewInbox(s.ID, rev, "handled"); !errors.Is(err, errRequestConflict) {
		t.Fatal(err)
	}
	filtered, _ := recovered.Inbox(func(string) bool { return false })
	if len(filtered.Items) != 0 || filtered.Counts["failed"] != 0 {
		t.Fatal(filtered)
	}
}
func TestInboxCannotHideActiveApproval(t *testing.T) {
	m := newTestManager(t)
	id := strings.Repeat("a", 64)
	m.sessions[id] = &managed{activity: &activityJournal{path: filepath.Join(m.dir, id+".activity")}, session: Session{ID: id, Kind: "agent", State: "running", Agent: "codex", Started: time.Now(), Revision: 1}, agent: &structuredAgent{approvals: map[string]json.RawMessage{"permission": nil}}}
	s, _ := m.Get(id)
	if err := m.ReviewInbox(id, inboxRevision(s), "archived"); err == nil {
		t.Fatal("approval was hidden")
	}
	m.sessions[id].agent = nil
	m.sessions[id].session.State = "interrupted"
	page, _ := m.Inbox(nil)
	if page.Counts["approval"] != 0 || page.Counts["failed"] != 1 {
		t.Fatal(page)
	}
}
func TestInboxACL(t *testing.T) {
	id := strings.Repeat("a", 64)
	other := strings.Repeat("b", 64)
	for _, role := range []string{"session_observer", "session_operator", "session_controller"} {
		d := Device{Role: role, Sessions: []string{id}}
		if !d.allows("GET", "/v1/inbox") || d.allows("POST", "/v1/inbox/"+other) {
			t.Fatal(role)
		}
		if d.allows("POST", "/v1/inbox/"+id) != (role != "session_observer") {
			t.Fatal(role)
		}
	}
	if (Device{Role: "observer"}).allows("POST", "/v1/inbox/"+id) {
		t.Fatal("observer mutated")
	}
	m := newTestManager(t)
	srv := &Server{manager: m, token: "owner"}
	r := httptest.NewRequest(http.MethodGet, "/v1/inbox?view=bogus", nil)
	r.Header.Set("Authorization", "Bearer owner")
	r.Header.Set("Nimote-Host-Protocol", "1")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body)
	}
}

func TestInboxHandledCompletedAgentTurnSurvivesHostRestart(t *testing.T) {
	m := newTestManager(t)
	id := strings.Repeat("c", 64)
	activity := &activityJournal{path: filepath.Join(m.dir, id+".activity")}
	if e := activity.append("session_started", "running", "", 0); e != nil {
		t.Fatal(e)
	}
	if e := activity.append("turn_completed", "completed", "done-turn", 0); e != nil {
		t.Fatal(e)
	}
	record := Session{ID: id, Kind: "agent", Agent: "codex", Interaction: "structured", State: "running", AgentStatus: "completed", Revision: 1, Started: time.Now()}
	m.sessions[id] = &managed{session: record, activity: activity, agent: &structuredAgent{thread: "conversation", lastTurnStatus: "completed"}}
	if e := saveJSON(filepath.Join(m.dir, id+".json"), record); e != nil {
		t.Fatal(e)
	}
	page, e := m.Inbox(nil)
	if e != nil || page.Counts["review"] != 1 {
		t.Fatal(page, e)
	}
	if e = m.ReviewInbox(id, page.Items[0].Revision, "handled"); e != nil {
		t.Fatal(e)
	}
	m.Close()
	recovered, e := NewManager(m.dir)
	if e != nil {
		t.Fatal(e)
	}
	defer recovered.Close()
	page, e = recovered.Inbox(nil)
	if e != nil || page.Counts["handled"] != 1 || page.Items[0].Session.State != "interrupted" {
		t.Fatal(page, e)
	}
	item := recovered.sessions[id]
	if e = item.activity.append("prompt_accepted", "running", "next-turn", 0); e != nil {
		t.Fatal(e)
	}
	page, e = recovered.Inbox(nil)
	if e != nil || page.Counts["handled"] != 0 {
		t.Fatal("a new turn remained hidden", page, e)
	}
}
