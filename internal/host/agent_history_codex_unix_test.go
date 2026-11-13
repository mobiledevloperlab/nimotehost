// SPDX-License-Identifier: Apache-2.0
//go:build linux || darwin

package host

import (
	"bufio"
	"context"
	"encoding/json"
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
			providers, allProviders := req.Params["modelProviders"].([]any)
			if req.Params["useStateDbOnly"] != true && allProviders && len(providers) == 0 {
				rows = append(rows, map[string]any{"id": "legacy-conversation", "preview": "Old provider task", "cwd": "/workspace", "updatedAt": 1700000000})
			}
			_ = encoder.Encode(map[string]any{"id": req.ID, "result": map[string]any{"data": rows}})
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := listCodexHistory(ctx, dir, "")
	if err != nil || len(page.Items) != 1 || page.Items[0]["conversation_id"] != "legacy-conversation" {
		t.Fatal(page, err)
	}
}
