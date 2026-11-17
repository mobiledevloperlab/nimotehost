// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type fixtureSessionAdapter struct {
	list func(context.Context, string, string) (codexHistoryPage, error)
}

func (a fixtureSessionAdapter) ListSessions(ctx context.Context, cwd, cursor string) (codexHistoryPage, error) {
	return a.list(ctx, cwd, cursor)
}
func (fixtureSessionAdapter) ReadSession(context.Context, string, string, string) (map[string]any, error) {
	return nil, errors.New("not supported by fixture")
}
func (fixtureSessionAdapter) SessionCapabilities() ExternalSessionCapabilities {
	return protocolSessionAdapter{agent: "codex"}.SessionCapabilities()
}

func TestExternalAdaptersSeparateDiscoveryResumeAndControl(t *testing.T) {
	for _, agent := range []string{"codex", "claude", "opencode"} {
		a, ok := lookupExternalSessionAdapter(agent)
		if !ok {
			t.Fatal(agent)
		}
		caps := a.SessionCapabilities()
		wantTransports := map[string]string{"codex": "codex-control", "claude": "", "opencode": "opencode-http-v1,opencode-http-v2"}
		if strings.Join(caps.LiveTransports, ",") != wantTransports[agent] {
			t.Fatal("incorrect external transport capability", agent, caps.LiveTransports)
		}
		if caps.Discovery != "conditional" || caps.History != "conditional" || caps.Resume != "conditional" || caps.LiveControl != "unsupported" || caps.Reason == "" {
			t.Fatal(agent, caps)
		}
		if agent != "codex" {
			if caps.HistoryMode != "native_load_replay" {
				t.Fatal("ACP replay misrepresented as passive read", caps)
			}
			if _, err := a.ReadSession(context.Background(), "saved", t.TempDir(), "cursor"); err == nil {
				t.Fatal("ACP silently discarded transcript cursor")
			}
		}
	}
	if _, ok := lookupExternalSessionAdapter("antigravity"); ok {
		t.Fatal("unimplemented adapter advertised")
	}
	t.Setenv("PATH", t.TempDir())
	for agent, info := range installedAgents() {
		if info.Installed || info.SessionCapabilities == nil || info.SessionCapabilities.Resume != "unsupported" || info.SessionCapabilities.LiveControl != "unsupported" {
			t.Fatal(agent, info)
		}
	}
}

func TestExternalDiscoveryOnlyLinksMatchingLiveHostSession(t *testing.T) {
	cwd := t.TempDir()
	base := Session{ID: strings.Repeat("a", 64), Kind: "agent", Agent: "codex", Interaction: "structured", State: "running", ConversationID: "saved", Directory: cwd, AgentStatus: "needs_approval"}
	for _, sample := range []struct {
		name   string
		edit   func(*Session)
		linked bool
	}{
		{"same native conversation", func(*Session) {}, true},
		{"other provider", func(s *Session) { s.Agent = "claude" }, false},
		{"other workspace", func(s *Session) { s.Directory = t.TempDir() }, false},
		{"other conversation", func(s *Session) { s.ConversationID = "other" }, false},
		{"terminal only", func(s *Session) { s.Interaction = "terminal" }, false},
		{"host restarted", func(s *Session) { s.State = "interrupted" }, false},
		{"completed process", func(s *Session) { s.State = "succeeded" }, false},
	} {
		t.Run(sample.name, func(t *testing.T) {
			s := base
			sample.edit(&s)
			item := map[string]any{"agent": "codex", "conversation_id": "saved", "directory": cwd, "updated_at": "2099-01-01T00:00:00Z"}
			describeExternalSessions([]map[string]any{item}, []Session{s})
			if sample.linked {
				if item["host_session_id"] != base.ID || item["execution_status"] != "needs_approval" || item["live_control"] != "supported" {
					t.Fatal(item)
				}
			} else if item["host_session_id"] != nil || item["execution_status"] != "unknown" || item["live_control"] != "unsupported" {
				t.Fatal("history fabricated control or running status", item)
			}
		})
	}
	item := map[string]any{"agent": "codex", "conversation_id": "saved", "directory": cwd}
	describeExternalSessions([]map[string]any{item}, []Session{base})
	describeExternalSessions([]map[string]any{item}, nil)
	if item["host_session_id"] != nil || item["origin"] != "external_history" {
		t.Fatal("stale Host link retained", item)
	}
}

