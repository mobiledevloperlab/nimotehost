// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"

	acp "github.com/coder/acp-go-sdk"
)

// ACP providers own their session stores. The Host asks their documented ACP
// session/list and load/resume methods and never scans provider-private files.
func acpHistoryRPC(ctx context.Context, agent, cwd, method string, params any) (json.RawMessage, []map[string]any, error) {
	var path string
	var args []string
	switch agent {
	case "opencode":
		var err error
		path, err = exec.LookPath("opencode")
		if err != nil {
			return nil, nil, errors.New("OpenCode is not installed")
		}
		args = []string{"acp"}
	case "claude":
		var err error
		path, err = exec.LookPath("claude-agent-acp")
		if err != nil {
			return nil, nil, errors.New("Claude ACP adapter is not installed")
		}
	default:
		return nil, nil, errors.New("unsupported ACP history provider")
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = cwdOrHome(cwd)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	var stderr boundedBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	var conn *acp.Connection
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if conn != nil {
			<-conn.Done()
		}
	}()
	// The SDK owns request IDs, pending responses, ordered notifications and
	// disconnect/cancellation handling. Host only projects history updates.
	var updatesMu sync.Mutex
	updates := []map[string]any{}
	reader := &acpHistoryReader{Reader: out, configured: make(chan struct{})}
	conn = acp.NewConnection(func(_ context.Context, incoming string, raw json.RawMessage) (any, *acp.RequestError) {
		if incoming != "session/update" {
			return nil, acp.NewMethodNotFound(incoming)
		}
		var update map[string]any
		if json.Unmarshal(raw, &update) == nil {
			updatesMu.Lock()
			if len(updates) < 2000 {
				updates = append(updates, update)
			}
			updatesMu.Unlock()
		}
		return nil, nil
	}, in, reader)
	// SDK diagnostics may contain raw protocol records. Keep them out of Host
	// logs, and configure the logger before its receive goroutine can use it.
	conn.SetLogger(slog.New(slog.DiscardHandler))
	close(reader.configured)
	// Killing and reaping the child closes stdout and shuts down the SDK reader.
	version := 1
	if agent == "claude" {
		version = 2
	}
	initialized, err := acp.SendRequest[struct {
		ProtocolVersion int `json:"protocolVersion"`
	}](conn, ctx, "initialize", map[string]any{
		"protocolVersion": version, "clientCapabilities": map[string]any{},
		"clientInfo": map[string]string{"name": "nimote_host_history", "version": Version},
	})
	if err != nil {
		return nil, nil, err
	}
	if initialized.ProtocolVersion != version {
		return nil, nil, errors.New("unsupported ACP history protocol version")
	}
	result, err := acp.SendRequest[json.RawMessage](conn, ctx, method, params)
	if err != nil {
		return nil, nil, fmt.Errorf("ACP %s failed: %w", method, err)
	}
	updatesMu.Lock()
	snapshot := append([]map[string]any(nil), updates...)
	updatesMu.Unlock()
	return result, snapshot, nil
}

type acpHistoryReader struct {
	io.Reader
	configured chan struct{}
}

func (r *acpHistoryReader) Read(p []byte) (int, error) {
	<-r.configured
	return r.Reader.Read(p)
}

func listACPHistory(ctx context.Context, agent, cwd, cursor string) (codexHistoryPage, error) {
	params := map[string]any{"limit": maxHistoryPage}
	if cwd != "" {
		params["cwd"] = cwd
	}
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || len(raw) > 4096 {
			return codexHistoryPage{}, errors.New("invalid history cursor")
		}
		params["cursor"] = string(raw)
	}
	result, _, err := acpHistoryRPC(ctx, agent, cwd, "session/list", params)
	if err != nil {
		return codexHistoryPage{}, err
	}
	var response struct {
		Sessions   []map[string]any `json:"sessions"`
		Items      []map[string]any `json:"items"`
		NextCursor string           `json:"nextCursor"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return codexHistoryPage{}, err
	}
	rows := response.Sessions
	if rows == nil {
		rows = response.Items
	}
	page := codexHistoryPage{Items: make([]map[string]any, 0, len(rows))}
	for _, s := range rows {
		id, _ := s["sessionId"].(string)
		if id == "" {
			id, _ = s["id"].(string)
		}
		dir, _ := s["cwd"].(string)
		title, _ := s["title"].(string)
		if title == "" {
			title, _ = s["name"].(string)
		}
		updated := s["updatedAt"]
		if updated == nil {
			updated = s["updated_at"]
		}
		page.Items = append(page.Items, map[string]any{"conversation_id": id, "agent": agent, "directory": dir, "title": title, "updated_at": updated, "message_count": 0, "resumable": id != ""})
	}
	if response.NextCursor != "" {
		c := base64.RawURLEncoding.EncodeToString([]byte(response.NextCursor))
		page.NextCursor = &c
	}
	return page, nil
}

func readACPHistory(ctx context.Context, agent, id, cwd string) (map[string]any, error) {
	if id == "" || len(id) > 256 || strings.ContainsAny(id, "/\\\x00") {
		return nil, errors.New("invalid conversation id")
	}
	method := "session/load"
	if agent == "claude" {
		method = "session/resume"
	}
	_, updates, err := acpHistoryRPC(ctx, agent, cwd, method, map[string]any{"sessionId": id, "cwd": cwd, "mcpServers": []any{}})
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(updates))
	for i, p := range updates {
		update, _ := p["update"].(map[string]any)
		kind, _ := update["sessionUpdate"].(string)
		content, _ := update["content"].(map[string]any)
		text, _ := content["text"].(string)
		role := "tool"
		if strings.Contains(kind, "user_message") {
			role = "user"
		} else if strings.Contains(kind, "agent_message") {
			role = "assistant"
		}
		if text == "" {
			if title, ok := update["title"].(string); ok {
				text = title
			}
		}
		if text == "" {
			text = kind
		}
		items = append(items, map[string]any{"id": fmt.Sprintf("acp-%d", i), "role": role, "kind": kind, "text": boundedHistoryText(text, 64<<10), "tool": safeToolDetails(update)})
	}
	return map[string]any{"items": items, "next_cursor": nil}, nil
}
