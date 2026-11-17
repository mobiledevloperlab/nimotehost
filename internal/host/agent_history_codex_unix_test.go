// SPDX-License-Identifier: Apache-2.0
//go:build linux || darwin

package host

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Simulates a host whose rollout exists but has not yet been indexed and whose
// original model provider differs from the current default provider.
func TestCodexHistoryLegacyChild(t *testing.T) {
	if os.Getenv("NIMOTE_CODEX_HISTORY_CHILD") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			os.Exit(2)
		}
		switch req.Method {
		case "initialize":
			_ = encoder.Encode(map[string]any{"id": req.ID, "result": map[string]any{}})
		case "thread/list":
			rows := []map[string]any{}
			cwd := os.Getenv("NIMOTE_CODEX_HISTORY_CWD")
			providers, allProviders := req.Params["modelProviders"].([]any)
			if req.Params["useStateDbOnly"] != true && allProviders && len(providers) == 0 {
				rows = append(rows, map[string]any{"id": "legacy-conversation", "preview": "Old provider task", "cwd": cwd, "updatedAt": 1700000000})
			}
			_ = encoder.Encode(map[string]any{"id": req.ID, "result": map[string]any{"data": rows}})
		case "thread/items/list":
			_ = encoder.Encode(map[string]any{"id": req.ID, "error": map[string]any{"code": -32000, "message": "thread was deleted"}})
		}
	}
	os.Exit(0)
}
func TestCodexHistoryIncludesUnindexedOtherProviderSessions(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(exe, "'", "'\\''") + "' -test.run=TestCodexHistoryLegacyChild\n"
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("NIMOTE_CODEX_HISTORY_CHILD", "1")
	t.Setenv("NIMOTE_CODEX_HISTORY_CWD", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := listCodexHistory(ctx, dir, "")
	if err != nil || len(page.Items) != 1 || page.Items[0]["conversation_id"] != "legacy-conversation" {
		t.Fatal(page, err)
	}
	m := newTestManager(t)
	id := strings.Repeat("c", 64)
	m.sessions[id] = &managed{session: Session{ID: id, Revision: 1, Kind: "agent", Agent: "codex", Interaction: "structured", State: "running", Directory: dir, ConversationID: "legacy-conversation"}, activity: &activityJournal{}}
	api := &Server{token: "owner", manager: m}
	for _, path := range []string{"/v1/agent-history?agent=codex", "/v1/agent-history?agent=all"} {
		request := httptest.NewRequest("GET", path, nil)
		reply := httptest.NewRecorder()
		api.ServeHTTP(reply, request)
		if reply.Code != 401 {
			t.Fatal("unauthenticated history exposed", reply.Code)
		}
		reply = deviceRequest(api, "owner", "GET", path, "")
		var body codexHistoryPage
		if reply.Code != 200 || json.Unmarshal(reply.Body.Bytes(), &body) != nil || len(body.Items) != 1 {
			t.Fatal(reply.Code, reply.Body.String())
		}
		item := body.Items[0]
		if item["host_session_id"] != id || item["origin"] != "host_managed" || item["live_control"] != "conditional" || item["execution_status"] != "unknown" {
			t.Fatal("HTTP discovery metadata lost", item)
		}
	}
	if reply := deviceRequest(api, "owner", "GET", "/v1/agent-history/deleted?agent=codex", ""); reply.Code != 404 {
		t.Fatal("deleted conversation exposed", reply.Code, reply.Body.String())
	}
}
