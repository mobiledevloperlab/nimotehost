// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConflictsSeparateWorktreeAndToolAttribution(t *testing.T) {
	m := newTestManager(t)
	dir := testRepo(t)
	ids := []string{strings.Repeat("a", 64), strings.Repeat("b", 64)}
	for _, id := range ids {
		m.sessions[id] = &managed{session: Session{ID: id, Revision: 1, Kind: "agent", Agent: "codex", State: "running", Started: time.Now().Add(-time.Minute), Directory: dir}, activity: &activityJournal{}}
	}
	warnings := m.Conflicts(context.Background(), nil)
	if len(warnings) != 1 || warnings[0].Kind != "shared_directory" || len(warnings[0].Files) != 0 {
		t.Fatal(warnings)
	}
	for _, id := range ids {
		j := &observedEdits{directory: dir, path: filepath.Join(m.dir, id+".edits")}
		j.record(map[string]any{"kind": "file_change", "status": "pending", "changes": []map[string]string{{"path": "file.txt"}}})
		m.sessions[id].edits = j
	}
	warnings = m.Conflicts(context.Background(), nil)
	if warnings[0].Kind != "shared_directory" {
		t.Fatal(warnings)
	}
	for _, id := range ids {
		m.sessions[id].edits.record(map[string]any{"kind": "file_change", "status": "completed", "changes": []map[string]string{{"path": "file.txt"}}})
	}
	warnings = m.Conflicts(context.Background(), nil)
	if warnings[0].Kind != "observed_tool_overlap" || len(warnings[0].Files) != 1 {
		t.Fatal(warnings)
	}
	tree := filepath.Join(t.TempDir(), "separate")
	gitTest(t, dir, "worktree", "add", "-b", "independent", tree)
	m.sessions[ids[1]].session.Directory = tree
	_ = saveJSON(filepath.Join(m.dir, ids[1]+".edits"), map[string]time.Time{})
	m.sessions[ids[1]].edits = &observedEdits{directory: tree, path: filepath.Join(m.dir, ids[1]+".edits")}
	m.sessions[ids[1]].edits.record(map[string]any{"kind": "file_change", "status": "completed", "changes": []map[string]string{{"path": "file.txt"}}})
	warnings = m.Conflicts(context.Background(), nil)
	if len(warnings) != 1 || warnings[0].Kind != "separate_worktrees" || warnings[0].Severity != "info" || len(warnings[0].Files) != 0 {
		t.Fatal(warnings)
	}
	external := filepath.Join(t.TempDir(), "shared-external.txt")
	for _, id := range ids {
		m.sessions[id].edits.record(map[string]any{"kind": "file_change", "status": "completed", "changes": []map[string]string{{"path": external}}})
	}
	warnings = m.Conflicts(context.Background(), nil)
	if len(warnings) != 1 || warnings[0].Kind != "observed_tool_overlap" || warnings[0].Severity != "warning" || len(warnings[0].Files) != 1 {
		t.Fatal(warnings)
	}
	if len(m.Conflicts(context.Background(), func(id string) bool { return id == ids[0] })) != 0 {
		t.Fatal("scoped request leaked another Agent")
	}
}
func TestWorktreeStartIsIdempotentAndPreservesOriginal(t *testing.T) {
	m := newTestManager(t)
	req := childRequest(t, "output")
	req.Directory = testRepo(t)
	original := req.Directory
	req.RequestID = "worktree-idempotency"
	req.Worktree = &WorktreeRequest{}
	s, e := m.Start(req)
	if e != nil {
		t.Fatal(e)
	}
	awaitSession(t, m, s.ID)
	again, e := m.Start(req)
	if e != nil || again.ID != s.ID || s.Directory == original {
		t.Fatalf("%+v %v", again, e)
	}
	if canonicalDirectory(s.Directory) == canonicalDirectory(original) {
		t.Fatal("worktree not isolated")
	}
	if gitTest(t, original, "status", "--porcelain") != "" {
		t.Fatal("original changed")
	}
	m.Close()
	recovered, e := NewManager(m.dir)
	if e != nil {
		t.Fatal(e)
	}
	defer recovered.Close()
	again, e = recovered.Start(req)
	if e != nil || again.ID != s.ID {
		t.Fatal(again, e)
	}
}
