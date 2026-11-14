// SPDX-License-Identifier: Apache-2.0
package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func policyAgent(t *testing.T, provider, policy string) *structuredAgent {
	t.Helper()
	a := newStructuredAgent(provider, new(bytes.Buffer), t.TempDir())
	a.thread = "thread"
	a.busy = true
	a.executionPolicy = policy
	if err := os.WriteFile(filepath.Join(a.cwd, "README.md"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	return a
}
func commandApproval(a *structuredAgent, command string) json.RawMessage {
	data, _ := json.Marshal(map[string]any{"threadId": a.thread, "command": command, "cwd": a.cwd})
	return data
}

func TestSmartCommandPolicyScopesAndSyntax(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX command recognizer; Windows uses structured tool permissions")
	}
	a := policyAgent(t, "codex", "smart")
	tests := []struct {
		command string
		want    bool
	}{
		{"pwd", true}, {"ls -la", true}, {"cat README.md", true}, {"head -n 10 README.md", true}, {"rg --no-config -n hello README.md", true}, {"cat 'README.md'", true},
		{"rm README.md", false}, {"git push", false}, {"python3 script.py", false}, {"rg --pre evil hello .", false}, {"ls; rm README.md", false}, {"cat README.md > out", false}, {"cat $(whoami)", false}, {"cat `whoami`", false}, {"cat ../secret", false}, {"cat /etc/passwd", false}, {"cat .env", false}, {"cat .git/config", false}, {"rg --hidden hello .", false}, {"rg -uuu hello .", false}, {"cat \"README.md", false}, {"cat *.md", false}, {"sh -c pwd", false}, {"/tmp/cat README.md", false}, {"ls\nrm README.md", false},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			if got := a.simpleReadCommand(tt.command, a.cwd); got != tt.want {
				t.Fatalf("%q = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(a.cwd, "escape")); err != nil {
		t.Fatal(err)
	}
	if a.workspacePath("escape/new.txt") {
		t.Fatal("symlink escaped workspace")
	}
	if a.workspacePath("escape/../secret") || a.simpleReadCommand("cat escape/../secret", a.cwd) {
		t.Fatal("parent traversal after a symlink escaped workspace")
	}
	if a.simpleReadCommand("pwd", outside) {
		t.Fatal("foreign working directory auto-approved")
	}
}
func TestSmartRequestsDoNotApproveEscalationsOrUnavailableDecisions(t *testing.T) {
	a := policyAgent(t, "codex", "smart")
	for _, extra := range []map[string]any{
		{"threadId": "other"}, {"additionalPermissions": map[string]any{"network": true}}, {"networkApprovalContext": map[string]any{"host": "example.com"}}, {"availableDecisions": []string{"decline"}}, {"kind": "stdin"},
	} {
		var p map[string]any
		_ = json.Unmarshal(commandApproval(a, "pwd"), &p)
		for k, v := range extra {
			p[k] = v
		}
		raw, _ := json.Marshal(p)
		if _, ok := a.automaticApproval("item/commandExecution/requestApproval", raw); ok {
			t.Fatal("escalation auto-approved", extra)
		}
	}
	if _, ok := a.automaticApproval("item/fileChange/requestApproval", commandApproval(a, "pwd")); ok {
		t.Fatal("native file-change escalation auto-approved")
	}
}

func TestSmartRipgrepRequiresAnActualNoConfigOption(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX command recognizer")
	}
	t.Setenv("RIPGREP_CONFIG_PATH", filepath.Join(t.TempDir(), "config"))
	a := policyAgent(t, "codex", "smart")
	for _, command := range []string{
		"rg hello README.md", "rg hello file--no-config", "rg -- --no-config README.md",
		"rg '' -- --no-config", "rg '--no-config ' README.md",
	} {
		if a.simpleReadCommand(command, a.cwd) {
			t.Errorf("configuration could execute a preprocessor: %s", command)
		}
	}
	if !a.simpleReadCommand("rg --no-config -- '' README.md", a.cwd) {
		t.Fatal("explicitly disabled configuration rejected")
	}
	words, ok := simpleCommandWords("rg '' -- --no-config")
	if !ok || len(words) != 4 || words[1] != "" {
		t.Fatalf("empty shell argument lost: %q", words)
	}
}

func TestExecutionPolicyPersistenceFailureDoesNotApplyOrAcknowledge(t *testing.T) {
	for _, provider := range []string{"codex", "opencode", "claude"} {
		t.Run(provider, func(t *testing.T) {
			m := newTestManager(t)
			revision := m.revision
			a := policyAgent(t, provider, "confirm")
			a.busy = false
			item := &managed{session: Session{ID: fmt.Sprintf("%064x", 1), ExecutionPolicy: "confirm"}}
			a.onExecutionPolicy = func(policy string) error { return m.setExecutionPolicy(item, policy) }
			path := filepath.Join(m.dir, item.session.ID+".json")
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			req := AgentMessage{Action: "configure", RequestID: "policy", OptionID: executionPolicyOption, Value: "auto"}
			if a.message(req) == nil {
				t.Fatal("failed persistence was acknowledged")
			}
			if a.effectiveExecutionPolicy() != "confirm" || item.session.ExecutionPolicy != "confirm" || a.seen[req.RequestID] || m.revision != revision {
				t.Fatal("failed persistence changed effective policy or metadata")
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := a.message(req); err != nil {
				t.Fatal(err)
			}
			var stored Session
			raw, err := os.ReadFile(path)
			if err != nil || json.Unmarshal(raw, &stored) != nil || stored.ExecutionPolicy != "auto" || a.effectiveExecutionPolicy() != "auto" || item.session.ExecutionPolicy != "auto" {
				t.Fatalf("retry did not durably apply policy: %s, %v", raw, err)
			}
		})
	}
}

func TestExecutionPoliciesPreserveCodexNativeSecurityConfiguration(t *testing.T) {
	for _, policy := range []string{"confirm", "smart", "auto"} {
		for _, resume := range []string{"", "existing-thread"} {
			t.Run(policy+"/"+resume, func(t *testing.T) {
				a, _ := testAgent()
				a.executionPolicy = policy
				a.resumeConversation = resume
				a.receiveCodex(codexFrame{ID: json.RawMessage(`"host-init"`), Result: json.RawMessage(`{}`)})
				queuedFrame(t, a) // initialized notification
				params := queuedFrame(t, a)["params"].(map[string]any)
				if _, ok := params["approvalPolicy"]; ok {
					t.Fatal("thread approval configuration overridden", params)
				}
				if _, ok := params["sandbox"]; ok {
					t.Fatal("thread sandbox configuration overridden", params)
				}
				a.thread = "thread"
				if err := a.message(AgentMessage{Action: "prompt", RequestID: "prompt", Text: "hello"}); err != nil {
					t.Fatal(err)
				}
				params = queuedFrame(t, a)["params"].(map[string]any)
				if _, ok := params["approvalPolicy"]; ok {
					t.Fatal("turn approval configuration overridden", params)
				}
				if _, ok := params["sandboxPolicy"]; ok {
					t.Fatal("turn sandbox configuration overridden", params)
				}
			})
		}
	}
}
func TestAutomaticApprovalsRunWithoutAClientAndNeverPersistACPGrants(t *testing.T) {
	for _, provider := range []string{"codex", "opencode", "claude"} {
		t.Run(provider, func(t *testing.T) {
			a := policyAgent(t, provider, "auto")
			method := "item/commandExecution/requestApproval"
			raw := commandApproval(a, "custom-build")
			if a.acp != nil {
				method = "session/request_permission"
				raw = json.RawMessage(`{"sessionId":"thread","toolCall":{"toolCallId":"tool","kind":"execute"},"options":[{"kind":"allow_always","optionId":"always"},{"kind":"allow_once","optionId":"once"}]}`)
			}
			result, err := a.handleAgentRPC(context.Background(), method, raw)
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(result)
			if a.acp != nil && !bytes.Contains(encoded, []byte(`"optionId":"once"`)) {
				t.Fatal(string(encoded))
			}
			if len(a.approvals) != 0 || len(a.rpc.permissions) != 0 {
				t.Fatal("automatic decision created pending UI state")
			}
			a.executionPolicy = "confirm"
			if _, ok := a.automaticApproval(method, raw); ok {
				t.Fatal("confirm mode bypassed")
			}
			a.executionPolicy = "auto"
			if a.acp != nil {
				raw = json.RawMessage(`{"sessionId":"thread","toolCall":{"toolCallId":"tool"},"options":[{"kind":"allow_always","optionId":"always"}]}`)
				if _, ok := a.automaticApproval(method, raw); ok {
					t.Fatal("auto approval broadened into a persistent grant")
				}
			}
		})
	}
}
func TestSmartACPUsesStructuredEvidenceAndWorkspaceBounds(t *testing.T) {
	a := policyAgent(t, "opencode", "smart")
	tests := []struct {
		tool string
		want bool
	}{
		{`{"toolCallId":"tool","kind":"read","locations":[{"path":"README.md"}]}`, true},
		{`{"toolCallId":"tool","kind":"search","locations":[{"path":"."}]}`, true},
		{`{"toolCallId":"tool","kind":"edit","content":[{"type":"diff","path":"new.txt","oldText":"old","newText":"new"}]}`, true},
		{`{"toolCallId":"tool","kind":"delete","locations":[{"path":"README.md"}]}`, false},
		{`{"toolCallId":"tool","kind":"read","locations":[{"path":"/etc/passwd"}]}`, false},
		{`{"toolCallId":"tool","kind":"read","locations":[{"path":".env"}]}`, false},
		{`{"toolCallId":"tool","title":"Read source"}`, false},
		{`{"toolCallId":"tool","kind":"edit","locations":[{"path":"README.md"}]}`, false},
		{`{"toolCallId":"tool","kind":"read","locations":[{"path":"README.md"}],"rawInput":{"path":"/etc/passwd"}}`, false},
	}
	for _, tt := range tests {
		raw := json.RawMessage(`{"sessionId":"thread","toolCall":` + tt.tool + `,"options":[{"optionId":"once","kind":"allow_once"}]}`)
		_, got := a.automaticApproval("session/request_permission", raw)
		if got != tt.want {
			t.Errorf("%s = %v want %v", tt.tool, got, tt.want)
		}
	}
}
func TestPolicyChangesRequireIdleAndAreProjectedInControls(t *testing.T) {
	for _, provider := range []string{"codex", "opencode", "claude"} {
		a := policyAgent(t, provider, "confirm")
		req := AgentMessage{Action: "configure", RequestID: "smart", OptionID: executionPolicyOption, Value: "smart"}
		a.busy = true
		if a.message(req) == nil {
			t.Fatal("changed policy during active turn")
		}
		a.busy = false
		if err := a.message(req); err != nil {
			t.Fatal(err)
		}
		if a.effectiveExecutionPolicy() != "smart" {
			t.Fatal("policy not applied")
		}
		found := false
		for _, option := range a.controls.Options {
			if option.ID == executionPolicyOption && option.Value == "smart" {
				found = true
			}
		}
		if !found {
			t.Fatal("effective policy not advertised")
		}
		if err := a.message(req); err != nil {
			t.Fatal("idempotent retry failed", err)
		}
		req.RequestID = "bad"
		req.Value = "unknown"
		if a.message(req) == nil {
			t.Fatal("unknown policy accepted")
		}
	}
}
func TestPoliciesNeverSilentlyFallBackToAnUncontrolledTerminal(t *testing.T) {
	for _, req := range []StartRequest{{Kind: "agent", Interaction: "terminal", ExecutionPolicy: "smart"}, {Kind: "task", ExecutionPolicy: "auto"}, {Kind: "agent", Interaction: "structured", ExecutionPolicy: "unknown"}} {
		if resolveCommand(&req) == nil {
			t.Fatal("invalid policy accepted", req)
		}
	}
	m := new(Manager)
	if _, err := m.startAuto(StartRequest{Kind: "agent", Agent: "codex", Interaction: "auto", ExecutionPolicy: "smart"}, 0); err == nil {
		t.Fatal("policy silently downgraded to auto transport")
	}
}
