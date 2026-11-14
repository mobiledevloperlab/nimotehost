// SPDX-License-Identifier: Apache-2.0
//go:build windows

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

func TestWindowsStructuredChild(t *testing.T) {
	if os.Getenv("NIMOTE_WINDOWS_AGENT_CHILD") != "1" {
		return
	}
	acp := os.Getenv("NIMOTE_WINDOWS_AGENT_PROVIDER") != "codex"
	fmt.Fprintln(os.Stderr, "WINDOWS_STDERR_DIAGNOSTIC")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var frame struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
			Params struct {
				ProtocolVersion int `json:"protocolVersion"`
				Input           []struct {
					Text string `json:"text"`
				} `json:"input"`
				Prompt []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			} `json:"params"`
		}
		_ = json.Unmarshal(scanner.Bytes(), &frame)
		reply := func(result any) {
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "result": result})
		}
		switch frame.Method {
		case "initialize":
			if acp {
				reply(map[string]any{"protocolVersion": frame.Params.ProtocolVersion})
			} else {
				reply(map[string]any{})
			}
		case "thread/start":
			reply(map[string]any{"thread": map[string]string{"id": "windows-thread"}})
		case "session/new":
			reply(map[string]string{"sessionId": "windows-session"})
		case "turn/start":
			reply(map[string]any{"turn": map[string]string{"id": "windows-turn"}})
			_ = encoder.Encode(map[string]any{"method": "turn/started", "params": map[string]any{"turn": map[string]string{"id": "windows-turn"}}})
			_ = encoder.Encode(map[string]any{"method": "item/agentMessage/delta", "params": map[string]string{"itemId": "reply", "delta": frame.Params.Input[0].Text}})
			_ = encoder.Encode(map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]string{"id": "windows-turn"}}})
		case "session/prompt":
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "windows-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": frame.Params.Prompt[0].Text}}}})
			reply(map[string]string{"stopReason": "end_turn"})
		}
	}
	os.Exit(0)
}
func TestWindowsStructuredPipesAndJobLifecycle(t *testing.T) {
	for _, provider := range []string{"codex", "opencode", "claude"} {
		t.Run(provider, func(t *testing.T) {
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(t.TempDir(), "agent tools")
			if err = os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			// Exercise npm-style .cmd launchers and paths containing spaces.
			script := "@echo off\r\n\"" + exe + "\" -test.run=TestWindowsStructuredChild -- %*\r\n"
			for _, tool := range []string{provider, "claude-agent-acp"} {
				if err = os.WriteFile(filepath.Join(dir, tool+".cmd"), []byte(script), 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("NIMOTE_WINDOWS_AGENT_CHILD", "1")
			t.Setenv("NIMOTE_WINDOWS_AGENT_PROVIDER", provider)
			m := newTestManager(t)
			session, err := m.startAuto(StartRequest{Kind: "agent", Agent: provider, Interaction: "auto", Directory: dir}, 10*time.Second)
			if err != nil || session.Interaction != "structured" {
				t.Fatalf("%+v %v", session, err)
			}
			text := strings.Repeat("你好 Windows quote\" \n", 2000)
			if err = m.AgentMessage(session.ID, AgentMessage{Action: "prompt", RequestID: "windows-prompt", Text: text}); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(10 * time.Second)
			found := false
			var accumulated strings.Builder
			var offset int64
			for time.Now().Before(deadline) {
				page, next, err := m.Output(session.ID, offset)
				if err != nil {
					t.Fatal(err)
				}
				accumulated.Write(page)
				offset = next
				output := accumulated.String()
				if strings.Contains(string(output), `"type":"turn_completed"`) {
					if strings.Contains(string(output), "WINDOWS_STDERR_DIAGNOSTIC") || strings.Contains(string(output), "Invalid") {
						t.Fatalf("stderr leaked into protocol: %s", output)
					}
					echoed := false
					for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
						var event struct {
							Type string          `json:"type"`
							Data json.RawMessage `json:"data"`
						}
						if json.Unmarshal([]byte(line), &event) == nil && event.Type == "text_delta" {
							var delta struct {
								Text string `json:"text"`
							}
							_ = json.Unmarshal(event.Data, &delta)
							echoed = delta.Text == text
						}
					}
					if !echoed {
						t.Fatal("pipe payload lost Unicode or multiline text")
					}
					found = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !found {
				t.Fatal("structured response missing")
			}
			if err = m.Cancel(session.ID); err != nil {
				t.Fatal(err)
			}
			awaitSession(t, m, session.ID)
			if err = m.AgentMessage(session.ID, AgentMessage{Action: "prompt", RequestID: "after-stop", Text: "hello"}); err == nil {
				t.Fatal("stopped process accepted input")
			}
		})
	}
}
