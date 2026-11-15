// SPDX-License-Identifier: Apache-2.0
package host

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxEvidenceBytes = 256 << 10
const maxVerificationDiff = 4 << 20

// Evidence is deliberately bounded. Failure to read any part makes the version
// unknown, so a partial snapshot can never validate a workspace.
type evidenceBuffer struct {
	bytes.Buffer
	limit int
}

func (b *evidenceBuffer) Write(p []byte) (int, error) {
	limit := b.limit
	if limit == 0 {
		limit = maxEvidenceBytes
	}
	if b.Len()+len(p) > limit {
		return 0, errors.New("evidence exceeds retention limit")
	}
	return b.Buffer.Write(p)
}

var evidenceProcessSlots = make(chan struct{}, 4)

func runEvidenceProcess(ctx context.Context, cmd *exec.Cmd, body io.Reader) ([]byte, error) {
	return runEvidenceProcessBounded(ctx, cmd, body, maxEvidenceBytes)
}
func runEvidenceProcessBounded(ctx context.Context, cmd *exec.Cmd, body io.Reader, limit int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case evidenceProcessSlots <- struct{}{}:
		defer func() { <-evidenceProcessSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	b := evidenceBuffer{limit: limit}
	process, input, err := startProtocolProcess(cmd, &b, io.Discard)
	if err != nil {
		return nil, err
	}
	sent := make(chan struct{})
	go func() {
		if body != nil {
			_, _ = io.Copy(input, body)
		}
		_ = input.Close()
		close(sent)
	}()
	stop := context.AfterFunc(ctx, func() { _ = input.Close(); _ = process.Kill() })
	code, err := process.Wait()
	stop()
	_ = input.Close()
	<-sent
	closeErr := process.Close()
	if err != nil || code != 0 || closeErr != nil || ctx.Err() != nil {
		return nil, errors.New("evidence process failed")
	}
	return b.Bytes(), nil
}
func gitEvidence(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return gitEvidenceBounded(ctx, dir, maxEvidenceBytes, args...)
}
func gitEvidenceBounded(ctx context.Context, dir string, limit int, args ...string) ([]byte, error) {
	c := exec.Command("git", append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-C", dir}, args...)...)
	c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	b, err := runEvidenceProcessBounded(ctx, c, nil, limit)
	if err != nil {
		return nil, errors.New("Git evidence unavailable")
	}
	return b, nil
}

type CodeVersion struct {
	Root        string   `json:"root"`
	CommonDir   string   `json:"common_dir"`
	Branch      string   `json:"branch"`
	Head        string   `json:"head"`
	Fingerprint string   `json:"fingerprint"`
	Dirty       bool     `json:"dirty"`
	Files       []string `json:"files"`
	DiffStat    string   `json:"diff_stat"`
	Unknown     string   `json:"unknown,omitempty"`
}

func captureCode(ctx context.Context, dir string) (CodeVersion, string) {
	v := CodeVersion{Files: []string{}}
	get := func(args ...string) string {
		b, e := gitEvidence(ctx, dir, args...)
		if e != nil {
			v.Unknown = e.Error()
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	v.Root = get("rev-parse", "--show-toplevel")
	if v.Root == "" {
		v.Unknown = "not a readable Git worktree"
		return v, ""
	}
	v.Root, _ = filepath.EvalSymlinks(v.Root)
	common := get("rev-parse", "--git-common-dir")
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	v.CommonDir, _ = filepath.EvalSymlinks(common)
	v.Head = get("rev-parse", "--verify", "HEAD")
	v.Branch = get("rev-parse", "--abbrev-ref", "HEAD")
	diff, err := gitEvidenceBounded(ctx, v.Root, maxVerificationDiff, "diff", "--no-ext-diff", "--no-textconv", "--binary", "HEAD", "--")
	if err != nil {
		v.Unknown = err.Error()
	}
	v.DiffStat = get("diff", "--no-ext-diff", "--no-textconv", "--stat", "HEAD", "--")
	names, err := gitEvidence(ctx, v.Root, "diff", "--no-ext-diff", "--name-only", "-z", "HEAD", "--")
	if err != nil {
		v.Unknown = err.Error()
	}
	others, err := gitEvidence(ctx, v.Root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		v.Unknown = err.Error()
	}
	if modes, e := gitEvidence(ctx, v.Root, "ls-files", "--stage", "-z"); e != nil {
		v.Unknown = "Git index unreadable"
	} else {
		for _, entry := range strings.Split(string(modes), "\x00") {
			if strings.HasPrefix(entry, "160000 ") {
				v.Unknown = "submodule contents are not fingerprinted"
				break
			}
		}
	}
	hash := sha256.New()
	fmt.Fprintf(hash, "%s\x00%s\x00", v.Root, v.Head+"/"+v.Branch)
	hash.Write(diff)
	for _, name := range strings.Split(string(names), "\x00") {
		if name != "" {
			v.Files = append(v.Files, name)
		}
	}
	budget := int64(maxEvidenceBytes)
	for _, name := range strings.Split(string(others), "\x00") {
		if name == "" {
			continue
		}
		v.Files = append(v.Files, name)
		fmt.Fprintf(hash, "\x00%s\x00", name)
		path := filepath.Join(v.Root, name)
		info, e := os.Lstat(path)
		if e != nil {
			v.Unknown = "untracked file unreadable"
			continue
		}
		fmt.Fprintf(hash, "mode:%o\x00", info.Mode().Perm())
		if info.Mode()&os.ModeSymlink != 0 {
			target, e := os.Readlink(path)
			if e != nil {
				v.Unknown = "untracked symlink unreadable"
			}
			hash.Write([]byte(target))
			continue
		}
		if !info.Mode().IsRegular() || info.Size() > budget {
			v.Unknown = "untracked files exceed evidence budget"
			continue
		}
		f, e := os.Open(path)
		if e != nil {
			v.Unknown = "untracked file unreadable"
			continue
		}
		n, e := io.Copy(hash, io.LimitReader(f, budget+1))
		f.Close()
		budget -= n
		if e != nil || budget < 0 {
			v.Unknown = "untracked files exceed evidence budget"
		}
	}
	v.Dirty = len(v.Files) > 0
	if v.Unknown == "" {
		v.Fingerprint = hex.EncodeToString(hash.Sum(nil))
	}
	return v, string(diff)
}

type TestCommand struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}
type TestEvidence struct {
	TestCommand
	SessionID  string     `json:"session_id,omitempty"`
	Started    *time.Time `json:"started,omitempty"`
	Finished   *time.Time `json:"finished,omitempty"`
	ExitCode   *int       `json:"exit_code"`
	Status     string     `json:"status"`
	Version    string     `json:"version"`
	Source     string     `json:"source"`
	OutputPath string     `json:"output_path,omitempty"`
}
type AgentStatement struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

type VerificationReport struct {
	AgentStatement   *AgentStatement `json:"agent_statement,omitempty"`
	BaseCommit       string          `json:"base_commit,omitempty"`
	TestedTree       string          `json:"tested_tree,omitempty"`
	RemoteStale      bool            `json:"remote_stale,omitempty"`
	PublishedCommit  string          `json:"published_commit,omitempty"`
	SessionID        string          `json:"session_id"`
	Task             *TaskReference  `json:"task"`
	Workspace        string          `json:"workspace"`
	Goal             string          `json:"goal"`
	Result           string          `json:"result"`
	ResultSource     string          `json:"result_source"`
	Captured         time.Time       `json:"captured"`
	Version          CodeVersion     `json:"version"`
	Source           string          `json:"source"`
	Status           string          `json:"status"`
	Tests            []TestEvidence  `json:"tests"`
	Stale            bool            `json:"stale"`
	PRURL            *string         `json:"pr_url"`
	Unknown          []string        `json:"unknown"`
	DiffSHA          string          `json:"diff_sha"`
	DiffPath         string          `json:"diff_path"`
	OutputPath       string          `json:"output_path"`
	ActivitySequence uint64          `json:"activity_sequence"`
	Diff             string          `json:"-"`
}

// A separate lock avoids holding the lifecycle lock across Git or disk I/O.
func (m *Manager) verificationLock(id string) *sync.Mutex { return &m.verificationMu }
func (m *Manager) CaptureVerification(ctx context.Context, id string) (VerificationReport, error) {
	lock := m.verificationLock(id)
	lock.Lock()
	defer lock.Unlock()
	return m.captureVerification(ctx, id)
}
func (m *Manager) captureVerification(ctx context.Context, id string) (VerificationReport, error) {
	s, err := m.Get(id)
	if err != nil {
		return VerificationReport{}, err
	}
	m.mu.Lock()
	isolated := m.sessions[id] != nil && m.sessions[id].scope != nil
	m.mu.Unlock()
	var v CodeVersion
	var diff string
	if isolated {
		v = CodeVersion{Files: []string{}, Unknown: "Host Git evidence is unavailable for identity-isolated sessions"}
	} else {
		v, diff = captureCode(ctx, s.Directory)
	}
	goal := ""
	if s.Task != nil {
		goal = s.Task.Label
	}
	report := VerificationReport{BaseCommit: s.BaseCommit, SessionID: id, Task: s.Task, Workspace: s.Directory, Goal: goal, Result: s.State + " / " + s.AgentStatus, ResultSource: "host_lifecycle_not_validation", Captured: time.Now().UTC(), Version: v, Source: "host_git_snapshot", Status: "not_run", Tests: []TestEvidence{}, Unknown: []string{"task goal is a client supplied label", "Agent tool commands are not Host test evidence", "PR association has not been confirmed", "snapshot does not attribute shared workspace changes to this Agent"}, DiffPath: "/v1/sessions/" + id + "/verification-diff", OutputPath: "/v1/sessions/" + id + "/output", ActivitySequence: s.ActivitySequence}
	if v.Unknown != "" {
		report.Unknown = append(report.Unknown, v.Unknown)
	}
	if data, e := readPrivateLimit(filepath.Join(m.dir, id+".summary"), 64<<10); e == nil {
		var summary map[string]string
		if decodeJSON(data, &summary) == nil && summary["text"] != "" {
			report.AgentStatement = &AgentStatement{Text: summary["text"], Source: "agent_self_report"}
		}
	}
	// The lifecycle records a base revision at launch. Include both committed
	// implementation changes and current uncommitted changes in the card.
	if s.BaseCommit != "" && !isolated {
		if b, e := gitEvidenceBounded(ctx, s.Directory, maxVerificationDiff, "diff", "--no-ext-diff", "--no-textconv", "--binary", s.BaseCommit, "--"); e == nil {
			diff = string(b)
		} else {
			report.Unknown = append(report.Unknown, "base revision diff unavailable")
		}
		if b, e := gitEvidence(ctx, s.Directory, "diff", "--no-ext-diff", "--stat", s.BaseCommit, "--"); e == nil {
			report.Version.DiffStat = string(b)
		}
		if b, e := gitEvidence(ctx, s.Directory, "diff", "--no-ext-diff", "--name-only", "-z", s.BaseCommit, "--"); e == nil {
			seen := map[string]bool{}
			files := []string{}
			for _, p := range append(strings.Split(string(b), "\x00"), report.Version.Files...) {
				if p != "" && !seen[p] {
					seen[p] = true
					files = append(files, p)
				}
			}
			report.Version.Files = files
		}
	}
	// A second capture detects common concurrent edits while reading evidence.
	var check CodeVersion
	if !isolated {
		check, _ = captureCode(ctx, s.Directory)
	}
	if v.Fingerprint == "" || check.Fingerprint != v.Fingerprint {
		report.Stale = true
	}
	sum := sha256.Sum256([]byte(diff))
	report.DiffSHA = hex.EncodeToString(sum[:])
	if err = atomicWrite(filepath.Join(m.dir, id+".diff"), []byte(diff)); err != nil {
		return report, err
	}
	err = saveJSON(filepath.Join(m.dir, id+".verification"), report)
	return report, err
}
func (m *Manager) Verification(ctx context.Context, id string) (VerificationReport, error) {
	var report VerificationReport
	if !validID(id) {
		return report, os.ErrNotExist
	}
	s, err := m.Get(id)
	if err != nil {
		return report, err
	}
	data, err := readPrivateLimit(filepath.Join(m.dir, id+".verification"), maxEvidenceBytes)
	if errors.Is(err, os.ErrNotExist) {
		report, err = m.CaptureVerification(ctx, id)
	} else if err == nil {
		err = decodeJSON(data, &report)
	}
	if err != nil {
		return report, err
	}
	m.mu.Lock()
	isolated := m.sessions[id] != nil && m.sessions[id].scope != nil
	m.mu.Unlock()
	var current CodeVersion
	if !isolated {
		current, _ = captureCode(ctx, s.Directory)
	}
	report.Stale = report.Stale || report.Version.Fingerprint == "" || current.Fingerprint != report.Version.Fingerprint || s.ActivitySequence != report.ActivitySequence
	if !report.RemoteStale && report.PublishedCommit != "" && report.TestedTree != "" && current.Head == report.PublishedCommit && !current.Dirty && current.Unknown == "" {
		tree, e := gitEvidence(ctx, s.Directory, "rev-parse", "HEAD^{tree}")
		if e == nil && strings.TrimSpace(string(tree)) == report.TestedTree && s.ActivitySequence == report.ActivitySequence {
			report.Stale = false
		}
	}
	if report.Status == "running" { // No worker survives Host restart.
		m.mu.Lock()
		active := m.verifying[id]
		m.mu.Unlock()
		if !active {
			// The worker may have published its final snapshot after our first
			// read and before clearing active. Re-read before declaring a crash.
			if latest, e := readPrivateLimit(filepath.Join(m.dir, id+".verification"), maxEvidenceBytes); e == nil {
				var final VerificationReport
				if decodeJSON(latest, &final) == nil && final.Status != "running" {
					final.Stale = final.Stale || report.Stale
					return final, nil
				}
			}
			report.Status = "interrupted"
			report.Unknown = append(report.Unknown, "Host stopped during verification; inspect test sessions before retrying")
		}
	}
	return report, nil
}
func (m *Manager) StartVerification(ctx context.Context, id string, commands []TestCommand) (VerificationReport, error) {
	lock := m.verificationLock(id)
	lock.Lock()
	defer lock.Unlock()
	s, err := m.Get(id)
	if err != nil {
		return VerificationReport{}, err
	}
	m.mu.Lock()
	isolated := m.sessions[id] != nil && m.sessions[id].scope != nil
	m.mu.Unlock()
	if isolated {
		return VerificationReport{}, errors.New("test execution for identity-isolated sessions requires a separately configured execution scope; unsupported")
	}
	if inboxView(s) != "review" && inboxView(s) != "failed" {
		return VerificationReport{}, errors.New("task must finish its turn before verification")
	}
	if len(commands) == 0 || len(commands) > 8 {
		return VerificationReport{}, errors.New("provide 1–8 test commands")
	}
	for _, cmd := range commands {
		if cmd.Command == "" || len(cmd.Command) > 4096 || len(cmd.Args) > 128 {
			return VerificationReport{}, errors.New("invalid test command")
		}
	}
	m.mu.Lock()
	if m.verifying == nil {
		m.verifying = map[string]bool{}
	}
	if m.verifying[id] {
		m.mu.Unlock()
		return VerificationReport{}, errRequestConflict
	}
	m.verifying[id] = true
	m.mu.Unlock()
	reset := func() { m.mu.Lock(); delete(m.verifying, id); m.mu.Unlock() }
	report, err := m.captureVerification(ctx, id)
	if err != nil {
		reset()
		return report, err
	}
	report.Status = "running"
	for _, cmd := range commands {
		report.Tests = append(report.Tests, TestEvidence{TestCommand: cmd, Status: "not_run", Source: "host_process", Version: report.Version.Fingerprint})
	}
	if err = saveJSON(filepath.Join(m.dir, id+".verification"), report); err != nil {
		reset()
		return report, err
	}
	if !m.runBackground(func() { defer reset(); m.executeVerification(report) }) {
		reset()
		report.Status = "interrupted"
		_ = saveJSON(filepath.Join(m.dir, id+".verification"), report)
		return report, errors.New("Host is shutting down")
	}
	return report, nil
}
func (m *Manager) executeVerification(report VerificationReport) {
	persist := func() { _ = saveJSON(filepath.Join(m.dir, report.SessionID+".verification"), report) }
	for i := range report.Tests {
		t := &report.Tests[i]
		ctx, cancel := context.WithTimeout(m.background, 5*time.Second)
		before, _ := captureCode(ctx, report.Workspace)
		cancel()
		if before.Fingerprint == "" || before.Fingerprint != report.Version.Fingerprint {
			report.Stale = true
			report.Status = "failed"
			persist()
			return
		}
		req := StartRequest{Kind: "task", Command: t.Command, Args: t.Args, Directory: report.Workspace, Task: &TaskReference{ID: "verification/" + report.SessionID, Label: "Verification: " + t.Command}}
		// The durable report records intent before process launch. Interrupted intent
		// is never replayed automatically, even if launch outcome is uncertain.
		t.Status = "starting"
		persist()
		s, err := m.Start(req)
		if err != nil {
			t.Status = "failed"
			report.Status = "failed"
			persist()
			return
		}
		t.SessionID = s.ID
		t.Started = &s.Started
		t.Status = "running"
		t.OutputPath = "/v1/sessions/" + s.ID + "/output"
		persist()
		m.mu.Lock()
		item := m.sessions[s.ID]
		done := item.done
		m.mu.Unlock()
		<-done
		s, err = m.Get(s.ID)
		if err != nil {
			t.Status = "unknown"
			report.Status = "failed"
			persist()
			return
		}
		t.ExitCode = s.ExitCode
		t.Finished = s.Finished
		t.Status = "failed"
		if s.State == "succeeded" && s.ExitCode != nil && *s.ExitCode == 0 {
			t.Status = "passed"
		}
		ctx, cancel = context.WithTimeout(m.background, 5*time.Second)
		after, _ := captureCode(ctx, report.Workspace)
		cancel()
		if after.Fingerprint == "" || after.Fingerprint != report.Version.Fingerprint {
			report.Stale = true
		}
		persist()
	}
	report.Status = "passed"
	for _, t := range report.Tests {
		if t.Status != "passed" {
			report.Status = "failed"
		}
	}
	if report.Stale {
		report.Status = "failed"
	}
	persist()
}

func (m *Manager) autoVerification(id string) {
	lock := m.verificationLock(id)
	lock.Lock()
	defer lock.Unlock()
	m.mu.Lock()
	active := m.verifying[id]
	closed := m.closed
	m.mu.Unlock()
	if active || closed {
		return
	}
	s, err := m.Get(id)
	if err != nil || inboxView(s) == "running" || inboxView(s) == "approval" {
		return
	}
	var existing VerificationReport
	if data, err := readPrivateLimit(filepath.Join(m.dir, id+".verification"), maxEvidenceBytes); err == nil && decodeJSON(data, &existing) == nil && len(existing.Tests) > 0 {
		return
	}
	ctx, cancel := context.WithTimeout(m.background, 5*time.Second)
	defer cancel()
	_, _ = m.captureVerification(ctx, id)
}
