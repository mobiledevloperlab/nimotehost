// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

type codexFrame struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

func (a *structuredAgent) receiveCodex(f codexFrame) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.receiveCodexLocked(f)
}

func (a *structuredAgent) receiveCodexLocked(f codexFrame) {
	if a.closed {
		return
	}
	if f.Method != "" {
		if a.attachment != nil && (f.Method == "turn/started" || f.Method == "turn/completed") {
			a.attachmentStateObserved = true
		}
		if len(f.ID) > 0 {
			key := string(f.ID)
			switch f.Method {
			case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
				a.approvals[key] = append(json.RawMessage(nil), f.ID...)
				detail := approvalDetail(key, f.Method, f.Params)
				detail["accept_allowed"] = codexDecisionAllowed(f.Params, "accept")
				if item := a.activeItems[fmt.Sprint(detail["item_id"])]; item != nil {
					if detail["command"] == "" && detail["kind"] == "command" {
						detail["command"] = item["title"]
					}
					if detail["cwd"] == "" {
						detail["cwd"] = item["cwd"]
					}
					if len(detail["changes"].([]map[string]string)) == 0 {
						detail["changes"] = item["changes"]
					}
				}
				a.approvalDetails[key] = detail
				a.emit("approval", detail)
			default:
				_ = a.send(map[string]any{"id": f.ID, "error": map[string]any{"code": -32601, "message": "This Host does not support this interactive request"}})
				a.emit("error", "Unsupported interactive request: "+f.Method)
			}
			return
		}
		if f.Method == "turn/started" {
			a.lastTurnStatus = ""
			var p struct {
				Turn struct {
					ID string `json:"id"`
				} `json:"turn"`
			}
			_ = json.Unmarshal(f.Params, &p)
			a.turn = p.Turn.ID
			a.busy = true
		}
		if f.Method == "thread/compacted" && a.compacting {
			a.lastTurnStatus = "completed"
			a.compacting = false
			a.turn = ""
			a.busy = false
			a.emit("turn_completed", map[string]string{"status": "completed"})
		}
		if f.Method == "turn/completed" {
			a.rejectPendingPermissions()
			a.lastTurnStatus = "unknown"
			var p struct {
				Turn struct {
					Status string `json:"status"`
				} `json:"turn"`
			}
			if json.Unmarshal(f.Params, &p) == nil && p.Turn.Status != "" {
				a.lastTurnStatus = p.Turn.Status
			}
			a.compacting = false
			a.busy = false
			a.turn = ""
			a.approvals = map[string]json.RawMessage{}
			a.approvalDetails = map[string]map[string]any{}
			a.activeItems = map[string]map[string]any{}
		}
		a.notification(f.Method, f.Params)
		return
	}
	var id string
	_ = json.Unmarshal(f.ID, &id)
	if req, ok := a.steers[id]; ok {
		delete(a.steers, id)
		delete(a.pending, id)
		if len(f.Error) > 0 && string(f.Error) != "null" {
			a.emit("steer_rejected", map[string]any{"request_id": req.RequestID, "text": req.Text, "error": f.Error})
		} else {
			// A completion notification may precede this acknowledgement. Never
			// revive busy state or replace the active turn on a steering reply.
			a.emit("user_followup", map[string]string{"request_id": req.RequestID, "text": req.Text, "turn_id": req.TurnID})
		}
		return
	}
	if len(f.Error) > 0 && string(f.Error) != "null" {
		if id == "host-models" {
			a.loadingModels = false
			a.modelPage = nil
		}
		if id == "host-init" || id == "host-thread" || id == "host-history" || id == "host-attach-check" {
			a.completeStartup(fmt.Errorf("app-server handshake rejected: %s", f.Error))
		}
		if a.pending[id] == "prompt" || a.pending[id] == "compact" {
			a.lastTurnStatus = "failed"
			a.compacting = false
			a.busy = false
			a.emit("turn_rejected", f.Error)
		}
		delete(a.pending, id)
		a.emit("error", f.Error)
		return
	}
	switch id {
	case "host-models":
		a.receiveModels(f.Result)
	case "host-history":
		a.receiveCodexHistory(f.Result)
	case "host-init":
		if err := a.send(map[string]any{"method": "initialized"}); err != nil {
			a.emit("error", err.Error())
			return
		}
		method := "thread/start"
		// Execution policies answer native requests; retain the provider's own
		// approval and sandbox configuration, including on resumed threads.
		params := map[string]any{"cwd": a.cwd}
		if a.resumeConversation != "" {
			method = "thread/resume"
			params["threadId"] = a.resumeConversation
		}
		if a.attachment != nil {
			// Read before subscribing: never cold-start a different saved thread
			// or override the live desktop thread's cwd or safety configuration.
			if err := a.send(map[string]any{"id": "host-attach-check", "method": "thread/read", "params": map[string]any{"threadId": a.resumeConversation, "includeTurns": false}}); err != nil {
				a.completeStartup(err)
			}
			return
		}
		if err := a.send(map[string]any{"id": "host-thread", "method": method, "params": params}); err != nil {
			a.emit("error", err.Error())
		}
	case "host-attach-check":
		if err := a.checkCodexAttachment(f.Result); err != nil {
			a.completeStartup(err)
			return
		}
		if err := a.send(map[string]any{"id": "host-thread", "method": "thread/resume", "params": map[string]any{"threadId": a.resumeConversation}}); err != nil {
			a.completeStartup(err)
		}
	case "host-thread":
		var result struct {
			Model           string `json:"model"`
			ReasoningEffort string `json:"reasoningEffort"`
			Thread          struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		_ = json.Unmarshal(f.Result, &result)
		a.model, a.effort = result.Model, result.ReasoningEffort
		if result.Thread.ID == "" {
			a.completeStartup(errors.New("app-server did not return a thread ID"))
			a.emit("error", "app-server did not return a thread ID")
		} else {
			if a.resumeConversation != "" {
				if result.Thread.ID != a.resumeConversation {
					err := errors.New("app-server resumed a different conversation")
					a.completeStartup(err)
					a.emit("error", err.Error())
					return
				}
				if a.attachment != nil {
					if err := a.checkCodexAttachment(f.Result); err != nil {
						a.completeStartup(err)
						return
					}
				}
				// Resume restores model context, but does not replay the transcript.
				// Persist history in the new Host log before accepting new turns.
				a.historyThread = result.Thread.ID
				a.historyCursors = map[string]bool{}
				a.requestCodexHistory("")
				return
			}
			a.thread = result.Thread.ID
			if a.onConversationID != nil {
				a.onConversationID(a.thread)
			}
			a.publishControls()
			a.emit("ready", map[string]string{"thread_id": a.thread})
			a.completeStartup(nil)
		}
	default:
		if a.pending[id] == "prompt" {
			var r struct {
				Turn struct {
					ID string `json:"id"`
				} `json:"turn"`
			}
			_ = json.Unmarshal(f.Result, &r)
			if r.Turn.ID != "" {
				a.turn = r.Turn.ID
			}
		}
		delete(a.pending, id)
	}
}
func (a *structuredAgent) message(req AgentMessage) error {
	if a.openCode != nil {
		return a.openCode.message(req)
	}
	if a.acp != nil {
		return a.messageACP(req)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return errors.New("agent has stopped")
	}
	if a.attachment != nil && !a.attachmentReady && req.Action != "approval" && req.Action != "interrupt" {
		return errors.New("external attachment is synchronizing")
	}
	if req.RequestID == "" || len(req.RequestID) > 128 {
		return errors.New("request_id is required (maximum 128 bytes)")
	}
	if replayed, err := a.replayedMessage(req); replayed {
		return err
	}
	// Stop new work at the limit, but allow the active turn to be drained.
	// Keep control IDs too: a late retry must never interrupt a later turn.
	if req.Action != "interrupt" && req.Action != "approval" && len(a.seen) >= 4096 {
		return errors.New("session request limit reached; start a new session")
	}
	var frame map[string]any
	a.next++
	id := fmt.Sprintf("host-%d", a.next)
	switch req.Action {
	case "steer":
		if !a.canSteer() || req.TurnID == "" || req.TurnID != a.turn {
			return errors.New("the requested turn is no longer available for steering")
		}
		if len(bytes.TrimSpace([]byte(req.Text))) == 0 || len(req.Text) > 256<<10 {
			return errors.New("prompt must contain 1 to 262144 bytes")
		}
		if len(a.steers) >= 16 {
			return errors.New("too many messages awaiting agent confirmation")
		}
		frame = map[string]any{"id": id, "method": "turn/steer", "params": map[string]any{"threadId": a.thread, "expectedTurnId": req.TurnID, "input": []any{map[string]any{"type": "text", "text": req.Text}}}}
	case "prompt":
		if a.thread == "" {
			return errors.New("agent is initializing")
		}
		if a.busy || a.configuring {
			return errors.New("a turn or configuration change is already running")
		}
		if len(bytes.TrimSpace([]byte(req.Text))) == 0 || len(req.Text) > 256<<10 {
			return errors.New("prompt must contain 1 to 262144 bytes")
		}
		params := map[string]any{"threadId": a.thread, "input": []any{map[string]any{"type": "text", "text": req.Text}}}
		if a.model != "" {
			params["model"] = a.model
		}
		if a.effort != "" {
			params["effort"] = a.effort
		}
		frame = map[string]any{"id": id, "method": "turn/start", "params": params}
	case "discover":
		if a.loadingModels {
			return nil
		}
		if a.thread == "" {
			return errors.New("agent is initializing")
		}
		frame = map[string]any{"id": "host-models", "method": "model/list", "params": map[string]any{"limit": 100}}
	case "configure":
		if req.OptionID == executionPolicyOption {
			return a.configureExecutionPolicy(req)
		}
		if a.thread == "" || a.busy || a.configuring {
			return errors.New("agent is not idle")
		}
		if err := a.configureCodex(req.OptionID, req.Value); err != nil {
			return err
		}
		a.rememberMessage(req)
		return nil
	case "compact":
		if a.thread == "" || a.busy || a.configuring {
			return errors.New("agent is not idle")
		}
		frame = map[string]any{"id": id, "method": "thread/compact/start", "params": map[string]string{"threadId": a.thread}}
	case "interrupt":
		if a.turn == "" {
			return errors.New("no active turn to interrupt")
		}
		frame = map[string]any{"id": id, "method": "turn/interrupt", "params": map[string]string{"threadId": a.thread, "turnId": a.turn}}
	case "approval":
		rpcID, ok := a.approvals[req.ApprovalID]
		if !ok {
			return errors.New("approval is no longer pending")
		}
		if req.Decision != "accept" && req.Decision != "decline" {
			return errors.New("decision must be accept or decline")
		}
		if allowed, present := a.approvalDetails[req.ApprovalID]["accept_allowed"]; req.Decision == "accept" && present && allowed != true {
			return errors.New("agent did not offer one-time approval")
		}
		frame = map[string]any{"id": rpcID, "result": map[string]string{"decision": req.Decision}}
	default:
		return errors.New("unknown agent action")
	}
	if err := a.send(frame); err != nil {
		return err
	}
	a.rememberMessage(req)
	switch req.Action {
	case "steer":
		if a.steers == nil {
			a.steers = map[string]AgentMessage{}
		}
		a.steers[id] = req
		a.pending[id] = "steer"
	case "discover":
		a.loadingModels = true
		a.modelPage = nil
	case "compact":
		a.compacting = true
		a.lastTurnStatus = ""
		a.busy = true
		a.pending[id] = "compact"
		a.emit("turn_started", map[string]string{"id": id})
	case "prompt":
		a.busy = true
		a.lastTurnStatus = ""
		a.pending[id] = "prompt"
		a.emit("user", req.Text)
	case "interrupt":
		a.pending[id] = "interrupt"
	case "approval":
		if a.onActivity != nil {
			a.onActivity("approval_"+req.Decision+"_requested", map[string]string{"id": req.ApprovalID})
		}
		delete(a.approvals, req.ApprovalID)
		delete(a.approvalDetails, req.ApprovalID)
		a.emit("approval_resolved", req.ApprovalID)
	}
	return nil
}
