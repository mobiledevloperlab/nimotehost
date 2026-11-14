// SPDX-License-Identifier: Apache-2.0
package host

import "encoding/json"

// Caller holds a.mu. Older Codex peers omit context; supplied identifiers must
// always match. A process may only ask for permission during active work.
func (a *structuredAgent) validPermissionContext(raw json.RawMessage) bool {
	if a.closed || a.thread == "" || !a.busy {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return false
	}
	if a.acp != nil {
		var session string
		return !a.acp.canceling && json.Unmarshal(fields["sessionId"], &session) == nil && session == a.thread
	}
	for _, field := range []struct{ key, expected string }{{"threadId", a.thread}, {"turnId", a.turn}} {
		if value, exists := fields[field.key]; exists {
			var id string
			if json.Unmarshal(value, &id) != nil || id == "" || id != field.expected {
				return false
			}
		}
	}
	return true
}

func codexDecisionAllowed(raw json.RawMessage, decision string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	offered, exists := fields["availableDecisions"]
	if !exists || string(offered) == "null" {
		return true
	}
	var decisions []json.RawMessage
	if json.Unmarshal(offered, &decisions) != nil {
		return false
	}
	for _, value := range decisions {
		var text string
		if json.Unmarshal(value, &text) == nil && text == decision {
			return true
		}
	}
	return false
}

// Settle native handlers before deleting their local handles. Queued accepts
// must not be delivered after the turn has ended or the process has closed.
func (a *structuredAgent) rejectPendingPermissions() {
	for key, reply := range a.rpc.permissions {
		var result any = map[string]string{"decision": "decline"}
		if a.acp != nil {
			result = map[string]any{"outcome": map[string]string{"outcome": "cancelled"}}
		}
		select {
		case reply <- agentRPCReply{result: result}:
		default:
		}
		delete(a.rpc.permissions, key)
	}
}
