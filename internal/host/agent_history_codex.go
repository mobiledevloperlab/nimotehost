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
	"time"
	"unicode/utf8"

	acp "github.com/coder/acp-go-sdk"
)

const maxHistoryPage = 50

type codexHistoryPage struct {
	Items      []map[string]any `json:"items"`
	NextCursor *string          `json:"next_cursor"`
}

type codexThreadCursor struct {
	Cursor string `json:"cursor"`
}

func encodeCodexCursor(cursor string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(cursor))
}

func decodeCodexCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(decoded) > 4096 {
		return "", errors.New("invalid history cursor")
	}
	return string(decoded), nil
}

// codexRPC starts the installed Codex app-server directly (never through a
// shell), negotiates the stable JSONL protocol, performs one bounded request,
// then stops the short-lived query process.
func codexRPC(ctx context.Context, cwd, method string, params any) (json.RawMessage, error) {
	adapter, _ := lookupAgentAdapter("codex")
	cmd := exec.CommandContext(ctx, adapter.Executable())
	if err := adapter.ConfigureCommand(cmd); err != nil {
		return nil, errors.New("Codex is not installed")
	}
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	stdin, stdout, cleanup, err := startAgentQuery(ctx, cmd)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	reader := &rpcConfiguredReader{Reader: stdout, configured: make(chan struct{})}
	conn := acp.NewConnection(func(_ context.Context, method string, _ json.RawMessage) (any, *acp.RequestError) {
		return nil, acp.NewMethodNotFound(method)
	}, codexRPCWriter{stdin}, reader)
	conn.SetLogger(slog.New(slog.DiscardHandler))
	close(reader.configured)
	if _, err := acp.SendRequest[json.RawMessage](conn, ctx, "initialize", map[string]any{
		"clientInfo": map[string]string{"name": "nimotehost_history", "version": Version}, "capabilities": map[string]any{},
	}); err != nil {
		return nil, err
	}
	if err := conn.SendNotification(ctx, "initialized", nil); err != nil {
		return nil, err
	}
	result, err := acp.SendRequest[json.RawMessage](conn, ctx, method, params)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("Codex %s failed: %w", method, err)
	}
	if len(result) == 0 || string(result) == "null" {
		return nil, errors.New("Codex app-server returned an empty result")
	}
	return result, nil
}

func listCodexHistory(ctx context.Context, cwd, cursor string) (codexHistoryPage, error) {
	nativeCursor, err := decodeCodexCursor(cursor)
	if err != nil {
		return codexHistoryPage{}, err
	}
	params := map[string]any{
		"limit":          maxHistoryPage,
		"sortKey":        "updated_at",
		"sortDirection":  "desc",
		"useStateDbOnly": false,
		"modelProviders": []string{},
	}
	if nativeCursor != "" {
		params["cursor"] = nativeCursor
	}
	if cwd != "" {
		params["cwd"] = cwd
	}
	result, err := codexRPC(ctx, cwdOrHome(cwd), "thread/list", params)
	if err != nil {
		return codexHistoryPage{}, err
	}
	var response struct {
		Data []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Preview   string `json:"preview"`
			CWD       string `json:"cwd"`
			UpdatedAt int64  `json:"updatedAt"`
			CreatedAt int64  `json:"createdAt"`
		} `json:"data"`
		NextCursor string `json:"nextCursor"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return codexHistoryPage{}, err
	}
	if len(response.Data) > maxHistoryPage || len(response.NextCursor) > 4096 {
		return codexHistoryPage{}, errors.New("Codex returned an invalid history page")
	}
	page := codexHistoryPage{Items: make([]map[string]any, 0, len(response.Data))}
	for _, thread := range response.Data {
		title := strings.TrimSpace(thread.Name)
		if title == "" {
			title = strings.TrimSpace(thread.Preview)
		}
		updated := thread.UpdatedAt
		if updated <= 0 {
			updated = thread.CreatedAt
		}
		page.Items = append(page.Items, map[string]any{
			"conversation_id": thread.ID,
			"agent":           "codex",
			"directory":       thread.CWD,
			"title":           title,
			"updated_at":      time.Unix(updated, 0).UTC().Format(time.RFC3339),
			"message_count":   0,
			"resumable":       thread.ID != "",
		})
	}
	if response.NextCursor != "" {
		cursor := encodeCodexCursor(response.NextCursor)
		page.NextCursor = &cursor
	}
	return page, nil
}

func readCodexHistory(ctx context.Context, conversationID, cursor string) (map[string]any, error) {
	if conversationID == "" || len(conversationID) > 128 || strings.ContainsAny(conversationID, "/\\\x00") {
		return nil, errors.New("invalid conversation id")
	}
	nativeCursor, err := decodeCodexCursor(cursor)
	if err != nil {
		return nil, err
	}
	params := map[string]any{"threadId": conversationID, "limit": 50, "sortDirection": "asc"}
	if nativeCursor != "" {
		params["cursor"] = nativeCursor
	}
	result, err := codexRPC(ctx, cwdOrHome(""), "thread/items/list", params)
	if err != nil {
		return nil, err
	}
	return projectCodexHistory(result)
}

// Shared by the history browser and the durable replay on session resume.
func projectCodexHistory(result json.RawMessage) (map[string]any, error) {
	var response struct {
		Data []struct {
			Item json.RawMessage `json:"item"`
		} `json:"data"`
		NextCursor string `json:"nextCursor"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return nil, err
	}
	if response.Data == nil {
		return nil, errors.New("Codex history response is missing items")
	}
	if len(response.Data) > maxHistoryPage {
		return nil, errors.New("Codex history response exceeds the requested page size")
	}
	if len(response.NextCursor) > 4096 {
		return nil, errors.New("Codex history cursor exceeds the supported size")
	}
	items := make([]map[string]any, 0)
	for _, entry := range response.Data {
		var item map[string]any
		if json.Unmarshal(entry.Item, &item) != nil {
			continue
		}
		kind, _ := item["type"].(string)
		if kind == "reasoning" {
			continue // Native reasoning is not user-visible conversation history.
		}
		role := "tool"
		if kind == "userMessage" {
			role = "user"
		} else if kind == "agentMessage" || kind == "plan" {
			role = "assistant"
		}
		items = append(items, boundHistoryItem(map[string]any{
			"id": item["id"], "role": role, "kind": kind, "text": itemText(item),
			"tool": safeToolDetails(item),
		}))
	}
	page := map[string]any{"items": items, "next_cursor": nil}
	if response.NextCursor != "" {
		page["next_cursor"] = encodeCodexCursor(response.NextCursor)
	}
	return page, nil
}

