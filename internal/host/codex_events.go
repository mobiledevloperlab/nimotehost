// SPDX-License-Identifier: Apache-2.0
package host

import (
	"encoding/json"
	"sort"
)

// The provider protocol stops here. Host clients consume this additive event
// contract, never Codex method names, tagged unions or experimental fields.
// Unknown optional notifications/fields are ignored for forward compatibility.
func (a *structuredAgent) notification(method string, raw json.RawMessage) {
	if method == "serverRequest/resolved" {
		var p struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		if json.Unmarshal(raw, &p) == nil {
			key := string(p.RequestID)
			if _, ok := a.approvals[key]; ok {
				delete(a.approvals, key)
				delete(a.approvalDetails, key)
				a.emit("approval_resolved", key)
			}
		}
		return
	}
	switch method {
	case "turn/started", "turn/completed", "item/agentMessage/delta", "item/started", "item/completed", "error":
	default:
		return
	}

	var p struct {
		ItemID string `json:"itemId"`
		Delta  string `json:"delta"`
		Turn   struct {
			ID     string          `json:"id"`
			Status string          `json:"status"`
			Error  json.RawMessage `json:"error"`
		} `json:"turn"`
		Item struct {
			ID               string          `json:"id"`
			Type             string          `json:"type"`
			Text             string          `json:"text"`
			Status           string          `json:"status"`
			Command          string          `json:"command"`
			Cwd              string          `json:"cwd"`
			Tool             string          `json:"tool"`
			AggregatedOutput string          `json:"aggregatedOutput"`
			Result           json.RawMessage `json:"result"`
			Error            json.RawMessage `json:"error"`
			Changes          json.RawMessage `json:"changes"`
		} `json:"item"`
	}
	if json.Unmarshal(raw, &p) != nil {
		a.emit("error", "Unrecognized agent event payload")
		return
	}
	switch method {
	case "turn/started":
		a.emit("turn_started", map[string]any{"id": p.Turn.ID, "can_steer": a.canSteer()})
	case "turn/completed":
		a.emit("turn_completed", map[string]any{"id": p.Turn.ID, "status": p.Turn.Status, "error": p.Turn.Error})
	case "item/agentMessage/delta":
		a.emit("text_delta", map[string]string{"id": p.ItemID, "text": p.Delta})
	case "item/started", "item/completed":
		item := p.Item
		if item.Type == "agentMessage" {
			if method == "item/completed" {
				a.emit("message_completed", map[string]string{"id": item.ID, "text": item.Text})
			}
			return
		}
		var kind, title string
		switch item.Type {
		case "commandExecution":
			kind = "command"
			title = item.Command
		case "fileChange":
			kind = "file_change"
			title = "File changes"
		case "mcpToolCall":
			kind = "tool"
			title = item.Tool
		default:
			return
		}
		tool := map[string]any{"id": item.ID, "kind": kind, "title": title, "status": item.Status, "cwd": item.Cwd, "output": item.AggregatedOutput, "changes": normalizeChanges(item.Changes), "result": item.Result, "error": item.Error}
		if method == "item/started" && len(a.activeItems) < 128 {
			a.activeItems[item.ID] = tool
		}
		if method == "item/completed" {
			delete(a.activeItems, item.ID)
		}
		a.emit("tool", tool)
	case "error":
		a.emit("error", raw)
	}
}
func approvalDetail(id, method string, raw json.RawMessage) map[string]any {
	var p struct {
		ItemID    string          `json:"itemId"`
		GrantRoot string          `json:"grantRoot"`
		Command   string          `json:"command"`
		Cwd       string          `json:"cwd"`
		Reason    string          `json:"reason"`
		Changes   json.RawMessage `json:"changes"`
	}
	_ = json.Unmarshal(raw, &p)
	kind := "command"
	if method == "item/fileChange/requestApproval" {
		kind = "file_change"
	}
	return map[string]any{"id": id, "kind": kind, "item_id": p.ItemID, "grant_root": p.GrantRoot, "command": p.Command, "cwd": p.Cwd, "reason": p.Reason, "changes": normalizeChanges(p.Changes)}
}

// Older and newer providers may encode changes as a list or a path-keyed map.
// The Host wire contract always exposes an array with path and diff strings.
func normalizeChanges(raw json.RawMessage) []map[string]string {
	var list []struct {
		Path string `json:"path"`
		Diff string `json:"diff"`
	}
	result := make([]map[string]string, 0)
	if json.Unmarshal(raw, &list) == nil {
		for _, change := range list {
			result = append(result, map[string]string{"path": change.Path, "diff": change.Diff})
		}
		return result
	}
	var files map[string]struct {
		Diff string `json:"diff"`
	}
	if json.Unmarshal(raw, &files) == nil {
		paths := make([]string, 0, len(files))
		for path := range files {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			result = append(result, map[string]string{"path": path, "diff": files[path].Diff})
		}
	}
	return result
}
