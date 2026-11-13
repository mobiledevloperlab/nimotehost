// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

func TestAgentRPCTransportSDKPermissionAndNotificationOrdering(t *testing.T) {
	for _, provider := range []string{"opencode", "codex"} {
		t.Run(provider, func(t *testing.T) {
			file, err := os.CreateTemp(t.TempDir(), "events")
			if err != nil {
				t.Fatal(err)
			}
			output := &outputWriter{file: file}
			defer output.Close()
			a := newStructuredAgent(provider, output, t.TempDir())
			journal := &activityJournal{path: filepath.Join(t.TempDir(), "activity")}
			item := &managed{activity: journal, output: output}
			a.onActivity = item.agentActivity
			input, inputW := io.Pipe()
			a.input = inputW
			a.prepareRPCTransport()
			defer input.Close()
			defer a.close()

			configured := &rpcConfiguredReader{Reader: input, configured: make(chan struct{})}
			var peer *acp.Connection
			permissionDone := make(chan error, 1)
			peer = acp.NewConnection(func(ctx context.Context, method string, params json.RawMessage) (any, *acp.RequestError) {
				switch method {
				case "initialize":
					return map[string]any{"protocolVersion": 1}, nil
				case "session/new":
					return map[string]any{"sessionId": "session"}, nil
				case "thread/start":
					return map[string]any{"thread": map[string]string{"id": "session"}}, nil
				case "initialized":
					return nil, nil
				case "session/prompt", "turn/start":
					notification := "session/update"
					update := map[string]any{"sessionId": "session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "before completion"}}}
					if provider == "codex" {
						notification = "item/agentMessage/delta"
						update = map[string]any{"itemId": "reply", "delta": "before completion"}
					}
					err := peer.SendNotification(ctx, notification, update)
					if err != nil {
						permissionDone <- err
						return nil, acp.NewInternalError(nil)
					}
					permissionMethod := "session/request_permission"
					permissionParams := map[string]any{"sessionId": "session", "toolCall": map[string]string{"toolCallId": "tool", "title": "Read source"}, "options": []map[string]string{{"kind": "allow_once", "optionId": "once"}}}
					if provider == "codex" {
						permissionMethod = "item/commandExecution/requestApproval"
						permissionParams = map[string]any{"command": "cat source"}
					}
					result, err := acp.SendRequest[struct {
						Decision string `json:"decision"`
						Outcome  struct {
							OptionID string `json:"optionId"`
						} `json:"outcome"`
					}](peer, ctx, permissionMethod, permissionParams)
					if err == nil && ((provider == "codex" && result.Decision != "accept") || (provider != "codex" && result.Outcome.OptionID != "once")) {
						err = io.ErrUnexpectedEOF
					}
					if provider == "codex" {
						_ = peer.SendNotification(ctx, "turn/completed", map[string]any{"turn": map[string]string{"id": "turn", "status": "completed"}})
					}
					permissionDone <- err
					return map[string]string{"stopReason": "end_turn"}, nil
				default:
					return nil, acp.NewMethodNotFound(method)
				}
			}, a.rpc.writer, configured)
			peer.SetLogger(slog.New(slog.DiscardHandler))
			close(configured.configured)
			a.startProtocolIO()
			if err := a.initialize(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-a.startup:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("SDK handshake timed out")
			}
			if err := a.message(AgentMessage{Action: "prompt", RequestID: "prompt", Text: "hello"}); err != nil {
				t.Fatal(err)
			}
			var approval string
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				a.mu.Lock()
				for id := range a.approvals {
					approval = id
				}
				a.mu.Unlock()
				if approval != "" {
					break
				}
				time.Sleep(time.Millisecond)
			}
			if approval == "" {
				t.Fatal("SDK permission request was not published")
			}
			if err := a.message(AgentMessage{Action: "approval", RequestID: "approve", ApprovalID: approval, Decision: "accept"}); err != nil {
				t.Fatal(err)
			}
			page, err := journal.page(0)
			if err != nil {
				t.Fatal(err)
			}
			requested := false
			for _, event := range page.Events {
				if event.Type == "approval_accept_requested" && event.ReferenceID == approval {
					requested = true
				}
			}
			if !requested {
				t.Fatal("SDK permission response missing from lifecycle history")
			}
			select {
			case err := <-permissionDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("SDK permission response timed out")
			}
			for time.Now().Before(deadline) {
				a.mu.Lock()
				busy := a.busy
				a.mu.Unlock()
				if !busy {
					break
				}
				time.Sleep(time.Millisecond)
			}
			data, _, _, err := output.read(0)
			if err != nil {
				t.Fatal(err)
			}
			events := string(data)
			textAt, completeAt := strings.Index(events, "before completion"), strings.Index(events, "turn_completed")
			if textAt < 0 || completeAt <= textAt {
				t.Fatal("notification did not precede completion:", events)
			}

		})
	}
}

func TestAgentSnapshotOwnsControlsAndApprovalDetails(t *testing.T) {
	m := newTestManager(t)
	file, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	output := &outputWriter{file: file}
	defer output.Close()
	a := newStructuredAgent("opencode", output, t.TempDir())
	a.thread = "session"
	a.controls = agentControls{Options: []agentOption{{ID: "model", Value: "one", Values: []agentChoice{{Value: "one"}}}}}
	a.approvalDetails["permission"] = map[string]any{"id": "permission", "changes": []map[string]string{{"path": "source.go"}}}
	m.sessions["test"] = &managed{agent: a, output: output}
	snapshot, err := m.AgentSnapshot("test")
	if err != nil {
		t.Fatal(err)
	}
	controls := snapshot["controls"].(agentControls)
	controls.Options[0].Value = "mutated"
	controls.Options[0].Values[0].Value = "mutated"
	detail := snapshot["approvals"].([]any)[0].(map[string]any)
	detail["id"] = "mutated"
	detail["changes"].([]map[string]string)[0]["path"] = "mutated"
	if a.controls.Options[0].Value != "one" || a.controls.Options[0].Values[0].Value != "one" || a.approvalDetails["permission"]["id"] != "permission" || a.approvalDetails["permission"]["changes"].([]map[string]string)[0]["path"] != "source.go" {
		t.Fatal("snapshot exposed live agent state")
	}
}

func TestAgentRPCCancellationCannotOvertakeQueuedPrompt(t *testing.T) {
	a := newStructuredAgent("opencode", io.Discard, t.TempDir())
	input, inputW := io.Pipe()
	a.input = inputW
	a.prepareRPCTransport()
	defer input.Close()
	defer a.close()
	a.thread = "session"
	// Queue both operations before the dispatcher starts. A request goroutine
	// must transmit the prompt before the dispatcher sends session/cancel.
	if err := a.message(AgentMessage{Action: "prompt", RequestID: "prompt", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := a.message(AgentMessage{Action: "interrupt", RequestID: "cancel"}); err != nil {
		t.Fatal(err)
	}
	methods := make(chan string, 2)
	go func() {
		decoder := json.NewDecoder(input)
		for range 2 {
			var frame struct {
				Method string `json:"method"`
			}
			if decoder.Decode(&frame) != nil {
				return
			}
			methods <- frame.Method
		}
	}()
	a.startProtocolIO()
	for _, want := range []string{"session/prompt", "session/cancel"} {
		select {
		case got := <-methods:
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("queued operation did not reach the peer")
		}
	}
}