func TestExternalAggregateRetriesFailedProviderWithoutRepeatingCompletedPage(t *testing.T) {
	ctx := context.Background()
	calls := 0
	adapters := map[string]externalSessionAdapter{
		"codex": fixtureSessionAdapter{list: func(context.Context, string, string) (codexHistoryPage, error) {
			calls++
			return codexHistoryPage{Items: []map[string]any{{"agent": "codex", "conversation_id": "saved"}}}, nil
		}},
		"claude": fixtureSessionAdapter{list: func(context.Context, string, string) (codexHistoryPage, error) {
			return codexHistoryPage{}, errors.New("network interrupted")
		}},
	}
	page, err := aggregateSessionHistory(ctx, "", "", adapters)
	if err != nil || !page.Partial || page.NextCursor == nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	adapters["claude"] = fixtureSessionAdapter{list: func(_ context.Context, _ string, cursor string) (codexHistoryPage, error) {
		if cursor != "" {
			t.Error("failed page cursor changed", cursor)
		}
		return codexHistoryPage{Items: []map[string]any{{"agent": "claude", "conversation_id": "recovered"}}}, nil
	}}
	page, err = aggregateSessionHistory(ctx, "", *page.NextCursor, adapters)
	if err != nil || page.Partial || page.NextCursor != nil || calls != 1 || len(page.Items) != 1 || page.Items[0]["conversation_id"] != "recovered" {
		t.Fatal(page, calls, err)
	}
}

func TestExternalAggregateFailureRetainsNonemptyCursorAndCancellation(t *testing.T) {
	encoded, _ := json.Marshal(map[string]string{"codex": "previous-page", "claude": "__done", "opencode": "__done"})
	cursor := base64.RawURLEncoding.EncodeToString(encoded)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	adapters := map[string]externalSessionAdapter{"codex": fixtureSessionAdapter{list: func(ctx context.Context, _, input string) (codexHistoryPage, error) {
		if input != "previous-page" {
			t.Error(input)
		}
		return codexHistoryPage{}, ctx.Err()
	}}}
	page, err := aggregateSessionHistory(ctx, "", cursor, adapters)
	if err != nil || !page.Partial || page.NextCursor == nil || *page.NextCursor != cursor {
		t.Fatal(page, err)
	}
	if _, err := aggregateSessionHistory(context.Background(), "", strings.Repeat("x", 30000), adapters); err == nil {
		t.Fatal("unbounded cursor accepted")
	}
}

func TestExternalSessionListsRejectIncompatibleResults(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `[]`, `{"data":null}`, `{"sessions":null}`, `{"data":[{"id":"../bad"}]}`, `{"sessions":[{"sessionId":"../bad"}]}`} {
		if _, err := projectCodexSessions(json.RawMessage(raw)); err == nil {
			t.Error("Codex accepted incompatible result", raw)
		}
		if _, err := projectACPSessions("opencode", json.RawMessage(raw)); err == nil {
			t.Error("ACP accepted incompatible result", raw)
		}
	}
	if p, err := projectCodexSessions(json.RawMessage(`{"data":[]}`)); err != nil || len(p.Items) != 0 {
		t.Fatal(p, err)
	}
	if p, err := projectACPSessions("claude", json.RawMessage(`{"sessions":[]}`)); err != nil || len(p.Items) != 0 {
		t.Fatal(p, err)
	}
}

func TestExternalAggregateStableOrderingAndMaximumCursorRoundtrip(t *testing.T) {
	adapters := map[string]externalSessionAdapter{}
	largeCursor := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("x", 4096)))
	for _, agent := range []string{"codex", "claude", "opencode"} {
		adapters[agent] = fixtureSessionAdapter{list: func(_ context.Context, _, cursor string) (codexHistoryPage, error) {
			if cursor != "" {
				if cursor != largeCursor {
					t.Error("cursor corrupted")
				}
				return codexHistoryPage{Items: []map[string]any{}}, nil
			}
			return codexHistoryPage{Items: []map[string]any{
				{"agent": agent, "conversation_id": "b", "updated_at": "same"},
				{"agent": agent, "conversation_id": "a", "updated_at": "same"},
			}, NextCursor: &largeCursor}, nil
		}}
	}
	page, err := aggregateSessionHistory(context.Background(), "", "", adapters)
	if err != nil || page.NextCursor == nil || len(page.Items) != 6 {
		t.Fatal(page, err)
	}
	for i, want := range []string{"claude/a", "claude/b", "codex/a", "codex/b", "opencode/a", "opencode/b"} {
		if got := fmt.Sprint(page.Items[i]["agent"], "/", page.Items[i]["conversation_id"]); got != want {
			t.Fatalf("unstable ordering: %s != %s", got, want)
		}
	}
	page, err = aggregateSessionHistory(context.Background(), "", *page.NextCursor, adapters)
	if err != nil || page.NextCursor != nil || len(page.Items) != 0 {
		t.Fatal("issued cursor cannot be consumed", page, err)
	}
}

func TestCodexResumeRejectsReplacementConversation(t *testing.T) {
	a, output := testAgent()
	a.resumeConversation = "original"
	a.receive([]byte(`{"id":"host-thread","result":{"thread":{"id":"replacement"}}}`))
	select {
	case err := <-a.startup:
		if err == nil {
			t.Fatal("replacement conversation accepted")
		}
	default:
		t.Fatal("resume failure did not settle startup")
	}
	if a.thread != "" || a.historyThread != "" || queuedAgentOperations(a) != 0 || strings.Contains(output.String(), `"type":"ready"`) {
		t.Fatal("replacement conversation became controllable", output.String())
	}
	if err := a.message(AgentMessage{Action: "prompt", RequestID: "continue", Text: "go"}); err == nil {
		t.Fatal("prompt accepted after mismatched resume")
	}
}

