// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Only the documented read command is run. In particular, no --bg, attach,
// resume, stop or daemon command may be substituted after a query failure.
// Process ownership, bounded output and cancellation reuse verification.go.
func listClaudeLiveSessions(ctx context.Context, cwd, cursor string) (codexHistoryPage, error) {
	ctx, cancel := context.WithTimeout(ctx, externalHTTPTimeout)
	defer cancel()
	args := []string{"agents", "--json"}
	if cwd != "" {
		if !filepath.IsAbs(cwd) {
			return codexHistoryPage{}, errors.New("Claude discovery requires an absolute workspace")
		}
		args = append(args, "--cwd", cwd)
	}
	cmd := exec.Command("claude", args...)
	cmd.Dir = cwdOrHome(cwd)
	cmd.Env = os.Environ()
	data, err := runEvidenceProcessBounded(ctx, cmd, nil, 2<<20)
	if err != nil {
		return codexHistoryPage{}, errors.New("Claude CLI discovery is unavailable, incompatible or timed out")
	}
	return projectClaudeLiveSessions(data, cwd, cursor)
}

func projectClaudeLiveSessions(data []byte, cwd, cursor string) (codexHistoryPage, error) {
	result := codexHistoryPage{Items: []map[string]any{}}
	var rows []struct {
		ID         string `json:"id"`
		SessionID  string `json:"sessionId"`
		Name       string `json:"name"`
		CWD        string `json:"cwd"`
		Kind       string `json:"kind"`
		StartedAt  *int64 `json:"startedAt"`
		PID        int64  `json:"pid"`
		State      string `json:"state"`
		Status     string `json:"status"`
		WaitingFor string `json:"waitingFor"`
	}
	if len(data) > 2<<20 || json.Unmarshal(data, &rows) != nil || rows == nil || len(rows) > maxHistoryUpdates {
		return result, errors.New("Claude session list is incompatible or exceeds its budget")
	}
	type entry struct {
		key  string
		item map[string]any
	}
	entries := make([]entry, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		if (row.Kind != "interactive" && row.Kind != "background") || !filepath.IsAbs(row.CWD) || strings.ContainsRune(row.CWD, '\x00') || row.StartedAt == nil || *row.StartedAt <= 0 || row.PID < 0 || row.PID > 1<<32-1 ||
			(row.SessionID != "" && !validExternalConversationID(row.SessionID)) || (row.ID != "" && !validExternalConversationID(row.ID)) || (row.Kind == "background" && row.ID == "") {
			return result, errors.New("Claude session identity is missing or incompatible")
		}
		if cwd != "" && !sameWorkspaceDirectory(canonicalDirectory(row.CWD), canonicalDirectory(cwd)) {
			continue
		}
		key := row.Kind + ":" + row.ID + ":" + row.SessionID
		if row.ID == "" && row.SessionID == "" {
			if row.PID == 0 {
				return result, errors.New("Claude interactive session has no identity")
			}
			// This is only a page boundary, never a native conversation ID.
			key = fmt.Sprintf("interactive:pid:%d:%d", row.PID, *row.StartedAt)
		}
		if seen[key] {
			return result, errors.New("Claude session list contains duplicate identities")
		}
		seen[key] = true
		status := "unknown"
		if row.PID > 0 {
			switch row.Status {
			case "busy":
				status = "running"
			case "idle":
				status = "idle"
			case "waiting":
				if row.WaitingFor == "permission prompt" || row.WaitingFor == "sandbox request" {
					status = "needs_approval"
				}
			}
		} else if row.State == "done" {
			status = "completed"
		} else if row.State == "failed" {
			status = "failed"
		}
		item := map[string]any{"agent": "claude", "conversation_id": row.SessionID, "external_session_id": row.ID, "directory": row.CWD, "title": boundedHistoryText(row.Name, 4096), "started_at": time.UnixMilli(*row.StartedAt).UTC().Format(time.RFC3339), "origin": "external_live", "execution_status": status, "status_source": "claude_agents_json", "native_kind": row.Kind, "native_state": boundedHistoryText(row.State, 128), "native_status": boundedHistoryText(row.Status, 128), "waiting_for": boundedHistoryText(row.WaitingFor, 256), "native_pid": row.PID, "resumable": false, "live_control": "unsupported"}
		entries = append(entries, entry{key, item})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
	start := 0
	if cursor != "" {
		if len(cursor) > 1024 {
			return result, errors.New("invalid Claude discovery cursor")
		}
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return result, errors.New("invalid Claude discovery cursor")
		}
		found := false
		for i, e := range entries {
			if e.key == string(decoded) {
				start, found = i+1, true
				break
			}
		}
		if !found {
			return result, errors.New("Claude discovery cursor expired; refresh the list")
		}
	}
	end := min(start+maxHistoryPage, len(entries))
	for _, e := range entries[start:end] {
		result.Items = append(result.Items, e.item)
	}
	if end < len(entries) {
		next := base64.RawURLEncoding.EncodeToString([]byte(entries[end-1].key))
		result.NextCursor = &next
	}
	describeExternalSessions(result.Items, nil)
	return result, nil
}
