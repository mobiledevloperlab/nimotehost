// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestClaudeNativeDiscoveryStateIdentityAndCapabilities(t *testing.T) {
	cwd := t.TempDir()
	for _, tc := range []struct {
		status, waiting, state, want string
		pid                          int
	}{
		{"busy", "", "working", "running", 42},
		{"waiting", "permission prompt", "blocked", "needs_approval", 42},
		{"waiting", "input needed", "blocked", "unknown", 42},
		{"idle", "", "done", "idle", 42},
		{"busy", "", "working", "unknown", 0},
		{"", "", "done", "completed", 0},
		{"", "", "failed", "failed", 0},
		{"future-status", "", "future-state", "unknown", 42},
	} {
		data, _ := json.Marshal([]any{map[string]any{"kind": "background", "id": "job_original", "sessionId": "original-session", "cwd": cwd, "startedAt": int64(1700000000000), "status": tc.status, "waitingFor": tc.waiting, "state": tc.state, "pid": tc.pid}})
		page, err := projectClaudeLiveSessions(data, cwd, "")
		if err != nil || len(page.Items) != 1 {
			t.Fatal(page, err)
		}
		item := page.Items[0]
		caps := item["session_capabilities"].(ExternalSessionCapabilities)
		if item["conversation_id"] != "original-session" || item["external_session_id"] != "job_original" || item["execution_status"] != tc.want || item["status_source"] != "claude_agents_json" || item["origin"] != "external_live" || caps.Discovery != "supported" || caps.LiveControl != "unsupported" || caps.History != "unsupported" || item["resumable"] != false {
			t.Fatal("discovery overstated native capabilities", item)
		}
	}
	data, _ := json.Marshal([]any{map[string]any{"kind": "interactive", "cwd": cwd, "startedAt": int64(1700000000000), "status": "busy", "pid": 42}})
	page, err := projectClaudeLiveSessions(data, cwd, "")
	if err != nil || page.Items[0]["conversation_id"] != "" || page.Items[0]["external_session_id"] != "" {
		t.Fatal("invented a native ID from the PID", page, err)
	}
}

func TestClaudeNativeDiscoveryBoundariesPaginationAndDeletion(t *testing.T) {
	cwd := t.TempDir()
	rows := []map[string]any{}
	for i := range 51 {
		rows = append(rows, map[string]any{"kind": "background", "id": fmt.Sprintf("job_%03d", i), "cwd": cwd, "startedAt": 1})
	}
	data, _ := json.Marshal(rows)
	page, err := projectClaudeLiveSessions(data, cwd, "")
	if err != nil || len(page.Items) != 50 || page.NextCursor == nil {
		t.Fatal(page, err)
	}
	second, err := projectClaudeLiveSessions(data, cwd, *page.NextCursor)
	if err != nil || len(second.Items) != 1 || second.NextCursor != nil {
		t.Fatal(second, err)
	}
	rows = append(rows[:49], rows[50:]...)
	deleted, _ := json.Marshal(rows)
	if _, err := projectClaudeLiveSessions(deleted, cwd, *page.NextCursor); err == nil {
		t.Fatal("deleted cursor boundary accepted")
	}
	filtered, err := projectClaudeLiveSessions(data, t.TempDir(), "")
	if err != nil || len(filtered.Items) != 0 {
		t.Fatal("provider workspace filter was trusted without validation", filtered, err)
	}
	for _, bad := range []string{"null", "{}", "[]\n{}", strings.Repeat(" ", 2<<20+1), `[{"kind":"background","id":"../job","cwd":"/workspace","startedAt":1}]`} {
		if _, err := projectClaudeLiveSessions([]byte(bad), "", ""); err == nil {
			t.Fatal("invalid or oversized native result accepted")
		}
	}
	duplicate, _ := json.Marshal([]map[string]any{rows[0], rows[0]})
	if _, err := projectClaudeLiveSessions(duplicate, cwd, ""); err == nil {
		t.Fatal("duplicate identities accepted")
	}
}

func TestClaudeNativeDiscoveryDoesNotAuthorizeLiveControl(t *testing.T) {
	c := ExternalAttachment{ExternalConnection: ExternalConnection{Transport: "claude-cli"}}
	if err := c.validateDiscovery("claude"); err != nil {
		t.Fatal(err)
	}
	if err := c.validate("claude"); err == nil {
		t.Fatal("read-only CLI discovery became live control")
	}
	for _, mutate := range []func(*ExternalAttachment){func(c *ExternalAttachment) { c.Endpoint = "http://127.0.0.1:4096" }, func(c *ExternalAttachment) { c.Password = "secret" }, func(c *ExternalAttachment) { c.Username = "owner" }} {
		copy := c
		mutate(&copy)
		if err := copy.validateDiscovery("claude"); err == nil {
			t.Fatal("unexpected connection/auth data accepted")
		}
	}
	if err := c.validateDiscovery("codex"); err == nil {
		t.Fatal("wrong provider accepted")
	}
	m := newTestManager(t)
	if _, err := m.Start(StartRequest{Kind: "agent", Agent: "claude", Interaction: "auto", Directory: t.TempDir(), Resume: &AgentResume{ConversationID: "original"}, Attach: &c}); err == nil {
		t.Fatal("read-only native discovery launched or attached an Agent")
	}
}

