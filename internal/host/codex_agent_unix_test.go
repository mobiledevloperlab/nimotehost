// SPDX-License-Identifier: Apache-2.0
//go:build linux || darwin

package host

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A real subprocess speaking the app-server handshake and turn protocol. It
// needs no account, network, or model call, and verifies daemon-owned stdin.
func TestCodexProtocolChild(t *testing.T) {
	if os.Getenv("NIMOTE_CODEX_PROTOCOL_CHILD") != "1" {
		return
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var f map[string]any
		_ = json.Unmarshal(scan.Bytes(), &f)
		switch f["method"] {
		case "initialize":
			if delay, _ := strconv.Atoi(os.Getenv("NIMOTE_CODEX_INIT_DELAY_MS")); delay > 0 {
				time.Sleep(time.Duration(delay) * time.Millisecond)
			}
			switch os.Getenv("NIMOTE_CODEX_PROTOCOL_VARIANT") {
			case "incompatible":
				fmt.Printf("{\"id\":%v,\"error\":{\"code\":-32601,\"message\":\"initialize removed\"}}\n", f["id"])
				continue
			case "timeout":
				continue
			}
			fmt.Printf("{\"id\":%v,\"result\":{}}\n", f["id"])
		case "thread/start":
			if os.Getenv("NIMOTE_CODEX_PROTOCOL_VARIANT") == "broken-input" {
				_ = os.Stdin.Close()
				fmt.Printf("{\"id\":%v,\"result\":{\"thread\":{\"id\":\"fake-thread\"}}}\n", f["id"])
				time.Sleep(time.Minute)
				return
			}
			fmt.Printf("{\"id\":%v,\"result\":{\"thread\":{\"id\":\"fake-thread\"}}}\n", f["id"])
		case "turn/start":
			fmt.Println(`{"method":"turn/started","params":{"turn":{"id":"fake-turn"}}}`)
			if os.Getenv("NIMOTE_CODEX_PROTOCOL_VARIANT") == "smart-approval" {
				cwd, _ := os.Getwd()
				request, _ := json.Marshal(map[string]any{"id": "native-approval", "method": "item/commandExecution/requestApproval", "params": map[string]any{"threadId": "fake-thread", "turnId": "fake-turn", "command": "cat README.md", "cwd": cwd}})
				fmt.Println(string(request))
				continue
			}
			fmt.Println(`{"method":"item/agentMessage/delta","params":{"itemId":"answer","delta":"hello from subprocess"}}`)
			fmt.Println(`{"method":"turn/completed","params":{"turn":{"id":"fake-turn","status":"completed"}}}`)
		default:
			if f["id"] == "native-approval" {
				result, _ := f["result"].(map[string]any)
				if result["decision"] != "accept" {
					os.Exit(2)
				}
				fmt.Println(`{"method":"item/agentMessage/delta","params":{"itemId":"answer","delta":"approval received without UI"}}`)
				fmt.Println(`{"method":"turn/completed","params":{"turn":{"id":"fake-turn","status":"completed"}}}`)
			}
		}
	}
	os.Exit(0)
}

