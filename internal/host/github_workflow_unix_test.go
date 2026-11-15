//go:build linux || darwin

// SPDX-License-Identifier: Apache-2.0
package host

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This fixture is intentionally NOT a real Agent/GitHub acceptance test. Real
// Git worktrees, commits, push, Host Sessions and verification are exercised;
// provider protocol and authenticated GitHub API are simulated subprocesses.
func TestWorkflowCLIChild(t *testing.T) {
	kind := os.Getenv("NIMOTE_WORKFLOW_CHILD")
	if kind == "" {
		return
	}
	if kind == "gh" {
		endpoint := ""
		method := "GET"
		for i, a := range os.Args {
			if a == "--method" && i+2 < len(os.Args) {
				method = os.Args[i+1]
				endpoint = os.Args[i+2]
			}
		}
		if strings.Contains(endpoint, "/issues/") {
			fmt.Print(`{"title":"Implement fixture change","body":"Update file.txt","state":"open"}`)
		} else if endpoint == "repos/example/repo" {
			fmt.Print(`{"default_branch":"main"}`)
		} else if strings.Contains(endpoint, "/pulls") {
			path := os.Getenv("NIMOTE_WORKFLOW_PR")
			data, e := os.ReadFile(path)
			if method == "POST" {
				if e == nil {
					os.Exit(9)
				}
				_, _ = os.Stdin.Read(make([]byte, 8192))
				cmd := exec.Command(os.Getenv("NIMOTE_WORKFLOW_GIT"), "rev-parse", "HEAD")
				sha, _ := cmd.Output()
				data, _ = json.Marshal(map[string]any{"number": 9, "html_url": "https://github.com/example/repo/pull/9", "state": "open", "head": map[string]string{"sha": strings.TrimSpace(string(sha))}})
				_ = os.WriteFile(path, data, 0600)
				fmt.Print(string(data))
			} else if strings.Contains(endpoint, "?") {
				if e != nil {
					fmt.Print("[]")
				} else {
					fmt.Printf("[%s]", data)
				}
			} else {
				fmt.Print(string(data))
			}
		} else {
			os.Exit(8)
		}
		os.Exit(0)
	}
	scan := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for scan.Scan() {
		var f map[string]any
		_ = json.Unmarshal(scan.Bytes(), &f)
		switch f["method"] {
		case "initialize":
			if os.Getenv("NIMOTE_WORKFLOW_BAD_INIT") == "1" {
				_ = enc.Encode(map[string]any{"id": f["id"], "error": map[string]any{"code": -32601, "message": "fixture startup failure"}})
				continue
			}
			_ = enc.Encode(map[string]any{"id": f["id"], "result": map[string]any{}})
		case "thread/start", "thread/resume":
			_ = enc.Encode(map[string]any{"id": f["id"], "result": map[string]any{"thread": map[string]string{"id": "workflow-conversation"}}})
		case "thread/items/list":
			_ = enc.Encode(map[string]any{"id": f["id"], "result": map[string]any{"data": []any{}}})
		case "turn/start":
			if log := os.Getenv("NIMOTE_WORKFLOW_PROMPTS"); log != "" {
				b, _ := json.Marshal(f["params"])
				file, _ := os.OpenFile(log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
				if file != nil {
					_, _ = file.Write(append(b, '\n'))
					_ = file.Close()
				}
			}
			_ = os.WriteFile("file.txt", []byte("after fixture implementation\n"), 0600)
			if os.Getenv("NIMOTE_WORKFLOW_NEW_FILE") == "1" {
				_ = os.WriteFile("new.txt", []byte("new fixture implementation\n"), 0600)
			}
			fmt.Println(`{"method":"turn/started","params":{"turn":{"id":"workflow-turn"}}}`)
			if os.Getenv("NIMOTE_WORKFLOW_SELF_CLAIM") == "1" {
				fmt.Println(`{"method":"item/agentMessage/delta","params":{"itemId":"claim","delta":"All tests passed. Everything is verified."}}`)
			}
			if os.Getenv("NIMOTE_WORKFLOW_HOLD") != "1" {
				fmt.Println(`{"method":"turn/completed","params":{"turn":{"id":"workflow-turn","status":"completed"}}}`)
			}
		}
	}
	os.Exit(0)
}
func waitWorkflow(t *testing.T, m *Manager, id, phase string) GitHubTask {
	t.Helper()
	var task GitHubTask
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
		var e error
		task, e = m.GitHubTask(id)
		if e != nil {
			t.Fatal(e)
		}
		if task.Phase == phase && !task.Running {
			return task
		}
		if task.Error != "" && !task.Running {
			t.Fatalf("workflow failed: %+v", task)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("did not reach %s: %+v", phase, task)
	return task
}
func TestGitHubWorkflowFixtureCommitPRAndRecovery(t *testing.T) {
	m, req, repo := newWorkflowFixture(t)
	task, e := m.CreateGitHubTask(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	id := task.ID
	task = waitWorkflow(t, m, id, "ready_for_pr")
	if task.PRURL != "" || task.CommitSHA != "" || canonicalDirectory(task.Workspace) == canonicalDirectory(repo) {
		t.Fatal(task)
	}
	before := len(m.List(0))
	duplicate, e := m.CreateGitHubTask(context.Background(), req)
	if e != nil || duplicate.ID != id || len(m.List(0)) != before {
		t.Fatal(duplicate, e)
	}
	if string(mustRead(t, filepath.Join(repo, "file.txt"))) != "before\n" {
		t.Fatal("original workspace modified")
	}
	if !m.launchGitHubTask(id, "publish") {
		t.Fatal("publish did not launch")
	}
	task = waitWorkflow(t, m, id, "review")
	head := gitTest(t, task.Workspace, "rev-parse", "HEAD")
	if task.CommitSHA != head || task.PRNumber != 9 || task.PRURL == "" {
		t.Fatal(task)
	}
	r, e := m.Verification(context.Background(), task.SessionID)
	if e != nil || r.Stale || r.PRURL == nil || r.TestedTree == "" || r.PublishedCommit != head {
		t.Fatal(r, e)
	}
	if !m.launchGitHubTask(id, "publish") {
		t.Fatal("retry did not launch")
	}
	task = waitWorkflow(t, m, id, "review")
	if gitTest(t, task.Workspace, "rev-parse", "HEAD") != head {
		t.Fatal("duplicate commit")
	}
	// Simulate losing commit/PR receipts after the effects succeeded.
	task.Phase = "ready_for_pr"
	task.CommitIntent = true
	task.CommitSHA = ""
	task.PRURL = ""
	task.PRNumber = 0
	task.PRCreateAttempted = true
	if e = m.saveGitHubTask(&task); e != nil {
		t.Fatal(e)
	}
	if !m.launchGitHubTask(id, "publish") {
		t.Fatal("reconcile did not launch")
	}
	task = waitWorkflow(t, m, id, "review")
	if task.CommitSHA != head || task.PRNumber != 9 {
		t.Fatal(task)
	}
	m.Close()
	recovered, e := NewManager(m.dir)
	if e != nil {
		t.Fatal(e)
	}
	defer recovered.Close()
	stored, e := recovered.GitHubTask(id)
	if e != nil || stored.PRURL != task.PRURL || stored.CommitSHA != head || stored.Running {
		t.Fatal(stored, e)
	}
}
func TestGitHubWorkflowPublishesNewFilesAfterVerification(t *testing.T) {
	m, req, repo := newWorkflowFixture(t)
	t.Setenv("NIMOTE_WORKFLOW_NEW_FILE", "1")
	req.RequestID = "new-file-publication"
	req.DispatchPolicy = "draft_pr_after_verification"
	task, err := m.CreateGitHubTask(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	task = waitWorkflow(t, m, task.ID, "review")
	if got := gitTest(t, task.Workspace, "show", "HEAD:new.txt"); got != "new fixture implementation" {
		t.Fatalf("new file was not published: %q", got)
	}
	if _, err = os.Stat(filepath.Join(repo, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("original workspace changed: %v", err)
	}
	report, err := m.Verification(context.Background(), task.SessionID)
	if err != nil || report.Status != "passed" || report.Stale || report.PublishedCommit != task.CommitSHA {
		t.Fatal(report, err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func newWorkflowFixture(t *testing.T) (*Manager, GitHubTaskRequest, string) {
	t.Helper()
	realGit, e := exec.LookPath("git")
	if e != nil {
		t.Skip("git unavailable")
	}
	repo := testRepo(t)
	gitTest(t, repo, "branch", "-M", "main")
	bare := filepath.Join(t.TempDir(), "remote.git")
	gitTest(t, repo, "clone", "--bare", repo, bare)
	gitTest(t, repo, "remote", "add", "origin", "https://github.com/example/repo.git")

	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	bin := t.TempDir()
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	for _, kind := range []string{"gh", "codex"} {
		script := "#!/bin/sh\nNIMOTE_WORKFLOW_CHILD=" + kind + " exec " + quote(exe) + " -test.run=TestWorkflowCLIChild -- \"$@\"\n"
		if e = os.WriteFile(filepath.Join(bin, kind), []byte(script), 0700); e != nil {
			t.Fatal(e)
		}
	}
	script := "#!/bin/sh\nfor arg in \"$@\"; do\nif [ \"$arg\" = fetch ]; then\nexec \"$NIMOTE_WORKFLOW_GIT\" -C \"$NIMOTE_WORKFLOW_REPO\" fetch --no-tags \"$NIMOTE_WORKFLOW_BARE\" refs/heads/main\nfi\nif [ \"$arg\" = push ]; then\nexec \"$NIMOTE_WORKFLOW_GIT\" -C \"$5\" push \"$NIMOTE_WORKFLOW_BARE\" \"$8\"\nfi\ndone\nexec \"$NIMOTE_WORKFLOW_GIT\" \"$@\"\n"
	if e = os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("NIMOTE_WORKFLOW_GIT", realGit)
	t.Setenv("NIMOTE_WORKFLOW_BARE", bare)
	t.Setenv("NIMOTE_WORKFLOW_REPO", repo)
	t.Setenv("NIMOTE_WORKFLOW_PR", filepath.Join(t.TempDir(), "pr.json"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	m := newTestManager(t)
	req := GitHubTaskRequest{RequestID: "workflow-fixture", IssueURL: "https://github.com/example/repo/issues/4", Directory: repo, Agent: "codex", DispatchPolicy: "manual", Tests: []TestCommand{{Command: realGit, Args: []string{"diff", "--check", "HEAD"}}}}
	return m, req, repo
}

func TestGitHubWorkflowInterruptedConversationContinuesWithoutOldPrompt(t *testing.T) {
	m, req, _ := newWorkflowFixture(t)
	log := filepath.Join(t.TempDir(), "prompts")
	t.Setenv("NIMOTE_WORKFLOW_PROMPTS", log)
	t.Setenv("NIMOTE_WORKFLOW_HOLD", "1")
	req.RequestID = "interrupted-fixture"
	task, e := m.CreateGitHubTask(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		task, e = m.GitHubTask(task.ID)
		if e != nil {
			t.Fatal(e)
		}
		if task.PromptSent {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !task.PromptSent {
		t.Fatal(task)
	}
	original := task.SessionID
	m.Close()
	t.Setenv("NIMOTE_WORKFLOW_HOLD", "0")
	recovered, e := NewManager(m.dir)
	if e != nil {
		t.Fatal(e)
	}
	defer recovered.Close()
	stored, e := recovered.GitHubTask(task.ID)
	if e != nil || stored.Running || stored.SessionID != original {
		t.Fatal(stored, e)
	}
	if !recovered.launchGitHubTask(task.ID, "continue") {
		t.Fatal("continue not launched")
	}
	task = waitWorkflow(t, recovered, task.ID, "ready_for_pr")
	if task.SessionID == original || task.Attempt != 1 {
		t.Fatal(task)
	}
	prompts := strings.Split(strings.TrimSpace(string(mustRead(t, log))), "\n")
	if len(prompts) != 2 || !strings.Contains(prompts[0], "Implement GitHub Issue") || !strings.Contains(prompts[1], "Continue the existing Issue task") || strings.Contains(prompts[1], "Implement GitHub Issue") {
		t.Fatal(prompts)
	}
}
func TestGitHubWorkflowValidationFailurePreservesWorkAndStage(t *testing.T) {
	m, req, repo := newWorkflowFixture(t)
	req.RequestID = "failed-validation"
	req.Tests[0].Args = []string{"diff", "--exit-code", "HEAD"}
	task, e := m.CreateGitHubTask(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	task = waitWorkflow(t, m, task.ID, "verification_failed")
	if task.Error == "" || task.FailureStage != "verification_failed" || task.CommitSHA != "" || task.PRURL != "" {
		t.Fatal(task)
	}
	if string(mustRead(t, filepath.Join(repo, "file.txt"))) != "before\n" || !strings.Contains(string(mustRead(t, filepath.Join(task.Workspace, "file.txt"))), "after fixture") {
		t.Fatal("work was discarded or original modified")
	}
	r, e := m.Verification(context.Background(), task.SessionID)
	if e != nil || r.Status != "failed" || r.Tests[0].ExitCode == nil || *r.Tests[0].ExitCode != 1 {
		t.Fatal(r, e)
	}
}

func TestGitHubWorkflowUnsentPromptCanRetryFailedAdapterStartup(t *testing.T) {
	m, req, _ := newWorkflowFixture(t)
	req.RequestID = "failed-adapter-start"
	log := filepath.Join(t.TempDir(), "prompts")
	t.Setenv("NIMOTE_WORKFLOW_PROMPTS", log)
	t.Setenv("NIMOTE_WORKFLOW_BAD_INIT", "1")
	task, e := m.CreateGitHubTask(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		task, e = m.GitHubTask(task.ID)
		if e != nil {
			t.Fatal(e)
		}
		if !task.Running && task.Error != "" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if task.Error == "" || task.PromptSent || task.InitialPromptSent || task.Workspace == "" {
		t.Fatal(task)
	}
	original := task.SessionID
	directory := task.Workspace
	m.Close()
	t.Setenv("NIMOTE_WORKFLOW_BAD_INIT", "0")
	recovered, e := NewManager(m.dir)
	if e != nil {
		t.Fatal(e)
	}
	defer recovered.Close()
	if !recovered.launchGitHubTask(task.ID, "continue") {
		t.Fatal("retry not launched")
	}
	task = waitWorkflow(t, recovered, task.ID, "ready_for_pr")
	if task.SessionID == original || task.Workspace != directory || !task.InitialPromptSent {
		t.Fatal(task)
	}
	prompts := strings.Split(strings.TrimSpace(string(mustRead(t, log))), "\n")
	if len(prompts) != 1 || !strings.Contains(prompts[0], "Implement GitHub Issue") {
		t.Fatal(prompts)
	}
}

func TestGitHubWorkflowCommitHookChangesRequireFreshVerification(t *testing.T) {
	m, req, repo := newWorkflowFixture(t)
	req.RequestID = "hook-revalidation"
	task, e := m.CreateGitHubTask(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	task = waitWorkflow(t, m, task.ID, "ready_for_pr")
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if e = os.WriteFile(hook, []byte("#!/bin/sh\nprintf 'formatted by hook\\n' > file.txt\ngit add file.txt\n"), 0700); e != nil {
		t.Fatal(e)
	}
	if !m.launchGitHubTask(task.ID, "publish") {
		t.Fatal("publish not launched")
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		task, e = m.GitHubTask(task.ID)
		if e != nil {
			t.Fatal(e)
		}
		if !task.Running && task.Error != "" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if task.PRURL != "" || task.CommitSHA == "" || task.Error == "" {
		t.Fatal(task)
	}
	r, e := m.Verification(context.Background(), task.SessionID)
	if e != nil || !r.Stale {
		t.Fatal(r, e)
	}
	commit := task.CommitSHA
	if !m.launchGitHubTask(task.ID, "verify") {
		t.Fatal("verification not launched")
	}
	task = waitWorkflow(t, m, task.ID, "ready_for_pr")
	if !m.launchGitHubTask(task.ID, "publish") {
		t.Fatal("publish retry not launched")
	}
	task = waitWorkflow(t, m, task.ID, "review")
	if task.CommitSHA != commit || gitTest(t, task.Workspace, "rev-list", "--count", task.BaseSHA+"..HEAD") != "1" {
		t.Fatal("publication created a duplicate commit", task)
	}
}

func TestAgentSelfClaimAndTurnCompletionNeverValidateCode(t *testing.T) {
	m, _, repo := newWorkflowFixture(t)
	t.Setenv("NIMOTE_WORKFLOW_SELF_CLAIM", "1")
	s, e := m.Start(StartRequest{Kind: "agent", Agent: "codex", Interaction: "structured", Directory: repo, Task: &TaskReference{ID: "self-claim", Label: "Generate a result"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.waitAgentReady(s.ID); e != nil {
		t.Fatal(e)
	}
	if e = m.AgentMessage(s.ID, AgentMessage{Action: "prompt", RequestID: "self-claim-prompt", Text: "Do the work"}); e != nil {
		t.Fatal(e)
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		state, e := m.AgentSnapshot(s.ID)
		if e != nil {
			t.Fatal(e)
		}
		if state["status"] == "completed" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	report, e := m.Verification(context.Background(), s.ID)
	if e != nil || report.Status != "not_run" || len(report.Tests) != 0 || report.AgentStatement == nil || report.AgentStatement.Source != "agent_self_report" || !strings.Contains(report.AgentStatement.Text, "All tests passed") {
		t.Fatal(report, e)
	}
}