func safeToolDetails(item map[string]any) map[string]any {
	allowed := []string{"name", "tool", "command", "cwd", "status", "exitCode", "aggregatedOutput", "output", "result", "error", "changes"}
	details := make(map[string]any)
	for _, key := range allowed {
		value, ok := item[key]
		if !ok {
			continue
		}
		if key == "aggregatedOutput" || key == "output" || key == "result" || key == "error" {
			if text, ok := value.(string); ok {
				value = boundedHistoryText(text, 32<<10)
			}
		}
		if key == "changes" {
			if changes, ok := value.([]any); ok {
				for index, raw := range changes {
					if change, ok := raw.(map[string]any); ok {
						if diff, ok := change["diff"].(string); ok {
							change["diff"] = boundedHistoryText(diff, 32<<10)
						}
						changes[index] = change
					}
				}
			}
		}
		details[key] = value
	}
	return details
}

func itemText(item map[string]any) string {
	if text, ok := item["text"].(string); ok {
		return text
	}
	if content, ok := item["content"].([]any); ok {
		parts := make([]string, 0, len(content))
		for _, value := range content {
			if part, ok := value.(map[string]any); ok {
				if text, ok := part["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	for _, key := range []string{"command", "tool", "name", "summary", "review"} {
		if text, ok := item[key].(string); ok && text != "" {
			return text
		}
	}
	return ""
}

func boundedHistoryText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit] + "…"
}

func cwdOrHome(cwd string) string {
	if cwd != "" {
		return cwd
	}
	home, _ := os.UserHomeDir()
	return home
}

type boundedBuffer struct{ data []byte }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > 8192 {
		p = p[len(p)-8192:]
	}
	b.data = append(b.data, p...)
	if len(b.data) > 8192 {
		b.data = append([]byte(nil), b.data[len(b.data)-8192:]...)
	}
	return n, nil
}

func (b *boundedBuffer) String() string { return strings.TrimSpace(string(b.data)) }

// The generic SDK emits standard JSON-RPC; Codex omits its version member.
type codexRPCWriter struct{ io.Writer }

func (w codexRPCWriter) Write(data []byte) (int, error) {
	length := len(data)
	wire, err := codexWireFrame(data)
	if err != nil {
		return 0, err
	}
	n, err := w.Writer.Write(wire)
	if err == nil && n != len(wire) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return 0, err
	}
	return length, nil
}
