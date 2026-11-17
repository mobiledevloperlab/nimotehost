// SPDX-License-Identifier: Apache-2.0
//go:build linux || darwin

package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// Uses the real installed CLI with an isolated CODEX_HOME and a local model
// protocol fixture. It never accesses user sessions or an external model API.
func TestCodexControlInstalledCLI(t *testing.T) {
	if os.Getenv("NIMOTE_TEST_CODEX_CONTROL") != "1" {
		t.Skip("set NIMOTE_TEST_CODEX_CONTROL=1 for isolated installed-CLI transport acceptance")
	}
	executable, err := exec.LookPath("codex")
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp("/private/tmp", "nc-live-")
	if err != nil {
		root, err = os.MkdirTemp("", "nc-live-")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	var contextPreserved atomic.Bool
	modelStarted := make(chan struct{})
	releaseModel := make(chan struct{})
	var releaseOnce sync.Once
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{}`)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		call := calls.Add(1)
		if call == 3 {
			contextPreserved.Store(bytes.Contains(body, []byte("isolated transport probe")))
		}
		if call == 2 {
			close(modelStarted)
			select {
			case <-releaseModel:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		item := map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]string{"type": "output_text", "text": "isolated transport verified"}}}
		for _, event := range []map[string]any{
			{"type": "response.created", "response": map[string]string{"id": "resp_fixture"}},
			{"type": "response.output_item.done", "output_index": 0, "item": item},
			{"type": "response.completed", "response": map[string]any{"id": "resp_fixture", "status": "completed", "output": []any{item}, "usage": map[string]int{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}},
		} {
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], data)
		}
	}))
	defer func() { releaseOnce.Do(func() { close(releaseModel) }); model.Close() }()
	config := fmt.Sprintf("model = \"nimote-fixture\"\nmodel_provider = \"nimote_fixture\"\n[model_providers.nimote_fixture]\nname = \"Nimote isolated fixture\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\n", model.URL)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "control.sock")
	cmd := exec.Command(executable, "app-server", "--listen", "unix://"+path)
	cmd.Env = append(os.Environ(), "CODEX_HOME="+home, "RUST_LOG=off")
	process, terminal, err := startProcess(cmd, false, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if terminal != nil {
		terminal.Close()
	}
	defer func() { process.Kill(); process.Wait(); process.Close() }()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Mode()&os.ModeSocket != 0 && info.Mode().Perm()&0077 == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	c := ExternalAttachment{ExternalConnection: ExternalConnection{Transport: "codex-control", Endpoint: path}}
	desktop, err := dialCodexControl(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer desktop.Close()
	reader := &rpcConfiguredReader{Reader: desktop, configured: make(chan struct{})}
	turnFinished := make(chan struct{}, 1)
	peer := acp.NewConnection(func(_ context.Context, method string, raw json.RawMessage) (any, *acp.RequestError) {
		if method == "turn/completed" {
			select {
			case turnFinished <- struct{}{}:
			default:
			}
		}
		return nil, acp.NewMethodNotFound(method)
	}, codexRPCWriter{desktop}, reader)
	peer.SetLogger(slog.New(slog.DiscardHandler))
	close(reader.configured)
	if _, err := acp.SendRequest[json.RawMessage](peer, ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "nimote_isolated_desktop_test", "version": "1"}}); err != nil {
		t.Fatal(err)
	}
	if err := peer.SendNotification(ctx, "initialized", nil); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	raw, err := acp.SendRequest[json.RawMessage](peer, ctx, "thread/start", map[string]any{"cwd": cwd})
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Thread.ID == "" {
		t.Fatal("native thread/start returned no thread ID")
	}
	// Empty native threads have no rollout and cannot be resumed. Execute one
	// harmless turn against our local fixed-response model to materialize it.
	if _, err := acp.SendRequest[json.RawMessage](peer, ctx, "turn/start", map[string]any{"threadId": result.Thread.ID, "input": []any{map[string]string{"type": "text", "text": "isolated transport probe"}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-turnFinished:
	case <-ctx.Done():
		t.Fatal("isolated native turn did not complete")
	}
	if _, err := acp.SendRequest[json.RawMessage](peer, ctx, "turn/start", map[string]any{"threadId": result.Thread.ID, "input": []any{map[string]string{"type": "text", "text": "keep the isolated original turn running during attachment"}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-modelStarted:
	case <-ctx.Done():
		t.Fatal("second native turn did not reach local model fixture")
	}
	m := newTestManager(t)
	session, err := m.Start(StartRequest{Kind: "agent", Agent: "codex", Interaction: "auto", Directory: cwd, RequestID: "attach-native", Resume: &AgentResume{ConversationID: result.Thread.ID}, Attach: &c})
	if err != nil {
		t.Fatal(err)
	}
	if session.ConversationID != result.Thread.ID {
		t.Fatal("native conversation was replaced")
	}
	snapshot, err := m.AgentSnapshot(session.ID)
	if err != nil || snapshot["ready"] != true || snapshot["status"] != "running" {
		t.Fatal(snapshot, err)
	}
	m.Close()
	// Releasing the local provider after Host shutdown must let the original
	// desktop turn finish; no cancel/interrupt may have been sent on detach.
	releaseOnce.Do(func() { close(releaseModel) })
	select {
	case <-turnFinished:
	case <-ctx.Done():
		t.Fatal("original desktop turn did not survive Host detach")
	}
	raw, err = acp.SendRequest[json.RawMessage](peer, ctx, "thread/read", map[string]any{"threadId": result.Thread.ID, "includeTurns": false})
	if err != nil {
		t.Fatal("Host shutdown affected the independent server", err)
	}
	var after struct {
		Thread struct {
			ID     string `json:"id"`
			Status struct {
				Type string `json:"type"`
			} `json:"status"`
		} `json:"thread"`
	}
	if json.Unmarshal(raw, &after) != nil || after.Thread.ID != result.Thread.ID || after.Thread.Status.Type != "idle" {
		t.Fatal("original desktop thread was not retained")
	}
	reconnected, err := NewManager(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reconnected.Close()
	continued, err := reconnected.Start(StartRequest{Kind: "agent", Agent: "codex", Interaction: "auto", Directory: cwd, RequestID: "reattach-native", Resume: &AgentResume{ConversationID: result.Thread.ID}, Attach: &c})
	if err != nil {
		t.Fatal(err)
	}
	if err := reconnected.AgentMessage(continued.ID, AgentMessage{Action: "prompt", RequestID: "continue-original", Text: "continue this original session from Host"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-turnFinished:
	case <-ctx.Done():
		t.Fatal("Host continuation did not complete on original desktop thread")
	}
	if continued.ConversationID != result.Thread.ID || !contextPreserved.Load() {
		t.Fatal("original native context was not retained across Host reconnect")
	}
	t.Log("installed Codex: active original turn attachment, task survival after Host detach, reconnect and Host prompt with original model context verified; no external model API called")
}
