// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"

	acp "github.com/coder/acp-go-sdk"
)

// Keep the SDK notification callback cheap. JSON projection runs after its
// response barrier, so a burst of history cannot fill the SDK's bounded queue
// while callbacks repeatedly marshal tool details.
const maxACPHistoryReplayBytes = 4 << 20

type acpHistoryReplay struct {
	updates   []json.RawMessage
	bytes     int
	truncated bool
}

// ACP providers own their session stores. The Host asks their documented ACP
// session/list and load/resume methods and never scans provider-private files.
func acpHistoryRPC(ctx context.Context, agent, cwd, method string, params any) (json.RawMessage, acpHistoryReplay, error) {
	adapter, ok := lookupAgentAdapter(agent)
	if !ok || adapter.Protocol() != "acp" {
		return nil, acpHistoryReplay{}, errors.New("unsupported ACP history provider")
	}
	cmd := exec.CommandContext(ctx, adapter.Executable())
	if err := adapter.ConfigureCommand(cmd); err != nil {
		return nil, acpHistoryReplay{}, err
	}
	cmd.Dir = cwdOrHome(cwd)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, acpHistoryReplay{}, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, acpHistoryReplay{}, err
	}
	var stderr boundedBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, acpHistoryReplay{}, err
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
	replay := acpHistoryReplay{}
	reader := &rpcConfiguredReader{Reader: out, configured: make(chan struct{})}
	conn = acp.NewConnection(func(_ context.Context, incoming string, raw json.RawMessage) (any, *acp.RequestError) {
		if incoming != "session/update" {
			return nil, acp.NewMethodNotFound(incoming)
		}
		updatesMu.Lock()
		if replay.truncated || len(replay.updates) >= maxHistoryUpdates || replay.bytes+len(raw) > maxACPHistoryReplayBytes {
			replay.truncated = true
		} else {
			// Params is an owned json.RawMessage allocated by the SDK decoder.
			replay.updates = append(replay.updates, raw)
			replay.bytes += len(raw)
		}
		updatesMu.Unlock()
		return nil, nil
	}, in, reader)
	// SDK diagnostics may contain raw protocol records. Keep them out of Host
	// logs, and configure the logger before its receive goroutine can use it.
	conn.SetLogger(slog.New(slog.DiscardHandler))
	close(reader.configured)
	// Killing and reaping the child closes stdout and shuts down the SDK reader.
	version := adapter.ProtocolVersion()
	initialized, err := acp.SendRequest[struct {
		ProtocolVersion int `json:"protocolVersion"`
	}](conn, ctx, "initialize", map[string]any{
		"protocolVersion": version, "clientCapabilities": map[string]any{},
		"clientInfo": map[string]string{"name": "nimotehost_history", "version": Version},
	})
	if err != nil {
		return nil, acpHistoryReplay{}, err
	}
	if initialized.ProtocolVersion != version {
		return nil, acpHistoryReplay{}, errors.New("unsupported ACP history protocol version")
	}
	result, err := acp.SendRequest[json.RawMessage](conn, ctx, method, params)
	if err != nil {
		return nil, acpHistoryReplay{}, fmt.Errorf("ACP %s failed: %w", method, err)
	}
	updatesMu.Lock()
	snapshot := replay
	updatesMu.Unlock()
	return result, snapshot, nil
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
	if len(rows) > maxHistoryPage || len(response.NextCursor) > 4096 {
		return codexHistoryPage{}, errors.New("ACP returned an invalid history page")
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
	_, replay, err := acpHistoryRPC(ctx, agent, cwd, method, map[string]any{"sessionId": id, "cwd": cwd, "mcpServers": []any{}})
	if err != nil {
		return nil, err
	}
	transcript := historyTranscript{Items: []map[string]any{}}
	for i, raw := range replay.updates {
		var update map[string]any
		if err := json.Unmarshal(raw, &update); err != nil {
			return nil, errors.New("invalid ACP history update")
		}
		transcript.append(projectACPHistoryUpdate(update, i))
		if transcript.Truncated {
			break
		}
	}
	return map[string]any{"items": transcript.Items, "next_cursor": nil, "truncated": transcript.Truncated || replay.truncated}, nil
}

func projectACPHistoryUpdate(p map[string]any, index int) map[string]any {
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
		text, _ = update["title"].(string)
	}
	if text == "" {
		text = kind
	}
	return map[string]any{"id": fmt.Sprintf("acp-%d", index), "role": role, "kind": kind, "text": text, "tool": safeToolDetails(update)}
}
