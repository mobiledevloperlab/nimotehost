// SPDX-License-Identifier: Apache-2.0
package host

import (
	"encoding/json"
	"fmt"
	"strings"
)

type acpTool struct {
	ID      string  `json:"toolCallId"`
	Title   *string `json:"title"`
	Kind    *string `json:"kind"`
	Status  *string `json:"status"`
	Content *[]struct {
		Type    string `json:"type"`
		Path    string `json:"path"`
		OldText string `json:"oldText"`
		NewText string `json:"newText"`
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"content"`
	Locations []struct {
		Path string `json:"path"`
	} `json:"locations"`
	RawInput json.RawMessage `json:"rawInput"`
}

func boundedAgentText(s string) string {
	if len(s) > 32768 {
		return s[:32768] + "…"
	}
	return s
}
func (a *structuredAgent) toolACP(t acpTool) map[string]any {
	existing := a.acp.tools[t.ID]
	tool := map[string]any{"id": t.ID, "kind": "tool", "title": "Tool", "status": "pending", "output": "", "changes": []map[string]string{}}
	for key, value := range existing {
		tool[key] = value
	}
	if t.Title != nil {
		tool["title"] = boundedAgentText(*t.Title)
	}
	if t.Kind != nil {
		switch *t.Kind {
		case "execute":
			tool["kind"] = "command"
		case "edit", "delete", "move":
			tool["kind"] = "file_change"
		default:
			tool["kind"] = "tool"
		}
	}
	var input map[string]any
	_ = json.Unmarshal(t.RawInput, &input)
	if command, ok := input["command"].(string); ok {
		tool["title"] = boundedAgentText(command)
	}
	if t.Status != nil {
		tool["status"] = *t.Status
	}
	if t.Content != nil {
		var output strings.Builder
		changes := make([]map[string]string, 0)
		for _, content := range *t.Content {
			switch content.Type {
			case "content":
				if content.Content.Type == "text" && output.Len() < 32768 {
					output.WriteString(boundedAgentText(content.Content.Text))
					output.WriteByte('\n')
				}
			case "diff":
				if len(changes) < 64 {
					changes = append(changes, map[string]string{"path": content.Path, "diff": boundedAgentText(fmt.Sprintf("Before:\n%s\n\nAfter:\n%s", content.OldText, content.NewText))})
				}
			}
		}
		tool["output"] = boundedAgentText(output.String())
		tool["changes"] = changes
	}
	if len(a.acp.tools) < 128 || existing != nil {
		a.acp.tools[t.ID] = tool
	}
	return tool
}
func (a *structuredAgent) updateACP(raw json.RawMessage) {
	var p struct {
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if json.Unmarshal(raw, &p) != nil || (a.thread != "" && p.SessionID != a.thread) {
		return
	}
	var update struct {
		Type    string          `json:"sessionUpdate"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(p.Update, &update) != nil {
		a.emit("error", "Invalid ACP session update")
		return
	}
	if a.thread == "" && update.Type != "available_commands_update" && update.Type != "config_option_update" {
		return
	}
	switch update.Type {
	case "available_commands_update":
		var commands struct {
			Commands []agentCommand `json:"availableCommands"`
		}
		if json.Unmarshal(p.Update, &commands) == nil {
			a.controls.Commands = commands.Commands
			a.publishControls()
		}
	case "config_option_update":
		a.configACP(p.Update)
	case "agent_message_chunk":
		var content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(update.Content, &content) == nil && content.Type == "text" {
			a.emit("text_delta", map[string]string{"id": a.turn + "-assistant", "text": content.Text})
		}
	case "tool_call", "tool_call_update":
		var tool acpTool
		if json.Unmarshal(p.Update, &tool) != nil || tool.ID == "" {
			return
		}
		normalized := a.toolACP(tool)
		a.emit("tool", normalized)
		if normalized["status"] == "completed" || normalized["status"] == "failed" {
			delete(a.acp.tools, tool.ID)
		}
	}
}
func (a *structuredAgent) permissionACP(id json.RawMessage, raw json.RawMessage) {
	var p struct {
		SessionID string  `json:"sessionId"`
		Tool      acpTool `json:"toolCall"`
		Options   []struct {
			ID   string `json:"optionId"`
			Kind string `json:"kind"`
		} `json:"options"`
	}
	if json.Unmarshal(raw, &p) != nil || p.SessionID != a.thread || a.thread == "" || p.Tool.ID == "" {
		_ = a.send(map[string]any{"id": id, "error": map[string]any{"code": -32602, "message": "Invalid ACP permission request"}})
		return
	}
	if a.acp.canceling || len(a.approvals) >= 64 {
		_ = a.send(map[string]any{"id": id, "result": map[string]any{"outcome": map[string]string{"outcome": "cancelled"}}})
		return
	}
	key := string(id)
	options := map[string]string{}
	for _, option := range p.Options {
		if option.ID != "" {
			options[option.Kind] = option.ID
		}
	}
	tool := a.toolACP(p.Tool)
	changes := tool["changes"].([]map[string]string)
	if len(changes) == 0 {
		for _, location := range p.Tool.Locations {
			if len(changes) < 64 {
				changes = append(changes, map[string]string{"path": location.Path, "diff": ""})
			}
		}
	}
	detail := map[string]any{"id": key, "kind": tool["kind"], "item_id": p.Tool.ID, "command": tool["title"], "changes": changes, "reason": tool["output"]}
	a.approvals[key] = append(json.RawMessage(nil), id...)
	a.acp.options[key] = options
	a.approvalDetails[key] = detail
	a.emit("approval", detail)
}
