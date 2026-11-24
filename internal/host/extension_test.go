// SPDX-License-Identifier: Apache-2.0
package host

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommunityPreservesUnregisteredWorkflowState(t *testing.T) {
	m := newTestManager(t)
	dir := m.dir
	taskDir := filepath.Join(dir, "github-tasks")
	worktree := filepath.Join(dir, "worktrees", "retained")
	for _, path := range []string{taskDir, worktree} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{filepath.Join(taskDir, strings.Repeat("a", 64)+".json"): "{\"phase\":\"ready_for_pr\"}", filepath.Join(worktree, "file"): "retained changes"}
	for path, data := range files {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m.Close()
	community, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer community.Close()
	s := &Server{manager: community, token: "owner"}
	caps := s.hostCapabilities()
	if caps.Features["github_issue_tasks_v1"] || caps.Features["github_issue_dispatch_v1"] || !caps.Features["task_verification_v1"] {
		t.Fatal(caps.Features)
	}
	r := httptest.NewRequest(http.MethodGet, "/v1/github-tasks", nil)
	r.Header.Set("Authorization", "Bearer owner")
	r.Header.Set("Nimote-Host-Protocol", "1")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatal(w.Code, w.Body)
	}
	for path, want := range files {
		if data, err := os.ReadFile(path); err != nil || string(data) != want {
			t.Fatal(path, string(data), err)
		}
	}
}

func TestRegisteredWorkflowGuardsIdleShutdown(t *testing.T) {
	m := newTestManager(t)
	runtime := NewWorkflowRuntime(m)
	release := make(chan struct{})
	defer close(release)
	if !runtime.Launch("task", func() { <-release }) || runtime.Launch("task", func() {}) {
		t.Fatal("expected exactly one active worker")
	}
	if err := m.PrepareIdleShutdown(); err == nil {
		t.Fatal("accepted idle shutdown during workflow")
	}
}
