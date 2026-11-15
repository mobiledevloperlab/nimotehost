// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitHubIssueValidationAndReadOnlyDevices(t *testing.T) {
	cases := []string{"http://github.com/a/b/issues/1", "https://example.com/a/b/issues/1", "https://github.com/a/b/pull/1", "https://github.com/a/b/issues/0", "https://user:secret@github.com/a/b/issues/1", "https://github.com/a/b/issues/1?token=secret"}
	for _, link := range cases {
		r := GitHubTaskRequest{IssueURL: link}
		if normalizeIssue(&r) == nil {
			t.Fatalf("accepted %s", link)
		}
	}
	r := GitHubTaskRequest{IssueURL: "https://github.com/example/repo/issues/12"}
	if e := normalizeIssue(&r); e != nil || r.Repository != "example/repo" || r.IssueNumber != 12 {
		t.Fatal(r, e)
	}
	for _, role := range []string{"observer", "session_observer", "session_operator", "session_controller"} {
		d := Device{Role: role}
		if !d.allows("GET", "/v1/github-tasks") || d.allows("POST", "/v1/github-tasks") || d.allows("POST", "/v1/github-tasks/"+strings.Repeat("a", 64)+"/publish") {
			t.Fatal(role)
		}
	}
	m := newTestManager(t)
	id := strings.Repeat("a", 64)
	task := GitHubTask{ID: id, Phase: "ready_for_pr", SessionID: strings.Repeat("b", 64), Workspace: "/retained", FailureStage: "verifying", Error: "test failed"}
	if e := m.saveGitHubTask(&task); e != nil {
		t.Fatal(e)
	}
	filtered, e := m.GitHubTasks(func(string) bool { return false })
	if e != nil || len(filtered) != 0 {
		t.Fatal(filtered, e)
	}
	m.Close()
	recovered, e := NewManager(m.dir)
	if e != nil {
		t.Fatal(e)
	}
	defer recovered.Close()
	got, e := recovered.GitHubTask(id)
	if e != nil || got.Workspace != "/retained" || got.Running || !got.ResumeRequired {
		t.Fatal(got, e)
	}
}
func TestGitHubActionsDedupeBeforeLaunching(t *testing.T) {
	m := newTestManager(t)
	id := strings.Repeat("a", 64)
	task := GitHubTask{ID: id, Phase: "review"}
	if e := m.saveGitHubTask(&task); e != nil {
		t.Fatal(e)
	}
	srv := &Server{manager: m, token: "owner"}
	key := `{"request_id":"repeat-action"}`
	for range 2 {
		r := httptest.NewRequest("POST", "/v1/github-tasks/"+id+"/continue", strings.NewReader(key))
		r.Header.Set("Authorization", "Bearer owner")
		r.Header.Set("Nimote-Host-Protocol", "1")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		if w.Code != 202 {
			t.Fatal(w.Code, w.Body)
		}
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		t, e := m.GitHubTask(id)
		if e != nil {
			break
		}
		if !t.Running {
			break
		}
		time.Sleep(time.Millisecond)
	}
	rows, e := m.GitHubTasks(nil)
	if e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	if _, e = m.CreateGitHubTask(context.Background(), GitHubTaskRequest{}); e == nil {
		t.Fatal("missing request ID accepted")
	}
	if _, e = json.Marshal(rows); e != nil {
		t.Fatal(e)
	}
}

func TestTaskPromptUnknownDeliveryNeverReexecutes(t *testing.T) {
	m := newTestManager(t)
	task := GitHubTask{ID: strings.Repeat("a", 64), Phase: "developing", Request: GitHubTaskRequest{IssueURL: "https://github.com/example/repo/issues/1"}, PromptKind: "issue"}
	if e := m.saveGitHubTask(&task); e != nil {
		t.Fatal(e)
	}
	if e := m.reconcileTaskPrompt(&task); e != nil || task.PromptSent {
		t.Fatal(task, e)
	}
	msg := taskPrompt(&task)
	payload, _ := json.Marshal(msg)
	fingerprint := sha256.Sum256(append([]byte("github.prompt/"+task.ID+"\x00"), payload...))
	key := sha256.Sum256([]byte(msg.RequestID))
	path := filepath.Join(m.requests.dir, hex.EncodeToString(key[:]))
	if e := saveJSON(path, requestRecord{Fingerprint: hex.EncodeToString(fingerprint[:])}); e != nil {
		t.Fatal(e)
	}
	if e := m.reconcileTaskPrompt(&task); !errors.Is(e, errRequestUncertain) || task.PromptSent {
		t.Fatal(task, e)
	}
	receipt, _ := json.Marshal(mutationReceipt{OK: true, Stage: "accepted", RequestID: msg.RequestID})
	if e := saveJSON(path, requestRecord{Fingerprint: hex.EncodeToString(fingerprint[:]), Complete: true, Result: receipt}); e != nil {
		t.Fatal(e)
	}
	if e := m.reconcileTaskPrompt(&task); e != nil || !task.PromptSent || !task.InitialPromptSent {
		t.Fatal(task, e)
	}
}
