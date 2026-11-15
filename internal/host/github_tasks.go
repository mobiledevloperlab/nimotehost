// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type GitHubTaskRequest struct {
	RequestID       string        `json:"request_id"`
	IssueURL        string        `json:"issue_url,omitempty"`
	Repository      string        `json:"repository,omitempty"`
	IssueNumber     int           `json:"issue_number,omitempty"`
	Directory       string        `json:"directory"`
	Agent           string        `json:"agent"`
	ExecutionPolicy string        `json:"execution_policy,omitempty"`
	DispatchPolicy  string        `json:"dispatch_policy,omitempty"`
	Tests           []TestCommand `json:"tests"`
}
type GitHubTask struct {
	StartAttempt        int               `json:"start_attempt"`
	ID                  string            `json:"id"`
	Request             GitHubTaskRequest `json:"request"`
	Title               string            `json:"title"`
	Body                string            `json:"body"`
	Phase               string            `json:"phase"`
	Updated             time.Time         `json:"updated"`
	SessionID           string            `json:"session_id,omitempty"`
	Workspace           string            `json:"workspace,omitempty"`
	Branch              string            `json:"branch"`
	BaseBranch          string            `json:"base_branch"`
	BaseSHA             string            `json:"base_sha"`
	Attempt             int               `json:"attempt"`
	VerificationAttempt int               `json:"verification_attempt"`
	InitialPromptSent   bool              `json:"initial_prompt_sent"`
	PromptKind          string            `json:"prompt_kind,omitempty"`
	PromptSent          bool              `json:"prompt_sent"`
	CommitIntent        bool              `json:"commit_intent"`
	ExpectedTree        string            `json:"expected_tree,omitempty"`
	CommitSHA           string            `json:"commit_sha,omitempty"`
	PRCreateAttempted   bool              `json:"pr_create_attempted"`
	PRNumber            int               `json:"pr_number,omitempty"`
	PRURL               string            `json:"pr_url,omitempty"`
	FailureStage        string            `json:"failure_stage,omitempty"`
	Error               string            `json:"error,omitempty"`
	Running             bool              `json:"running"`
	ResumeRequired      bool              `json:"resume_required"`
}

