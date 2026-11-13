// SPDX-License-Identifier: Apache-2.0
//go:build linux || darwin

package host

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestACPProtocolChild(t *testing.T) {
	if os.Getenv("NIMOTE_ACP_PROTOCOL_CHILD") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var frame map[string]any
		_ = json.Unmarshal(scanner.Bytes(), &frame)
		switch frame["method"] {
		case "initialize":
			version := 1
			if params, ok := frame["params"].(map[string]any); ok && params["protocolVersion"] == float64(2) {
				version = 2
			}
			if os.Getenv("NIMOTE_ACP_PROTOCOL_VARIANT") == "unsupported" {
				version = 99
			}
			if os.Getenv("NIMOTE_ACP_PROTOCOL_VARIANT") == "timeout" {
				continue
			}
			fmt.Printf("{\"jsonrpc\":\"2.0\",\"id\":%v,\"result\":{\"protocolVersion\":%d,\"futureField\":true}}\n", frame["id"], version)
		case "session/new":
			fmt.Printf("{\"jsonrpc\":\"2.0\",\"id\":%v,\"result\":{\"sessionId\":\"fake-acp\"}}\n", frame["id"])
		case "session/prompt":
			fmt.Println(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"fake-acp","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello from ACP subprocess"}}}}`)
			fmt.Printf("{\"jsonrpc\":\"2.0\",\"id\":%v,\"result\":{\"stopReason\":\"end_turn\"}}\n", frame["id"])
		}
	}
	os.Exit(0)
}
func TestOpenCodeAutoProtocolAndTerminalFallback(t *testing.T) {
	for _, variant := range []string{"compatible", "unsupported", "timeout"} {
		t.Run(variant, func(t *testing.T) {
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			script := "#!/bin/sh\nif [ \"$1\" = acp ]; then\nexec '" + strings.ReplaceAll(exe, "'", "'\\''") + "' -test.run=TestACPProtocolChild\nfi\nprintf 'OPENCODE_TERMINAL_READY\\n'\nread value\n"
			if err = os.WriteFile(filepath.Join(dir, "opencode"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("NIMOTE_ACP_PROTOCOL_CHILD", "1")
			t.Setenv("NIMOTE_ACP_PROTOCOL_VARIANT", variant)
			m := newTestManager(t)
			timeout := 10 * time.Second
			if variant == "timeout" {
				timeout = 200 * time.Millisecond
			}
			session, err := m.startAuto(StartRequest{Kind: "agent", Agent: "opencode", Interaction: "auto", Directory: dir}, timeout)
			if err != nil {
				t.Fatal(err)
			}
			if variant == "compatible" {
				if session.Interaction != "structured" || session.CompatibilityFallback != "" {
					t.Fatal(session)
				}
				if err = m.AgentMessage(session.ID, AgentMessage{Action: "prompt", RequestID: "p1", Text: "hello"}); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(5 * time.Second)
				found := false
				for time.Now().Before(deadline) {
					output, _, err := m.Output(session.ID, 0)
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(string(output), "hello from ACP subprocess") {
						found = true
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if !found {
					t.Fatal("ACP response missing")
				}
				snapshot, err := m.AgentSnapshot(session.ID)
				if err != nil || snapshot["ready"] != true {
					t.Fatal(snapshot, err)
				}
			} else {
				if session.Interaction != "terminal" || !strings.Contains(session.CompatibilityFallback, "opencode") {
					t.Fatal(session)
				}
				if err = m.Input(session.ID, []byte("exit\n")); err != nil {
					t.Fatal(err)
				}
				awaitSession(t, m, session.ID)
			}
			if len(m.List(0)) != 1 {
				t.Fatal("probe left an orphaned session")
			}
		})
	}
}

func TestInstalledOpenCodeHandshake(t *testing.T) {
	if os.Getenv("NIMOTE_TEST_INSTALLED_OPENCODE") != "1" {
		t.Skip("set NIMOTE_TEST_INSTALLED_OPENCODE=1 for the installed CLI handshake (no model request)")
	}
	// Keep the smoke test independent of user plugins, credentials and settings.
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
		t.Setenv(name, t.TempDir())
	}
	m := newTestManager(t)
	session, err := m.Start(StartRequest{Kind: "agent", Agent: "opencode", Interaction: "auto", Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if session.Interaction != "structured" {
		t.Fatalf("installed OpenCode fell back: %s", session.CompatibilityFallback)
	}
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		output, _, err := m.Output(session.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(output), `"type":"ready"`) {
			if err = m.Cancel(session.ID); err != nil {
				t.Fatal(err)
			}
			awaitSession(t, m, session.ID)
			return
		}
		if strings.Contains(string(output), `"type":"error"`) {
			t.Fatal(string(output))
		}
		time.Sleep(25 * time.Millisecond)
	}
	output, _, _ := m.Output(session.ID, 0)
	t.Fatalf("installed OpenCode failed to initialize: %s", output)
}

func TestCancelDuringACPProbeDoesNotLaunchFallback(t *testing.T) {
	dir := t.TempDir()
	// Silent protocol initialization keeps the probe pending until canceled.
	script := "#!/bin/sh\nif [ \"$1\" = acp ]; then\nwhile read value; do :; done\nelse\nprintf 'UNEXPECTED_TERMINAL_FALLBACK\\n'\nfi\n"
	if err := os.WriteFile(filepath.Join(dir, "opencode"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	m := newTestManager(t)
	type started struct {
		session Session
		err     error
	}
	result := make(chan started, 1)
	go func() {
		s, err := m.startAuto(StartRequest{Kind: "agent", Agent: "opencode", Interaction: "auto", Directory: dir}, 10*time.Second)
		result <- started{s, err}
	}()
	deadline := time.Now().Add(5 * time.Second)
	var id string
	for time.Now().Before(deadline) {
		rows := m.List(0)
		if len(rows) > 0 {
			id = rows[0].ID
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("probe never started")
	}
	if err := m.Cancel(id); err != nil {
		t.Fatal(err)
	}
	select {
	case started := <-result:
		if started.err != nil || started.session.ID != id || started.session.State != "canceled" || started.session.Interaction != "structured" {
			t.Fatal(started)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled probe did not return")
	}
	if len(m.List(0)) != 1 {
		t.Fatal("cancel launched an unexpected new session")
	}
}

func TestClaudeMaintainedAdapterAndTerminalFallback(t *testing.T) {
	for _, installed := range []bool{true, false} {
		t.Run(fmt.Sprint(installed), func(t *testing.T) {
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err = os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\nprintf 'CLAUDE_TERMINAL_READY\\n'\nread value\n"), 0700); err != nil {
				t.Fatal(err)
			}
			if installed {
				script := "#!/bin/sh\nexec '" + strings.ReplaceAll(exe, "'", "'\\''") + "' -test.run=TestACPProtocolChild\n"
				if err = os.WriteFile(filepath.Join(dir, "claude-agent-acp"), []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
			}
			// Do not inherit a developer's globally installed adapter in the missing case.
			t.Setenv("PATH", dir)
			t.Setenv("NIMOTE_ACP_PROTOCOL_CHILD", "1")
			caps := capabilities("test")
			if caps.Features["claude_structured"] != installed {
				t.Fatal(caps.Features)
			}
			m := newTestManager(t)
			session, err := m.startAuto(StartRequest{Kind: "agent", Agent: "claude", Interaction: "auto", Directory: dir}, 10*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if installed {
				if session.Interaction != "structured" {
					t.Fatal(session)
				}
				if err = m.AgentMessage(session.ID, AgentMessage{Action: "prompt", RequestID: "claude-prompt", Text: "hello"}); err != nil {
					t.Fatal(err)
				}
			} else {
				if session.Interaction != "terminal" || !strings.Contains(session.CompatibilityFallback, "adapter is not installed") {
					t.Fatal(session)
				}
				if err = m.Input(session.ID, []byte("exit\n")); err != nil {
					t.Fatal(err)
				}
				awaitSession(t, m, session.ID)
			}
		})
	}
}

func TestInstalledClaudeAdapterStartup(t *testing.T) {
	if os.Getenv("NIMOTE_TEST_INSTALLED_CLAUDE_ACP") != "1" {
		t.Skip("set NIMOTE_TEST_INSTALLED_CLAUDE_ACP=1 for the maintained adapter startup probe (no model request)")
	}
	// Isolate the smoke test from user hooks, MCP servers and credentials.
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
		t.Setenv(name, t.TempDir())
	}
	m := newTestManager(t)
	session, err := m.startAuto(StartRequest{Kind: "agent", Agent: "claude", Interaction: "auto", Directory: t.TempDir()}, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if session.Interaction != "structured" {
		reason := strings.ToLower(session.CompatibilityFallback)
		if !strings.Contains(reason, "auth") && !strings.Contains(reason, "login") && !strings.Contains(reason, "log in") && !strings.Contains(reason, "api key") {
			t.Fatal(session.CompatibilityFallback)
		}
		t.Log("ACP startup requires authentication; terminal fallback verified without a model request")
	} else {
		t.Log("Maintained Claude adapter initialized a structured session without a model request")
	}
	if err = m.Cancel(session.ID); err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, session.ID)
}
