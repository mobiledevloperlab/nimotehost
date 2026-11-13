// SPDX-License-Identifier: Apache-2.0
package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// ACP v1 is deliberately negotiated explicitly. An unsupported major version
// fails startup and auto mode selects the installed CLI's terminal interface.
type acpState struct {
	canceling bool
	version   int
	tools     map[string]map[string]any
	options   map[string]map[string]string
}

func (a *structuredAgent) initializeACP() error {
	version := 1
	if a.provider == "claude" {
		version = 2
	}
	a.acp.version = version
	return a.send(map[string]any{"id": "host-init", "method": "initialize", "params": map[string]any{"protocolVersion": version, "clientCapabilities": map[string]any{}, "clientInfo": map[string]string{"name": "nimote_host", "version": Version}}})
}

// receiveACPResult projects SDK results into Host session state. IDs here are
// local operation names; the SDK exclusively owns wire IDs and correlations.
func (a *structuredAgent) receiveACPResult(id string, payload, rpcError json.RawMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	if len(rpcError) > 0 && string(rpcError) != "null" {
		if id == "host-init" || id == "host-session" {
			a.completeStartup(fmt.Errorf("ACP handshake rejected: %s", rpcError))
		}
		if a.pending[id] == "prompt" {
			a.finishACP("failed")
			a.emit("turn_rejected", rpcError)
		}
		if a.pending[id] == "configure" {
			a.configuring = false
			a.pendingConfig = nil
		}
		delete(a.pending, id)
		a.emit("error", rpcError)
		return
	}
	switch id {
	case "host-init":
		var result struct {
			ProtocolVersion int `json:"protocolVersion"`
		}
		if json.Unmarshal(payload, &result) != nil || result.ProtocolVersion == 0 || result.ProtocolVersion != a.acp.version {
			a.completeStartup(errors.New("agent did not negotiate supported ACP v1"))
			return
		}
		method := "session/new"
		params := map[string]any{"cwd": a.cwd, "mcpServers": []any{}}
		if a.resumeConversation != "" {
			method = "session/load"
			if a.provider == "claude" {
				method = "session/resume"
			}
			params["sessionId"] = a.resumeConversation
		}
		if err := a.send(map[string]any{"id": "host-session", "method": method, "params": params}); err != nil {
			a.completeStartup(err)
		}
	case "host-session":
		var result struct {
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(payload, &result) != nil || result.SessionID == "" {
			a.completeStartup(errors.New("ACP did not return a session ID"))
			return
		}
		a.thread = result.SessionID
		a.configACP(payload)
		if a.onConversationID != nil {
			a.onConversationID(a.thread)
		}
		a.emit("ready", map[string]string{"thread_id": a.thread})
		a.completeStartup(nil)
	default:
		if a.pending[id] == "configure" {
			a.configuring = false
			if a.pendingConfig != nil {
				for i := range a.controls.Options {
					if a.controls.Options[i].ID == a.pendingConfig.OptionID {
						a.controls.Options[i].Value = a.pendingConfig.Value
					}
				}
				a.pendingConfig = nil
			}
			a.configACP(payload)
		}
		if a.pending[id] == "prompt" {
			var result struct {
				StopReason string `json:"stopReason"`
			}
			if json.Unmarshal(payload, &result) != nil || result.StopReason == "" {
				a.finishACP("failed")
				a.emit("error", "ACP prompt response did not include a stop reason")
			} else {
				a.finishACP(result.StopReason)
			}
		}
		delete(a.pending, id)
	}
}
func (a *structuredAgent) finishACP(status string) {
	a.acp.canceling = false
	a.busy = false
	a.turn = ""
	a.approvals = map[string]json.RawMessage{}
	a.approvalDetails = map[string]map[string]any{}
	a.acp.options = map[string]map[string]string{}
	a.acp.tools = map[string]map[string]any{}
	a.emit("turn_completed", map[string]string{"status": status})
}
func (a *structuredAgent) messageACP(req AgentMessage) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return errors.New("agent has stopped")
	}
	if req.RequestID == "" || len(req.RequestID) > 128 {
		return errors.New("request_id is required (maximum 128 bytes)")
	}
	if a.seen[req.RequestID] {
		return nil
	}
	if req.Action != "interrupt" && req.Action != "approval" && len(a.seen) >= 4096 {
		return errors.New("session request limit reached; start a new session")
	}
	a.next++
	id := fmt.Sprintf("host-%d", a.next)
	var frame map[string]any
	var batch []map[string]any
	switch req.Action {
	case "prompt":
		if a.thread == "" {
			return errors.New("agent is initializing")
		}
		if a.busy || a.configuring {
			return errors.New("a turn is already running")
		}
		if len(bytes.TrimSpace([]byte(req.Text))) == 0 || len(req.Text) > 256<<10 {
			return errors.New("prompt must contain 1 to 262144 bytes")
		}
		frame = map[string]any{"id": id, "method": "session/prompt", "params": map[string]any{"sessionId": a.thread, "prompt": []any{map[string]string{"type": "text", "text": req.Text}}}}
	case "discover":
		a.publishControls()
		a.seen[req.RequestID] = true
		return nil
	case "configure":
		if a.thread == "" || a.busy || a.configuring {
			return errors.New("agent is not idle")
		}
		valid := false
		for _, opt := range a.controls.Options {
			if opt.ID == req.OptionID {
				for _, v := range opt.Values {
					if v.Value == req.Value {
						valid = true
					}
				}
			}
		}
		if !valid {
			return errors.New("unsupported configuration value")
		}
		method := "session/set_config_option"
		params := map[string]any{"sessionId": a.thread, "configId": req.OptionID, "value": req.Value}
		if req.OptionID == "__model" {
			method = "session/set_model"
			params = map[string]any{"sessionId": a.thread, "modelId": req.Value}
		}
		frame = map[string]any{"id": id, "method": method, "params": params}
	case "interrupt":
		if !a.busy {
			return errors.New("no active turn to interrupt")
		}
		frame = map[string]any{"method": "session/cancel", "params": map[string]string{"sessionId": a.thread}}
		batch = append(batch, frame)
		for _, approvalID := range a.approvals {
			batch = append(batch, map[string]any{"id": approvalID, "result": map[string]any{"outcome": map[string]string{"outcome": "cancelled"}}})
		}
	case "approval":
		rpcID, ok := a.approvals[req.ApprovalID]
		if !ok {
			return errors.New("approval is no longer pending")
		}
		if req.Decision != "accept" && req.Decision != "decline" {
			return errors.New("decision must be accept or decline")
		}
		kind := "reject_once"
		if req.Decision == "accept" {
			kind = "allow_once"
		}
		option := a.acp.options[req.ApprovalID][kind]
		outcome := map[string]string{"outcome": "cancelled"}
		if option != "" {
			outcome = map[string]string{"outcome": "selected", "optionId": option}
		} else if req.Decision == "accept" {
			return errors.New("agent did not offer one-time approval; use the terminal for other permission modes")
		}
		frame = map[string]any{"id": rpcID, "result": map[string]any{"outcome": outcome}}
	default:
		return errors.New("unknown agent action")
	}
	if len(batch) > 0 {
		if err := a.sendRPCBatch(batch); err != nil {
			return err
		}
	} else if err := a.send(frame); err != nil {
		return err
	}
	a.seen[req.RequestID] = true
	switch req.Action {
	case "configure":
		a.configuring = true
		a.pendingConfig = &req
		a.pending[id] = "configure"
	case "prompt":
		a.acp.canceling = false
		a.busy = true
		a.turn = id
		a.pending[id] = "prompt"
		a.emit("user", req.Text)
		a.emit("turn_started", map[string]string{"id": id})
	case "interrupt":
		a.acp.canceling = true
		for key := range a.approvals {
			a.emit("approval_resolved", key)
		}
		a.approvals = map[string]json.RawMessage{}
		a.approvalDetails = map[string]map[string]any{}
		a.acp.options = map[string]map[string]string{}
	case "approval":
		delete(a.approvals, req.ApprovalID)
		delete(a.approvalDetails, req.ApprovalID)
		delete(a.acp.options, req.ApprovalID)
		a.emit("approval_resolved", req.ApprovalID)
	}
	return nil
}