const maxGitHubTaskBytes = 1 << 20

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,99}/[A-Za-z0-9_.-]{1,100}$`)

func normalizeIssue(req *GitHubTaskRequest) error {
	if req.IssueURL != "" {
		u, e := url.Parse(req.IssueURL)
		if e != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("expected a github.com HTTPS Issue URL")
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) != 4 || parts[2] != "issues" {
			return errors.New("expected an Issue URL")
		}
		n, e := strconv.Atoi(parts[3])
		if e != nil {
			return errors.New("invalid Issue number")
		}
		repo := parts[0] + "/" + parts[1]
		if req.Repository != "" && !strings.EqualFold(req.Repository, repo) || req.IssueNumber != 0 && req.IssueNumber != n {
			return errors.New("Issue reference conflicts with repository or number")
		}
		req.Repository = repo
		req.IssueNumber = n
	}
	if !repositoryPattern.MatchString(req.Repository) || strings.HasSuffix(req.Repository, "/.") || strings.HasSuffix(req.Repository, "/..") || req.IssueNumber < 1 {
		return errors.New("valid owner/repository and positive Issue number required")
	}
	req.IssueURL = fmt.Sprintf("https://github.com/%s/issues/%d", req.Repository, req.IssueNumber)
	return nil
}

// gh owns credential retrieval. Never request, return, persist or log tokens;
// raw stderr can contain remote URLs or credentials and is discarded.
func githubAPI(ctx context.Context, dir, method, endpoint string, payload any, target any) error {
	args := []string{"api", "--hostname", "github.com", "--method", method, endpoint}
	var body io.Reader
	if payload != nil {
		data, e := json.Marshal(payload)
		if e != nil {
			return e
		}
		body = strings.NewReader(string(data))
		args = append(args, "--input", "-")
	}
	cmd := exec.Command("gh", args...)
	cmd.Dir = dir
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GH_HOST=github.com")
	output, err := runEvidenceProcess(ctx, cmd, body)
	if err != nil {
		return errors.New("GitHub request failed; check local gh authorization and repository access")
	}
	if target != nil {
		if e := json.Unmarshal(output, target); e != nil {
			return errors.New("invalid GitHub response")
		}
	}
	return nil
}
func githubOrigin(ctx context.Context, dir, repo string) error {
	for _, args := range [][]string{{"remote", "get-url", "--all", "origin"}, {"remote", "get-url", "--push", "--all", "origin"}} {
		b, e := gitEvidence(ctx, dir, args...)
		if e != nil {
			return e
		}
		for _, remote := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if e = validateGitHubRemote(remote, repo); e != nil {
				return e
			}
		}
	}
	return nil
}
func validateGitHubRemote(remote, repo string) error {
	s := strings.TrimSpace(remote)

	var name string
	if strings.HasPrefix(s, "git@github.com:") {
		name = strings.TrimPrefix(s, "git@github.com:")
	} else {
		u, e := url.Parse(s)
		if e != nil || !strings.EqualFold(u.Hostname(), "github.com") || u.Scheme != "https" && u.Scheme != "ssh" || u.User != nil && u.Scheme == "https" {
			return errors.New("origin must be a credential-free GitHub HTTPS or SSH remote")
		}
		name = strings.TrimPrefix(u.Path, "/")
	}
	if !strings.EqualFold(strings.TrimSuffix(name, ".git"), repo) {
		return errors.New("local origin does not match the Issue repository")
	}
	return nil
}
func (m *Manager) taskDir() (string, error) {
	p := filepath.Join(m.dir, "github-tasks")
	return p, privateDir(p)
}
func (m *Manager) saveGitHubTask(t *GitHubTask) error {
	p, e := m.taskDir()
	if e != nil {
		return e
	}
	t.Updated = time.Now().UTC()
	t.Running = false
	t.ResumeRequired = false
	data, e := json.Marshal(t)
	if e != nil {
		return e
	}
	if len(data) > maxGitHubTaskBytes {
		return errors.New("task snapshot exceeds retention limit")
	}
	return atomicWrite(filepath.Join(p, t.ID+".json"), data)
}
func (m *Manager) GitHubTask(id string) (GitHubTask, error) {
	var t GitHubTask
	if !validID(id) {
		return t, os.ErrNotExist
	}
	p, e := m.taskDir()
	if e != nil {
		return t, e
	}
	b, e := readPrivateLimit(filepath.Join(p, id+".json"), maxGitHubTaskBytes)
	if e != nil {
		return t, e
	}
	if e = decodeJSON(b, &t); e != nil {
		return t, e
	}
	if t.ID != id {
		return t, errors.New("invalid task record")
	}
	m.mu.Lock()
	t.Running = m.taskRuns[id]
	m.mu.Unlock()
	t.ResumeRequired = !t.Running && t.Phase != "review" && t.Phase != "completed"
	return t, nil
}
func (m *Manager) GitHubTasks(allowed func(string) bool) ([]GitHubTask, error) {
	p, e := m.taskDir()
	if e != nil {
		return nil, e
	}
	entries, e := os.ReadDir(p)
	if e != nil {
		return nil, e
	}
	rows := []GitHubTask{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		t, e := m.GitHubTask(strings.TrimSuffix(entry.Name(), ".json"))
		if e != nil {
			return nil, e
		}
		if allowed == nil || t.SessionID != "" && allowed(t.SessionID) {
			t.Body = ""
			t.Request.Tests = nil
			rows = append(rows, t)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Updated.After(rows[j].Updated) })
	return rows, nil
}
func (m *Manager) CreateGitHubTask(ctx context.Context, req GitHubTaskRequest) (GitHubTask, error) {
	return runOnce(m, req.RequestID, "github-task.create", req, func() (GitHubTask, error) {
		if req.RequestID == "" {
			return GitHubTask{}, errors.New("request_id is required")
		}
		if e := normalizeIssue(&req); e != nil {
			return GitHubTask{}, e
		}
		if !filepath.IsAbs(req.Directory) || !supportsStructuredAgent(req.Agent) {
			return GitHubTask{}, errors.New("absolute repository directory and supported Agent required")
		}
		if req.ExecutionPolicy == "" {
			req.ExecutionPolicy = "confirm"
		}
		if !validExecutionPolicy(req.ExecutionPolicy) {
			return GitHubTask{}, errors.New("invalid execution policy")
		}
		if req.DispatchPolicy == "" {
			req.DispatchPolicy = "manual"
		}
		if req.DispatchPolicy != "manual" && req.DispatchPolicy != "draft_pr_after_verification" {
			return GitHubTask{}, errors.New("invalid dispatch policy")
		}
		if len(req.Tests) == 0 || len(req.Tests) > 8 {
			return GitHubTask{}, errors.New("configure 1–8 verification commands")
		}
		for _, t := range req.Tests {
			if t.Command == "" || len(t.Command) > 4096 || len(t.Args) > 128 {
				return GitHubTask{}, errors.New("invalid test configuration")
			}
		}
		if data, e := json.Marshal(req); e != nil || len(data) > 32<<10 || len(req.Directory) > 4096 {
			return GitHubTask{}, errors.New("execution configuration exceeds retention limit")
		}
		if e := githubOrigin(ctx, req.Directory, req.Repository); e != nil {
			return GitHubTask{}, e
		}
		var issue struct {
			Title       string          `json:"title"`
			Body        string          `json:"body"`
			State       string          `json:"state"`
			PullRequest json.RawMessage `json:"pull_request"`
		}
		endpoint := fmt.Sprintf("repos/%s/issues/%d", req.Repository, req.IssueNumber)
		if e := githubAPI(ctx, req.Directory, "GET", endpoint, nil, &issue); e != nil {
			return GitHubTask{}, e
		}
		if len(issue.PullRequest) > 0 || issue.Title == "" || len(issue.Title) > 512 || len(issue.Body) > 64<<10 {
			return GitHubTask{}, errors.New("Issue unavailable, is a PR, or exceeds context limit")
		}
		var repo struct {
			DefaultBranch string `json:"default_branch"`
		}
		if e := githubAPI(ctx, req.Directory, "GET", "repos/"+req.Repository, nil, &repo); e != nil {
			return GitHubTask{}, e
		}
		if repo.DefaultBranch == "" {
			return GitHubTask{}, errors.New("default branch unavailable")
		}
		// Base the worktree on the fetched default branch, not an unrelated local branch.
		if _, e := gitEvidence(ctx, req.Directory, "fetch", "--no-tags", "origin", "refs/heads/"+repo.DefaultBranch); e != nil {
			return GitHubTask{}, errors.New("cannot fetch Issue repository default branch")
		}
		b, e := gitEvidence(ctx, req.Directory, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
		if e != nil {
			return GitHubTask{}, e
		}
		sum := sha256.Sum256([]byte(req.RequestID))
		id := hex.EncodeToString(sum[:])
		t := GitHubTask{ID: id, Request: req, Title: issue.Title, Body: issue.Body, Phase: "queued", Branch: "nimote/issue-" + strconv.Itoa(req.IssueNumber) + "-" + id[:12], BaseBranch: repo.DefaultBranch, BaseSHA: strings.TrimSpace(string(b))}
		if e = m.saveGitHubTask(&t); e != nil {
			return t, e
		}
		m.launchGitHubTask(id, "continue")
		return t, nil
	})
}

func (m *Manager) launchGitHubTask(id, action string) bool {
	m.mu.Lock()
	if m.closed || m.taskRuns[id] {
		m.mu.Unlock()
		return false
	}
	if m.taskRuns == nil {
		m.taskRuns = map[string]bool{}
	}
	m.taskRuns[id] = true
	m.workers.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.workers.Done()
		defer func() { m.mu.Lock(); delete(m.taskRuns, id); m.mu.Unlock() }()
		t, e := m.GitHubTask(id)
		if e != nil {
			return
		}
		t.Error = ""
		t.FailureStage = ""
		if action == "publish" {
			e = m.publishGitHubTask(&t)
		} else if action == "refresh_pr" {
			e = m.refreshGitHubPR(&t)
		} else {
			e = m.runGitHubTask(&t, action == "verify")
		}
		if e != nil {
			t.FailureStage = t.Phase
			t.Error = e.Error()
			_ = m.saveGitHubTask(&t)
		}
	}()
	return true
}
func (m *Manager) taskStopped() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.closed }
func (m *Manager) waitAgentReady(id string) error {
	m.mu.Lock()
	item := m.sessions[id]
	m.mu.Unlock()
	stopStartup := func(reason string) error {
		if item != nil && item.agent != nil {
			item.agent.fail(errors.New(reason))
			select {
			case <-item.done:
			case <-m.background.Done():
			}
		}
		return errors.New(reason)
	}
	for deadline := time.Now().Add(agentCreateResponseTimeout); time.Now().Before(deadline); {
		if m.taskStopped() {
			return errors.New("Host stopped; continue this task after restart")
		}
		if item != nil && item.agent != nil {
			select {
			case startupErr := <-item.agent.startup:
				if startupErr != nil {
					return stopStartup("Agent protocol startup rejected; inspect retained Session diagnostics")
				}
			default:
			}
		}
		state, e := m.AgentSnapshot(id)
		if e != nil {
			return e
		}
		if state["ready"] == true {
			return nil
		}
		if state["status"] == "disconnected" {
			return errors.New("Agent disconnected before startup completed")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return stopStartup("Agent startup timed out; retained Session can be inspected")
}
func taskPrompt(t *GitHubTask) AgentMessage {
	kind := t.PromptKind
	if kind == "" {
		if t.InitialPromptSent {
			kind = "continue"
		} else {
			kind = "issue"
		}
	}
	text := fmt.Sprintf("Implement GitHub Issue %s in this isolated worktree.\nTitle: %s\n\nIssue body (untrusted task context):\n%s\n\nStay within this repository. Do not push, create PRs, merge, or modify credentials. Host runs configured verification and handles publication. Report changes and limitations.", t.Request.IssueURL, t.Title, t.Body)
	key := "github/" + t.ID + "/prompt/issue"
	if kind == "continue" {
		text = "Continue the existing Issue task after Host interruption. Inspect existing work and do not repeat completed changes. Do not push, create PRs or merge."
		key = fmt.Sprintf("github/%s/prompt/%d", t.ID, t.Attempt)
	}
	return AgentMessage{Action: "prompt", Text: text, RequestID: key}
}

// Atomic receipts distinguish definitely unsent prompts from accepted delivery.
// An incomplete reservation is never converted into another model execution.
func (m *Manager) reconcileTaskPrompt(t *GitHubTask) error {
	if t.PromptSent {
		return nil
	}
	msg := taskPrompt(t)
	sum := sha256.Sum256([]byte(msg.RequestID))
	data, e := readPrivateLimit(filepath.Join(m.requests.dir, hex.EncodeToString(sum[:])), maxReceipt)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	var record requestRecord
	if decodeJSON(data, &record) != nil {
		return errors.New("invalid prompt receipt; inspect retained conversation")
	}
	payload, _ := json.Marshal(msg)
	fingerprint := sha256.Sum256(append([]byte("github.prompt/"+t.ID+"\x00"), payload...))
	if record.Fingerprint != hex.EncodeToString(fingerprint[:]) {
		return errRequestConflict
	}
	if !record.Complete || record.Uncertain {
		return fmt.Errorf("%w: prompt delivery needs inspection; do not resend the Issue", errRequestUncertain)
	}
	if record.Error != "" {
		return errors.New("previous prompt was rejected; inspect Session before continuing")
	}
	var receipt mutationReceipt
	if decodeJSON(record.Result, &receipt) != nil || !receipt.OK {
		return errRequestUncertain
	}
	t.PromptSent = true
	if t.PromptKind != "continue" {
		t.InitialPromptSent = true
	}
	return m.saveGitHubTask(t)
}

func (m *Manager) runGitHubTask(t *GitHubTask, verifyOnly bool) error {
	if t.Phase == "completed" || t.Phase == "review" {
		return nil
	}
	if t.Phase == "ready_for_pr" && !verifyOnly {
		if t.Request.DispatchPolicy == "draft_pr_after_verification" {
			return m.publishGitHubTask(t)
		}
		return nil
	}
	if !verifyOnly && t.Phase != "verifying" && t.Phase != "verification_failed" {
		if t.SessionID != "" && !t.PromptSent {
			if e := m.reconcileTaskPrompt(t); e != nil {
				return e
			}
		}
		if t.SessionID == "" {
			req := StartRequest{Kind: "agent", Agent: t.Request.Agent, Interaction: "structured", Directory: t.Request.Directory, ExecutionPolicy: t.Request.ExecutionPolicy, Task: &TaskReference{ID: t.ID, Label: t.Title}, RequestID: "github/" + t.ID + "/start", Worktree: &WorktreeRequest{Branch: t.Branch, Base: t.BaseSHA}}
			// Failed launches can leave a useful worktree before any Session
			// exists. Reuse that directory only after a definite failed start.
			key := sha256.Sum256([]byte(req.RequestID))
			retained := filepath.Join(m.dir, "worktrees", hex.EncodeToString(key[:]))
			if info, e := os.Stat(retained); e == nil && info.IsDir() {
				t.Workspace = retained
				if e = m.saveGitHubTask(t); e != nil {
					return e
				}
				for _, known := range m.List(0) {
					if known.Task != nil && known.Task.ID == t.ID && known.Kind == "agent" && sameWorkspaceDirectory(canonicalDirectory(known.Directory), canonicalDirectory(retained)) {
						return errors.New("a retained Agent Session exists; inspect its start outcome before retrying")
					}
				}
				sum := sha256.Sum256([]byte(req.RequestID))
				data, e := readPrivateLimit(filepath.Join(m.requests.dir, hex.EncodeToString(sum[:])), maxReceipt)
				var receipt requestRecord
				if e != nil || decodeJSON(data, &receipt) != nil || !receipt.Complete || receipt.Uncertain || receipt.Error == "" {
					return fmt.Errorf("%w: inspect retained worktree before starting another Agent", errRequestUncertain)
				}
				t.StartAttempt++
				req.RequestID = fmt.Sprintf("github/%s/start-retry/%d", t.ID, t.StartAttempt)
				req.Directory = retained
				req.Worktree = nil
				req.baseCommit = t.BaseSHA
				ctx, cancel := context.WithTimeout(m.background, 5*time.Second)
				branch, e := gitEvidence(ctx, retained, "rev-parse", "--abbrev-ref", "HEAD")
				cancel()
				if e != nil || strings.TrimSpace(string(branch)) != t.Branch {
					return errors.New("retained task branch changed; inspect worktree")
				}
				if e = m.saveGitHubTask(t); e != nil {
					return e
				}
			}
			s, e := m.Start(req)
			if e != nil {
				if info, statErr := os.Stat(retained); statErr == nil && info.IsDir() {
					t.Workspace = retained
				}
				return e
			}

			t.SessionID = s.ID
			t.Workspace = s.Directory
			t.Phase = "developing"
			if e = m.saveGitHubTask(t); e != nil {
				return e
			}
		} else {
			s, e := m.Get(t.SessionID)
			if e != nil {
				return e
			}
			if s.State != "running" && s.AgentStatus != "completed" {
				if s.ConversationID == "" && (t.InitialPromptSent || t.PromptSent) {
					return errors.New("no confirmed conversation ID; inspect retained worktree and Session before restarting")
				}
				t.Attempt++
				if t.InitialPromptSent {
					t.PromptKind = "continue"
				} else {
					t.PromptKind = "issue"
				}
				if e = m.saveGitHubTask(t); e != nil {
					return e
				}
				req := StartRequest{Kind: "agent", Agent: t.Request.Agent, Interaction: "structured", Directory: t.Workspace, ExecutionPolicy: t.Request.ExecutionPolicy, Task: &TaskReference{ID: t.ID, Label: t.Title}, RequestID: fmt.Sprintf("github/%s/resume/%d", t.ID, t.Attempt), Resume: &AgentResume{ConversationID: s.ConversationID}, baseCommit: t.BaseSHA}
				if s.ConversationID == "" {
					req.Resume = nil
				}
				s, e = m.Start(req)
				if e != nil {
					return e
				}
				t.SessionID = s.ID
				t.PromptSent = false
				t.Phase = "developing"
				if e = m.saveGitHubTask(t); e != nil {
					return e
				}
			}
		}
		if e := m.waitAgentReady(t.SessionID); e != nil {
			return e
		}
		if !t.PromptSent {
			if t.PromptKind == "" {
				if t.InitialPromptSent {
					t.PromptKind = "continue"
				} else {
					t.PromptKind = "issue"
				}
			}
			msg := taskPrompt(t)
			_, e := runOnce(m, msg.RequestID, "github.prompt/"+t.ID, msg, func() (mutationReceipt, error) {
				e := m.AgentMessage(t.SessionID, msg)
				return mutationReceipt{OK: e == nil, Stage: "accepted", RequestID: msg.RequestID}, e
			})
			if e != nil {
				return e
			}
			t.PromptSent = true
			if t.PromptKind == "issue" {
				t.InitialPromptSent = true
			}
			if e = m.saveGitHubTask(t); e != nil {
				return e
			}
		}
		for {
			if m.taskStopped() {
				return errors.New("Host stopped; task worktree retained")
			}
			state, e := m.AgentSnapshot(t.SessionID)
			if e != nil {
				return e
			}
			status, _ := state["status"].(string)
			if status == "completed" {
				break
			}
			if status == "failed" || status == "disconnected" {
				return errors.New("Agent stopped or failed; inspect Session and continue explicitly")
			}
			next := "developing"
			if status == "needs_approval" {
				next = "needs_approval"
			}
			if next != t.Phase {
				t.Phase = next
				if e = m.saveGitHubTask(t); e != nil {
					return e
				}
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	if t.SessionID == "" {
		return errors.New("no development Session to verify")
	}
	if t.Phase != "verifying" || verifyOnly {
		// Stage the implementation before capturing test evidence. Otherwise a
		// newly created file moves from untracked content into the Git diff at
		// publication time, changing its fingerprint without changing its bytes.
		ctx, cancel := context.WithTimeout(m.background, 5*time.Second)
		_, e := gitEvidence(ctx, t.Workspace, "add", "-A", "--")
		cancel()
		if e != nil {
			return e
		}
		t.VerificationAttempt++
		t.Phase = "verifying"
		if e := m.saveGitHubTask(t); e != nil {
			return e
		}
	}
	key := fmt.Sprintf("github/%s/verify/%d", t.ID, t.VerificationAttempt)
	ctx, cancel := context.WithTimeout(m.background, 5*time.Second)
	_, e := runOnce(m, key, "github.verify/"+t.ID, t.VerificationAttempt, func() (VerificationReport, error) { return m.StartVerification(ctx, t.SessionID, t.Request.Tests) })
	cancel()
	if e != nil {
		return e
	}
	for {
		if m.taskStopped() {
			return errors.New("Host stopped during verification; inspect retained test Sessions")
		}
		ctx, cancel := context.WithTimeout(m.background, 5*time.Second)
		r, e := m.Verification(ctx, t.SessionID)
		cancel()
		if e != nil {
			return e
		}
		if r.Status == "running" {
			time.Sleep(250 * time.Millisecond)
			continue
		}
		if r.Status != "passed" || r.Stale {
			t.Phase = "verification_failed"
			if e = m.saveGitHubTask(t); e != nil {
				return e
			}
			return errors.New("verification did not pass for the current version; inspect evidence and run verification explicitly")
		}
		break
	}
	t.Phase = "ready_for_pr"
	if e = m.saveGitHubTask(t); e != nil {
		return e
	}
	if t.Request.DispatchPolicy == "draft_pr_after_verification" {
		return m.publishGitHubTask(t)
	}
	return nil
}

type githubPull struct {
	Number   int     `json:"number"`
	URL      string  `json:"html_url"`
	State    string  `json:"state"`
	Merged   bool    `json:"merged"`
	MergedAt *string `json:"merged_at"`
	Head     struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

func (m *Manager) publishGitHubTask(t *GitHubTask) error {
	if t.PRURL != "" {
		return m.refreshGitHubPR(t)
	}
	if t.Phase != "ready_for_pr" {
		return errors.New("task must have current passing verification before publication")
	}
	ctx, cancel := context.WithTimeout(m.background, 60*time.Second)
	defer cancel()
	if e := githubOrigin(ctx, t.Workspace, t.Request.Repository); e != nil {
		return e
	}
	r, e := m.Verification(ctx, t.SessionID)
	if e != nil {
		return e
	}
	if r.Status != "passed" {
		return errors.New("passing verification required")
	}
	// A hook or an explicit follow-up commit may change the tree after an
	// earlier publication attempt. Only a fresh, clean Host-verified commit
	// can replace the stored expected tree; never create another commit for it.
	if t.CommitSHA != "" && !r.Stale && !r.Version.Dirty && r.Version.Branch == t.Branch && r.Version.Head != t.BaseSHA {
		tree, readErr := gitEvidence(ctx, t.Workspace, "rev-parse", "HEAD^{tree}")
		if readErr != nil {
			return readErr
		}
		if r.Version.Head != t.CommitSHA || strings.TrimSpace(string(tree)) != t.ExpectedTree {
			t.CommitSHA = r.Version.Head
			t.ExpectedTree = strings.TrimSpace(string(tree))
			if e = m.saveGitHubTask(t); e != nil {
				return e
			}
		}
	}
	marker := "Nimote-Task: " + t.ID
	// Reconcile a commit that succeeded before its receipt was persisted.
	if t.CommitIntent && t.CommitSHA == "" {
		head, e := gitEvidence(ctx, t.Workspace, "rev-parse", "HEAD")
		if e != nil {
			return e
		}
		message, e := gitEvidence(ctx, t.Workspace, "log", "-1", "--format=%B")
		if e != nil {
			return e
		}
		tree, e := gitEvidence(ctx, t.Workspace, "rev-parse", "HEAD^{tree}")
		if e != nil {
			return e
		}
		if strings.Contains(string(message), marker) && strings.TrimSpace(string(tree)) == t.ExpectedTree {
			t.CommitSHA = strings.TrimSpace(string(head))
		} else {
			return errors.New("commit outcome uncertain; inspect retained branch before further writes")
		}
	}
	if t.CommitSHA == "" {
		if r.Status != "passed" || r.Stale || r.Version.Unknown != "" {
			return errors.New("current Host verification required before GitHub writes")
		}
		if r.Version.Branch != t.Branch {
			return errors.New("task branch changed")
		}
		if !r.Version.Dirty {
			if r.Version.Head == t.BaseSHA {
				return errors.New("no implementation changes to publish")
			}
			tree, e := gitEvidence(ctx, t.Workspace, "rev-parse", "HEAD^{tree}")
			if e != nil {
				return e
			}
			t.ExpectedTree = strings.TrimSpace(string(tree))
			t.CommitSHA = r.Version.Head
		} else {
			if _, e = gitEvidence(ctx, t.Workspace, "add", "-A", "--"); e != nil {
				return e
			}
			current, _ := captureCode(ctx, t.Workspace)
			if current.Fingerprint != r.Version.Fingerprint || current.Fingerprint == "" {
				return errors.New("code changed while staging; verify again")
			}
			tree, e := gitEvidence(ctx, t.Workspace, "write-tree")
			if e != nil {
				return e
			}
			t.ExpectedTree = strings.TrimSpace(string(tree))
			t.CommitIntent = true
			if e = m.saveGitHubTask(t); e != nil {
				return e
			}
			message := fmt.Sprintf("Resolve #%d: %s\n\n%s", t.Request.IssueNumber, t.Title, marker)
			if _, e = gitEvidence(ctx, t.Workspace, "commit", "-m", message); e != nil {
				return errors.New("commit failed or uncertain; inspect retained index and branch")
			}
			head, e := gitEvidence(ctx, t.Workspace, "rev-parse", "HEAD")
			if e != nil {
				return e
			}
			t.CommitSHA = strings.TrimSpace(string(head))
		}
		if e = m.saveGitHubTask(t); e != nil {
			return e
		}
	}
	tree, e := gitEvidence(ctx, t.Workspace, "rev-parse", "HEAD^{tree}")
	if e != nil {
		return e
	}
	current, _ := captureCode(ctx, t.Workspace)
	if current.Head != t.CommitSHA || current.Dirty || current.Unknown != "" || strings.TrimSpace(string(tree)) != t.ExpectedTree {
		return errors.New("committed code differs from tested tree; inspect and verify again")
	}
	r.PublishedCommit = t.CommitSHA
	r.TestedTree = t.ExpectedTree
	r.Stale = false
	if e = saveJSON(filepath.Join(m.dir, t.SessionID+".verification"), r); e != nil {
		return e
	}
	if _, e = gitEvidence(ctx, t.Workspace, "push", "origin", t.CommitSHA+":refs/heads/"+t.Branch); e != nil {
		return errors.New("push failed or uncertain; local commit retained, retry reconciles the same SHA")
	}
	owner := strings.Split(t.Request.Repository, "/")[0]
	q := url.Values{"head": {owner + ":" + t.Branch}, "state": {"all"}, "per_page": {"100"}}
	var pulls []githubPull
	if e = githubAPI(ctx, t.Workspace, "GET", "repos/"+t.Request.Repository+"/pulls?"+q.Encode(), nil, &pulls); e != nil {
		return e
	}
	if len(pulls) > 1 {
		return errors.New("multiple PRs found for task branch; inspect manually")
	}
	var pr githubPull
	if len(pulls) == 1 {
		pr = pulls[0]
	} else {
		if t.PRCreateAttempted {
			return errors.New("PR creation outcome uncertain; refresh existing PRs before any further creation")
		}
		t.PRCreateAttempted = true
		if e = m.saveGitHubTask(t); e != nil {
			return e
		}
		payload := map[string]any{"title": t.Title, "head": t.Branch, "base": t.BaseBranch, "draft": true, "body": fmt.Sprintf("Closes %s\n\nHost task: %s\nVerified commit: %s\nTests executed by Host; inspect the local verification card and raw evidence before review.", t.Request.IssueURL, t.ID, t.CommitSHA)}
		if e = githubAPI(ctx, t.Workspace, "POST", "repos/"+t.Request.Repository+"/pulls", payload, &pr); e != nil {
			return e
		}
	}
	if pr.Number < 1 || !strings.HasPrefix(strings.ToLower(pr.URL), "https://github.com/"+strings.ToLower(t.Request.Repository)+"/pull/") {
		return errors.New("unconfirmed PR identity")
	}
	if pr.Head.SHA != t.CommitSHA {
		return errors.New("remote PR head does not match verified commit")
	}
	t.PRNumber = pr.Number
	t.PRURL = pr.URL
	unknown := []string{}
	for _, message := range r.Unknown {
		if message != "PR association has not been confirmed" {
			unknown = append(unknown, message)
		}
	}
	r.Unknown = unknown
	t.Phase = "review"
	r.PRURL = &t.PRURL
	if e = saveJSON(filepath.Join(m.dir, t.SessionID+".verification"), r); e != nil {
		return e
	}
	return m.saveGitHubTask(t)
}
func (m *Manager) refreshGitHubPR(t *GitHubTask) error {
	if t.PRNumber < 1 {
		return errors.New("no confirmed PR; use publish to reconcile task branch")
	}
	ctx, cancel := context.WithTimeout(m.background, 20*time.Second)
	defer cancel()
	var pr githubPull
	if e := githubAPI(ctx, t.Workspace, "GET", fmt.Sprintf("repos/%s/pulls/%d", t.Request.Repository, t.PRNumber), nil, &pr); e != nil {
		return e
	}
	if pr.Head.SHA != t.CommitSHA {
		if data, e := readPrivateLimit(filepath.Join(m.dir, t.SessionID+".verification"), maxEvidenceBytes); e == nil {
			var report VerificationReport
			if decodeJSON(data, &report) == nil {
				report.RemoteStale = true
				report.Stale = true
				report.Unknown = append(report.Unknown, "remote PR head changed after Host verification")
				if e = saveJSON(filepath.Join(m.dir, t.SessionID+".verification"), report); e != nil {
					return e
				}
			}
		}
		return errors.New("PR head changed after Host verification; review new changes on GitHub")
	}
	if pr.Merged || pr.MergedAt != nil {
		t.Phase = "completed"
	} else {
		t.Phase = "review"
	}
	return m.saveGitHubTask(t)
}
func (s *Server) serveGitHubTasks(w http.ResponseWriter, r *http.Request, allowed func(string) bool) {
	if r.URL.Path == "/v1/github-tasks" {
		if r.Method == "GET" {
			rows, e := s.manager.GitHubTasks(allowed)
			if e != nil {
				fail(w, 500, "task storage unavailable")
				return
			}
			start := 0
			if cursor := r.URL.Query().Get("cursor"); cursor != "" {
				if !validID(cursor) {
					fail(w, 400, "invalid task cursor")
					return
				}
				found := false
				for i, t := range rows {
					if t.ID == cursor {
						start = i + 1
						found = true
						break
					}
				}
				if !found {
					fail(w, 400, "task cursor not found; refresh the task list")
					return
				}
			}
			end := min(start+100, len(rows))
			var next *string
			if end < len(rows) {
				id := rows[end-1].ID
				next = &id
			}
			respond(w, 200, map[string]any{"tasks": rows[start:end], "next_cursor": next})
			return
		}
		if r.Method == "POST" {
			var req GitHubTaskRequest
			if !readJSON(w, r, &req) {
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
			defer cancel()
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(65 * time.Second))
			t, e := s.manager.CreateGitHubTask(ctx, req)
			if e != nil {
				mutationFailure(w, e)
				return
			}
			respond(w, 202, t)
			return
		}
		fail(w, 405, "method not allowed")
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/github-tasks/"), "/")
	if len(parts) < 1 || len(parts) > 2 || !validID(parts[0]) {
		fail(w, 404, "unknown task")
		return
	}
	t, e := s.manager.GitHubTask(parts[0])
	if e != nil {
		fail(w, 404, "unknown task")
		return
	}
	if allowed != nil && (t.SessionID == "" || !allowed(t.SessionID)) {
		fail(w, 403, "device session scope denied")
		return
	}
	if len(parts) == 1 && r.Method == "GET" {
		respond(w, 200, t)
		return
	}
	if len(parts) != 2 || r.Method != "POST" || !oneOf(parts[1], "continue", "verify", "publish", "refresh_pr") {
		fail(w, 405, "method not allowed")
		return
	}
	var req struct {
		RequestID string `json:"request_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.RequestID == "" {
		fail(w, 400, "request_id required")
		return
	}
	_, e = runOnce(s.manager, req.RequestID, "github-task."+parts[1]+"/"+t.ID, req, func() (mutationReceipt, error) {
		if !s.manager.launchGitHubTask(t.ID, parts[1]) {
			return mutationReceipt{}, errRequestConflict
		}
		return mutationReceipt{OK: true, RequestID: req.RequestID, Stage: "accepted"}, nil
	})
	if e != nil {
		mutationFailure(w, e)
		return
	}
	respond(w, 202, map[string]bool{"accepted": true})
}
