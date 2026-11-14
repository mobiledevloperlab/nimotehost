// SPDX-License-Identifier: Apache-2.0
//go:build linux || darwin

package host

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestACPHistorySDKChild(t *testing.T) {
	if os.Getenv("NIMOTE_ACP_HISTORY_CHILD") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
			Params struct {
				ProtocolVersion int `json:"protocolVersion"`
			} `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			os.Exit(2)
		}
		var result any
		switch req.Method {
		case "initialize":
			version := req.Params.ProtocolVersion
			if os.Getenv("NIMOTE_ACP_HISTORY_VARIANT") == "wrong-version" {
				version = 99
			}
			result = map[string]any{"protocolVersion": version}
		case "session/list":
			if os.Getenv("NIMOTE_ACP_HISTORY_VARIANT") == "timeout" {
				continue
			}
			result = map[string]any{"sessions": []map[string]any{{"sessionId": "conversation", "cwd": "/workspace", "title": "Saved task"}}}
		case "session/load", "session/resume":
			count := 3
			if os.Getenv("NIMOTE_ACP_HISTORY_VARIANT") == "large-replay" {
				count = maxHistoryUpdates + 1
			}
			for i := 0; i < count; i++ {
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "conversation", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": fmt.Sprint(i)}}}})
			}
			result = map[string]any{}
		default:
			continue
		}
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
	os.Exit(0)
}

func TestACPHistoryUsesSDKAndDrainsReplay(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(exe, "'", "'\\''") + "' -test.run=TestACPHistorySDKChild\n"
	for _, name := range []string{"opencode", "claude-agent-acp"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("NIMOTE_ACP_HISTORY_CHILD", "1")
	for _, provider := range []string{"opencode", "claude"} {
		t.Run(provider, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			page, err := listACPHistory(ctx, provider, dir, "")
			if err != nil || len(page.Items) != 1 || page.Items[0]["conversation_id"] != "conversation" {
				t.Fatal(page, err)
			}
			history, err := readACPHistory(ctx, provider, "conversation", dir)
			if err != nil {
				t.Fatal(err)
			}
			items := history["items"].([]map[string]any)
			if len(items) != 3 || items[2]["text"] != "2" {
				t.Fatal("replay lost or reordered", items)
			}
		})
	}
	t.Run("truncated-replay", func(t *testing.T) {
		t.Setenv("NIMOTE_ACP_HISTORY_VARIANT", "large-replay")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		page, err := readACPHistory(ctx, "opencode", "conversation", dir)
		if err != nil {
			t.Fatal(err)
		}
		if page["truncated"] != true || len(page["items"].([]map[string]any)) != maxHistoryUpdates {
			t.Fatal("SDK replay silently dropped updates")
		}
	})
	t.Run("wrong-version", func(t *testing.T) {
		t.Setenv("NIMOTE_ACP_HISTORY_VARIANT", "wrong-version")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := listACPHistory(ctx, "opencode", dir, ""); err == nil {
			t.Fatal("unsupported protocol accepted")
		}
	})
	t.Run("cancel", func(t *testing.T) {
		t.Setenv("NIMOTE_ACP_HISTORY_VARIANT", "timeout")
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		if _, err := listACPHistory(ctx, "opencode", dir, ""); err == nil {
			t.Fatal("canceled query succeeded")
		}
	})
}