func TestExternalResumeConcurrentClientsReuseOneHostSession(t *testing.T) {
	m := newTestManager(t)
	cwd := t.TempDir()
	id := strings.Repeat("b", 64)
	m.sessions[id] = &managed{session: Session{ID: id, Revision: 1, Kind: "agent", Agent: "codex", Interaction: "structured", Directory: cwd, ConversationID: "native", State: "running"}, activity: &activityJournal{}}
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			s, err := m.Start(StartRequest{Kind: "agent", Agent: "codex", Interaction: "structured", Directory: cwd, Resume: &AgentResume{ConversationID: "native"}, RequestID: fmt.Sprintf("resume-%d", i)})
			if err != nil || s.ID != id {
				t.Error(s, err)
			}
		})
	}
	wg.Wait()
	if len(m.List(0)) != 1 {
		t.Fatal("duplicate native conversation started")
	}
}

func TestCodexAttachedDesktopResolutionBeforeHandlerDoesNotRecreateApproval(t *testing.T) {
	a, _ := testAgent()
	a.attachment = &ExternalAttachment{ExternalConnection: ExternalConnection{Transport: "codex-control", Endpoint: "unused"}}
	a.thread = "original"
	a.turn = "active"
	a.busy = true
	a.notification("serverRequest/resolved", json.RawMessage(`{"requestId":"native-permission"}`))
	_, err := a.handleAgentRPC(context.Background(), "item/commandExecution/requestApproval", json.RawMessage(`{"threadId":"original","turnId":"active","_nimote_native_request_id":"native-permission"}`))
	if err == nil || len(a.approvals) != 0 || len(a.rpc.permissions) != 0 {
		t.Fatal("desktop-resolved native request recreated", err)
	}
}

func TestCodexAttachedUnsupportedNativeQuestionWaitsForDesktop(t *testing.T) {
	a, _ := testAgent()
	a.attachment = &ExternalAttachment{}
	a.thread = "original"
	a.busy = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		_, _ = a.handleAgentRPC(ctx, "item/tool/requestUserInput", json.RawMessage(`{"threadId":"original","_nimote_native_request_id":"native-question"}`))
		close(done)
	}()
	waitExternalCondition(t, func() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.rpc.permissions[`"native-question"`] != nil })
	select {
	case <-done:
		t.Fatal("unsupported native question was answered by Host")
	case <-time.After(20 * time.Millisecond):
	}
	a.mu.Lock()
	a.notification("serverRequest/resolved", json.RawMessage(`{"requestId":"native-question"}`))
	a.mu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("desktop resolution did not release native question handler")
	}
}

func TestCodexAttachedPendingReplayBeforeResumeTurnSnapshot(t *testing.T) {
	a, _ := testAgent()
	a.attachment = &ExternalAttachment{}
	a.thread = "original"
	a.busy = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		_, _ = a.handleAgentRPC(ctx, "item/commandExecution/requestApproval", json.RawMessage(`{"threadId":"original","turnId":"native-active-turn","_nimote_native_request_id":42,"command":"pwd"}`))
		close(done)
	}()
	waitExternalCondition(t, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return len(a.approvals) == 1 && a.turn == "native-active-turn"
	})
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("pending replay handler leaked")
	}
}

func TestCodexAttachmentReaderDoesNotTreatNullIDAsPendingRequest(t *testing.T) {
	for _, raw := range []string{`{"method":"server/connected","params":{}}`, `{"id":null,"method":"server/connected","params":{}}`} {
		data, err := io.ReadAll(newCodexAttachmentReader(strings.NewReader(raw + "\n")))
		if err != nil || strings.Contains(string(data), "_nimote_native_request_id") {
			t.Fatal("notification converted into a blocking native request", string(data), err)
		}
	}
	if _, err := io.ReadAll(newCodexAttachmentReader(strings.NewReader(fmt.Sprintf(`{"id":%q,"method":"native/question","params":{}}`, strings.Repeat("x", 257)) + "\n"))); err == nil {
		t.Fatal("unbounded native request ID accepted")
	}
}

func TestExternalAttachmentCannotEnableAutomaticApprovals(t *testing.T) {
	a, _ := testAgent()
	a.attachment = &ExternalAttachment{}
	a.thread = "original"
	a.executionPolicy = "auto"
	if a.effectiveExecutionPolicy() != "confirm" {
		t.Fatal("external safety policy changed")
	}
	a.publishExecutionPolicy()
	for _, option := range a.controls.Options {
		if option.ID == executionPolicyOption && (option.Value != "confirm" || len(option.Values) != 1) {
			t.Fatal("unsupported external execution policy advertised", option)
		}
	}
	if err := a.configureExecutionPolicy(AgentMessage{RequestID: "unsafe-policy", Value: "auto"}); err == nil {
		t.Fatal("external auto approvals enabled after attachment")
	}
}
