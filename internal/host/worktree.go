// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

type WorktreeRequest struct {
	Branch string `json:"branch,omitempty"`
	Base   string `json:"base,omitempty"`
}

func (m *Manager) prepareWorktree(req StartRequest) (StartRequest, error) {
	if req.Worktree == nil {
		return req, nil
	}
	if req.RequestID == "" || req.Identity != nil || req.Resume != nil {
		return req, errors.New("worktree creation requires request_id and cannot change a resumed conversation or execution identity")
	}
	check := req
	if check.Interaction == "auto" {
		check.Interaction = "structured"
	}
	if err := resolveCommand(&check); err != nil {
		return req, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sum := sha256.Sum256([]byte(req.RequestID))
	key := hex.EncodeToString(sum[:])
	branch := req.Worktree.Branch
	if branch == "" {
		branch = "nimote/task-" + key[:16]
	}
	if strings.HasPrefix(branch, "-") || strings.ContainsAny(branch, "\x00\r\n") {
		return req, errors.New("invalid branch")
	}
	if _, e := gitEvidence(ctx, req.Directory, "check-ref-format", "--branch", branch); e != nil {
		return req, errors.New("invalid branch")
	}
	base := req.Worktree.Base
	if base == "" {
		base = "HEAD"
	}
	if strings.HasPrefix(base, "-") {
		return req, errors.New("invalid base revision")
	}
	b, e := gitEvidence(ctx, req.Directory, "rev-parse", "--verify", base+"^{commit}")
	if e != nil {
		return req, e
	}
	sha := strings.TrimSpace(string(b))
	dir := filepath.Join(m.dir, "worktrees")
	if e = privateDir(dir); e != nil {
		return req, e
	}
	path := filepath.Join(dir, key)
	if _, e = gitEvidence(ctx, req.Directory, "worktree", "add", "-b", branch, path, sha); e != nil {
		return req, errors.New("cannot create worktree; inspect branch and retained worktrees before retrying")
	}
	req.sourceDirectory = req.Directory
	req.baseCommit = sha
	req.Directory = path
	return req, nil
}