func TestBrokenAgentInputTerminatesManagedProcess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(exe, "'", "'\\''") + "' -test.run=TestCodexProtocolChild\n"
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NIMOTE_CODEX_PROTOCOL_CHILD", "1")
	t.Setenv("NIMOTE_CODEX_PROTOCOL_VARIANT", "broken-input")
	m := newTestManager(t)
	s, err := m.Start(StartRequest{Kind: "agent", Agent: "codex", Interaction: "auto", Directory: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.AgentMessage(s.ID, AgentMessage{Action: "prompt", RequestID: "p", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	ended := awaitSession(t, m, s.ID)
	if ended.State != "failed" || !strings.Contains(ended.Error, "agent input failed") {
		t.Fatal("broken input did not fail the managed session", ended)
	}
	state, err := m.AgentSnapshot(s.ID)
	if err != nil || state["busy"] != false || state["ready"] != false {
		t.Fatal(state, err)
	}
}
func TestStructuredAgentProcessLifecycle(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(exe, "'", "'\\''") + "' -test.run=TestCodexProtocolChild\n"
	if err = os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NIMOTE_CODEX_PROTOCOL_CHILD", "1")
	m := newTestManager(t)
	s, err := m.Start(StartRequest{Kind: "agent", Agent: "codex", Interaction: "structured", Directory: dir})
	if err != nil {
		t.Fatal(err)
	}
	awaitOutput := func(fragment string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			data, _, err := m.Output(s.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), fragment) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("missing output: " + fragment)
	}
	awaitOutput(`"type":"ready"`)
	snapshot, err := m.AgentSnapshot(s.ID)
	if err != nil || snapshot["ready"] != true || snapshot["busy"] != false {
		t.Fatalf("snapshot: %v %v", snapshot, err)
	}
	_, cursor, err := m.Output(s.ID, 0)
	if err != nil || snapshot["cursor"] != cursor {
		t.Fatalf("snapshot cursor: %v %d %v", snapshot, cursor, err)
	}
	if err = m.Input(s.ID, []byte("raw input")); err == nil {
		t.Fatal("terminal input accepted")
	}
	if err = m.AgentMessage(s.ID, AgentMessage{Action: "prompt", RequestID: "hello-1", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	awaitOutput("hello from subprocess")
	// Re-attaching reads the same durable event log, without starting a new agent.
	awaitOutput(`"type":"user"`)
	api := &Server{manager: m, token: "owner", admission: newRequestAdmission()}
	path := "/v1/sessions/" + s.ID
	if w := deviceRequest(api, "owner", "POST", path+"/cancel", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	ended := awaitSession(t, m, s.ID)
	if ended.Interaction != "structured" || ended.State != "canceled" || ended.Error != "" {
		t.Fatal(ended)
	}
	data, _, err := m.Output(s.ID, 0)
	if err != nil || strings.Contains(string(data), `"type":"error"`) {
		t.Fatalf("unexpected errors after stopping Agent: %s (%v)", data, err)
	}
	if err = m.AgentMessage(s.ID, AgentMessage{Action: "prompt", RequestID: "after-stop", Text: "hello"}); err == nil {
		t.Fatal("stopped process accepted prompt")
	}
}

func TestInstalledCodexHandshake(t *testing.T) {
	if os.Getenv("NIMOTE_TEST_INSTALLED_CODEX") != "1" {
		t.Skip("set NIMOTE_TEST_INSTALLED_CODEX=1 for the installed CLI handshake (no model request)")
	}
	m := newTestManager(t)
	s, err := m.Start(StartRequest{Kind: "agent", Agent: "codex", Interaction: "auto", Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if s.Interaction != "structured" {
		t.Fatalf("installed Codex fell back: %s", s.CompatibilityFallback)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		data, _, err := m.Output(s.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `"type":"ready"`) {
			_ = m.Cancel(s.ID)
			awaitSession(t, m, s.ID)
			return
		}
		if strings.Contains(string(data), `"type":"error"`) {
			t.Fatal(string(data))
		}
		time.Sleep(25 * time.Millisecond)
	}
	data, _, _ := m.Output(s.ID, 0)
	t.Fatalf("installed Codex failed to initialize: %s", data)
}

func TestAutoAgentCompatibilityFallback(t *testing.T) {
	for _, variant := range []string{"incompatible", "timeout", "compatible"} {
		t.Run(variant, func(t *testing.T) {
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			// The terminal branch never receives or replays a user prompt.
			script := "#!/bin/sh\nif [ \"$1\" = app-server ]; then\nexec '" + strings.ReplaceAll(exe, "'", "'\\''") + "' -test.run=TestCodexProtocolChild\nfi\nprintf 'TERMINAL_FALLBACK_READY\\n'\nread value\n"
			if err = os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("NIMOTE_CODEX_PROTOCOL_CHILD", "1")
			t.Setenv("NIMOTE_CODEX_PROTOCOL_VARIANT", variant)
			m := newTestManager(t)
			timeout := 10 * time.Second
			if variant == "timeout" {
				timeout = 200 * time.Millisecond
			}
			s, err := m.startAuto(StartRequest{Kind: "agent", Agent: "codex", Interaction: "auto", Directory: dir}, timeout)
			if err != nil {
				t.Fatal(err)
			}
			if variant == "compatible" {
				if s.Interaction != "structured" || s.CompatibilityFallback != "" {
					t.Fatal(s)
				}
			} else {
				if s.Interaction != "terminal" || s.CompatibilityFallback == "" {
					t.Fatal(s)
				}
				if err = m.Input(s.ID, []byte("exit\n")); err != nil {
					t.Fatal(err)
				}
				awaitSession(t, m, s.ID)
				output, _, _ := m.Output(s.ID, 0)
				if !strings.Contains(string(output), "TERMINAL_FALLBACK_READY") || strings.Contains(string(output), `"type":"user"`) {
					t.Fatalf("bad fallback output: %s", output)
				}
			}
			page := m.List(0)
			if len(page) != 1 {
				t.Fatalf("probe left orphaned sessions: %v", page)
			}
		})
	}
}

func TestSmartApprovalsSurviveClientDetachmentAndPolicyIsDurable(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(exe, "'", "'\\''") + "' -test.run=TestCodexProtocolChild\n"
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NIMOTE_CODEX_PROTOCOL_CHILD", "1")
	t.Setenv("NIMOTE_CODEX_PROTOCOL_VARIANT", "smart-approval")
	m := newTestManager(t)
	session, err := m.Start(StartRequest{Kind: "agent", Agent: "codex", Interaction: "structured", Directory: dir, ExecutionPolicy: "smart"})
	if err != nil {
		t.Fatal(err)
	}
	await := func(fragment string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			data, _, err := m.Output(session.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), fragment) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		data, _, _ := m.Output(session.ID, 0)
		t.Fatal("missing output", fragment, string(data))
	}
	await(`"type":"ready"`)
	// There is no Agent approval call from any client. The daemon must answer.
	if err := m.AgentMessage(session.ID, AgentMessage{Action: "prompt", RequestID: "prompt", Text: "read source"}); err != nil {
		t.Fatal(err)
	}
	await("approval received without UI")
	await(`"type":"turn_completed"`)
	if err := m.AgentMessage(session.ID, AgentMessage{Action: "configure", RequestID: "policy", OptionID: executionPolicyOption, Value: "confirm"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(m.dir, session.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored Session
	if json.Unmarshal(data, &stored) != nil || stored.ExecutionPolicy != "confirm" {
		t.Fatal("policy not persisted", string(data))
	}
	snapshot, err := m.AgentSnapshot(session.ID)
	if err != nil || len(snapshot["approvals"].([]any)) != 0 {
		t.Fatal(snapshot, err)
	}
}
