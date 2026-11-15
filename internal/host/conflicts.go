// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type observedEdits struct {
	mu        sync.Mutex
	path      string
	directory string
	files     map[string]time.Time
}

func (j *observedEdits) record(data any) {
	tool, ok := data.(map[string]any)
	if !ok || tool["kind"] != "file_change" || tool["status"] != "completed" {
		return
	}
	changes, ok := tool["changes"].([]map[string]string)
	if !ok {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.files == nil {
		j.files = map[string]time.Time{}
	}
	for _, change := range changes {
		p := change["path"]
		if p == "" || len(p) > 4096 {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(j.directory, p)
		}
		p = filepath.Clean(p)
		if real, e := filepath.EvalSymlinks(p); e == nil {
			p = real
		}
		if len(j.files) < 256 {
			j.files[p] = time.Now().UTC()
		}
	}
	_ = saveJSON(j.path, j.files)
}

type ConflictWarning struct {
	Sessions    []string `json:"sessions"`
	Agents      []string `json:"agents"`
	Directories []string `json:"directories"`
	Kind        string   `json:"kind"`
	Severity    string   `json:"severity"`
	Files       []string `json:"files"`
	Evidence    string   `json:"evidence"`
	Attribution string   `json:"attribution"`
}
type workspaceIdentity struct {
	directory, root, common string
	edits                   map[string]time.Time
}

func canonicalDirectory(p string) string {
	abs, e := filepath.Abs(p)
	if e != nil {
		return ""
	}
	real, e := filepath.EvalSymlinks(abs)
	if e != nil {
		return ""
	}
	return filepath.Clean(real)
}
func (m *Manager) Conflicts(ctx context.Context, allowed func(string) bool) []ConflictWarning {
	sessions := []Session{}
	identities := []workspaceIdentity{}
	for _, s := range m.List(0) {
		if s.Kind != "agent" || s.State != "running" || allowed != nil && !allowed(s.ID) {
			continue
		}
		w := workspaceIdentity{directory: canonicalDirectory(s.Directory)}
		if w.directory == "" {
			continue
		}
		if b, e := gitEvidence(ctx, w.directory, "rev-parse", "--show-toplevel"); e == nil {
			w.root = canonicalDirectory(strings.TrimSpace(string(b)))
		}
		if b, e := gitEvidence(ctx, w.directory, "rev-parse", "--git-common-dir"); e == nil {
			p := strings.TrimSpace(string(b))
			if !filepath.IsAbs(p) {
				p = filepath.Join(w.directory, p)
			}
			w.common = canonicalDirectory(p)
		}
		if data, e := readPrivateLimit(filepath.Join(m.dir, s.ID+".edits"), maxEvidenceBytes); e == nil {
			_ = json.Unmarshal(data, &w.edits)
		}
		sessions = append(sessions, s)
		identities = append(identities, w)
	}
	result := []ConflictWarning{}
	for i, a := range sessions {
		for j := i + 1; j < len(sessions); j++ {
			b := sessions[j]
			x, y := identities[i], identities[j]
			w := ConflictWarning{Sessions: []string{a.ID, b.ID}, Agents: []string{a.Agent, b.Agent}, Directories: []string{a.Directory, b.Directory}, Files: []string{}, Severity: "warning", Evidence: "host_canonical_directory", Attribution: "shared workspace changes cannot be attributed from Git status alone"}
			for path, at := range x.edits {
				bt, ok := y.edits[path]
				if ok && at.After(b.Started) && bt.After(a.Started) {
					w.Files = append(w.Files, path)
				}
			}
			switch {
			case len(w.Files) > 0:
				sort.Strings(w.Files)
				w.Kind = "observed_tool_overlap"
				w.Evidence = "completed_provider_file_change_events"
				w.Attribution = "Host observed both providers report completed edits; OS write authorship is not independently confirmed"
			case sameWorkspaceDirectory(x.directory, y.directory):
				w.Kind = "shared_directory"
			case sameWorkspaceDirectory(x.root, y.root):
				w.Kind = "shared_worktree"
			case sameWorkspaceDirectory(x.common, y.common):
				w.Kind = "separate_worktrees"
				w.Severity = "info"
				w.Attribution = "same repository, independent working directories"
			default:
				continue
			}
			result = append(result, w)
		}
	}
	return result
}

func sameWorkspaceDirectory(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	left, e := os.Stat(a)
	if e != nil {
		return false
	}
	right, e := os.Stat(b)
	return e == nil && os.SameFile(left, right)
}
