// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const maxHistoryPage = 50
const maxHistoryFrame = 2 << 20

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
	path, err := exec.LookPath("codex")
	if err != nil {
		return nil, errors.New("Codex is not installed")
	}
	cmd := exec.CommandContext(ctx, path, "app-server")
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr boundedBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start Codex history adapter: %w", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	reader := bufio.NewScanner(stdout)
	reader.Buffer(make([]byte, 64<<10), maxHistoryFrame)
	write := func(frame any) error {
		data, err := json.Marshal(frame)
		if err != nil {
			return err
		}
		data = append(data, '\n')
		_, err = stdin.Write(data)
		return err
	}
	wait := func(id string) (json.RawMessage, error) {
		for {
			if !reader.Scan() {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				if reader.Err() == nil && stderr.String() != "" {
					return nil, fmt.Errorf("Codex app-server stopped: %s", stderr.String())
				}
				if reader.Err() != nil {
					return nil, fmt.Errorf("read Codex app-server: %w", reader.Err())
				}
				return nil, io.EOF
			}
			var frame struct {
				ID     json.RawMessage `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if json.Unmarshal(reader.Bytes(), &frame) != nil || len(frame.ID) == 0 {
				continue // Ignore valid server notifications and diagnostics.
			}
			var responseID string
			if json.Unmarshal(frame.ID, &responseID) != nil || responseID != id {
				continue
			}
			if len(frame.Error) != 0 && string(frame.Error) != "null" {
				return nil, fmt.Errorf("Codex %s failed: %s", method, frame.Error)
			}
			if len(frame.Result) == 0 || string(frame.Result) == "null" {
				return nil, errors.New("Codex app-server returned an empty result")
			}
			return frame.Result, nil
		}
	}

	if err := write(map[string]any{
		"id": "host-init", "method": "initialize",
		"params": map[string]any{
			"clientInfo":   map[string]string{"name": "nimote_host_history", "version": Version},
			"capabilities": map[string]any{},
		},
	}); err != nil {
		return nil, err
	}
	if _, err := wait("host-init"); err != nil {
		return nil, err
	}
	if err := write(map[string]any{"method": "initialized"}); err != nil {
		return nil, err
	}
	if err := write(map[string]any{"id": "host-history", "method": method, "params": params}); err != nil {
		return nil, err
	}
	return wait("host-history")
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
	var response struct {
		Data []struct {
			Item json.RawMessage `json:"item"`
		} `json:"data"`
		NextCursor string `json:"nextCursor"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return nil, err
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
		text := boundedHistoryText(itemText(item), 64<<10)
		items = append(items, map[string]any{
			"id": item["id"], "role": role, "kind": kind, "text": text,
			"tool": safeToolDetails(item),
		})
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