func TestClaudeNativeDiscoveryCLIAndOwnerAuthorization(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture; projection and validation run on all platforms")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" != agents ] || [ \"$2\" != --json ]; then exit 9; fi\ncase \"$NIMOTE_CLAUDE_DISCOVERY_FIXTURE\" in\n fail) echo sensitive-error >&2; exit 7;;\n hang) exec /bin/sleep 30;;\n esac\nprintf '%s' \"$NIMOTE_CLAUDE_DISCOVERY_ROWS\"\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("NIMOTE_CLAUDE_DISCOVERY_ROWS", "[]")
	api := &Server{token: "owner", manager: newTestManager(t)}
	body := `{"agent":"claude","connection":{"transport":"claude-cli"}}`
	if reply := deviceRequest(api, "wrong", "POST", "/v1/agent-history", body); reply.Code != 401 {
		t.Fatal(reply.Code)
	}
	if reply := deviceRequest(api, "owner", "POST", "/v1/agent-history", body); reply.Code != 200 || !strings.Contains(reply.Body.String(), `"items":[]`) {
		t.Fatal(reply.Code, reply.Body.String())
	}
	caps := capabilities("fixture")
	if !caps.Features["claude_live_discovery_v1"] || caps.Features["claude_structured"] || caps.Agents["claude"].SessionCapabilities.Discovery != "conditional" {
		t.Fatal("native CLI discovery requires an unrelated ACP installation", caps.Agents["claude"])
	}
	t.Setenv("NIMOTE_CLAUDE_DISCOVERY_FIXTURE", "fail")
	if _, err := listClaudeLiveSessions(context.Background(), "", ""); err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatal("native failure leaked or silently succeeded", err)
	}
	t.Setenv("NIMOTE_CLAUDE_DISCOVERY_FIXTURE", "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := listClaudeLiveSessions(ctx, "", ""); err == nil || time.Since(start) > 3*time.Second {
		t.Fatal("native discovery did not cancel", err)
	}
}

func TestClaudeNativeDiscoveryInstalledCLI(t *testing.T) {
	if os.Getenv("NIMOTE_TEST_CLAUDE_DISCOVERY") != "1" {
		t.Skip("enable the installed Claude read-only check explicitly")
	}
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("DISABLE_AUTOUPDATER", "1")
	page, err := listClaudeLiveSessions(context.Background(), t.TempDir(), "")
	if err != nil || len(page.Items) != 0 {
		t.Fatal("installed CLI with isolated empty config failed", page, err)
	}
}

func TestClaudeInstalledACPInitialization(t *testing.T) {
	if os.Getenv("NIMOTE_TEST_CLAUDE_ACP") != "1" {
		t.Skip("enable the installed ACP handshake check explicitly")
	}
	// No real credentials, stored sessions, hooks or model endpoints are used.
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer endpoint.Close()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CLAUDE_CODE_SIMPLE", "1")
	t.Setenv("DISABLE_AUTOUPDATER", "1")
	t.Setenv("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1")
	t.Setenv("ANTHROPIC_API_KEY", "nimote-isolated-fixture")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_BASE_URL", endpoint.URL)
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "")
	t.Setenv("CLAUDE_CODE_USE_VERTEX", "")
	t.Setenv("CLAUDE_CODE_USE_FOUNDRY", "")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	raw, _, err := acpHistoryRPC(ctx, "claude", t.TempDir(), "session/list", map[string]any{"limit": 50})
	if err != nil {
		t.Fatal("installed ACP initialization/list failed", err)
	}
	page, err := projectACPSessions("claude", raw)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("isolated ACP listing is incompatible", page, err)
	}
	m := newTestManager(t)
	session, err := m.Start(StartRequest{Kind: "agent", Agent: "claude", Interaction: "auto", Directory: t.TempDir(), RequestID: "isolated-claude-initialize"})
	if err != nil || session.Interaction != "structured" {
		t.Fatal("installed ACP session initialization failed or silently fell back", session, err)
	}
	// Start returns the admission record. Native ID assignment is published by
	// the existing asynchronous session update, not that initial copy.
	waitExternalCondition(t, func() bool { current, err := m.Get(session.ID); return err == nil && current.ConversationID != "" })
	if err := m.Cancel(session.ID); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, session.ID)
}
