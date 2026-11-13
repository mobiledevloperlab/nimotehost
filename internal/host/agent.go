// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"slices"
	"sync"
)

// AgentMessage is a semantic operation, never terminal input or arbitrary RPC.
type AgentMessage struct {
	Action     string `json:"action"`
	RequestID  string `json:"request_id,omitempty"`
	Text       string `json:"text,omitempty"`
	ApprovalID string `json:"approval_id,omitempty"`
	Decision   string `json:"decision,omitempty"`
	OptionID   string `json:"option_id,omitempty"`
	Value      string `json:"value,omitempty"`
}

type structuredAgent struct {
	controls           agentControls
	models             []codexModel
	loadingModels      bool
	modelPage          []codexModel
	model, effort      string
	configuring        bool
	compacting         bool
	pendingConfig      *AgentMessage
	stopProcess        func() error
	failure            error
	onConversationID   func(string)
	resumeConversation string
	provider           string
	acp                *acpState
	activeItems        map[string]map[string]any
	startup            chan error
	startupOnce        sync.Once
	approvalDetails    map[string]map[string]any
	diagnostics        agentDiagnostics
	rpc                agentRPCState
	done               chan struct{}
	inputClosed        bool
	mu                 sync.Mutex
	writeMu            sync.Mutex
	output             io.Writer
	input              io.WriteCloser
	cwd, thread, turn  string
	busy, closed       bool
	next               int
	seen               map[string]bool
	pending            map[string]string
	approvals          map[string]json.RawMessage
}

func (a *structuredAgent) emit(kind string, data any) {
	b, _ := json.Marshal(map[string]any{"type": kind, "data": data})
	_, _ = a.output.Write(append(b, '\n'))
}

// Queue bounded frames instead of blocking on subprocess stdin while holding
// protocol state. Agents may write notifications before reading another request.
func (a *structuredAgent) send(v map[string]any) error {
	return a.sendRPCBatch([]map[string]any{v})
}
func (a *structuredAgent) initialize() error {
	if a.acp != nil {
		return a.initializeACP()
	}
	return a.send(map[string]any{"id": "host-init", "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "nimote_host", "version": Version}}})
}
func (a *structuredAgent) close() {
	a.mu.Lock()
	a.closeLocked()
	a.mu.Unlock()
}

// State and its final events change together, including on abnormal exit.
func (a *structuredAgent) closeLocked() {
	if a.closed {
		return
	}
	a.closed = true
	a.completeStartup(errors.New("agent stopped before initialization completed"))
	for id := range a.approvals {
		a.emit("approval_resolved", id)
	}
	if a.busy {
		status := "interrupted"
		if a.failure != nil {
			status = "failed"
		}
		a.emit("turn_completed", map[string]string{"id": a.turn, "status": status})
	}
	a.busy = false
	a.configuring = false
	a.compacting = false
	a.pendingConfig = nil
	a.turn = ""
	clear(a.pending)
	clear(a.approvals)
	clear(a.approvalDetails)
	clear(a.activeItems)
	if a.rpc.ready != nil {
		a.rpc.readyOnce.Do(func() { close(a.rpc.ready) })
	}
	if a.rpc.cancel != nil {
		a.rpc.cancel()
	}
	if a.rpc.reader != nil {
		_ = a.rpc.reader.Close()
	}
	if a.rpc.writer != nil {
		_ = a.rpc.writer.Close()
	}
	if a.acp != nil {
		a.acp.canceling = false
		clear(a.acp.options)
		clear(a.acp.tools)
	}
	a.writeMu.Lock()
	if !a.inputClosed {
		close(a.done)
		a.inputClosed = true
	}
	if a.input != nil {
		_ = a.input.Close()
	}
	a.writeMu.Unlock()
}

func (a *structuredAgent) fail(err error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.failure = err
	a.completeStartup(err)
	a.emit("error", err.Error())
	a.closeLocked()
	a.mu.Unlock()
	if a.stopProcess != nil {
		_ = a.stopProcess()
	}
}

func (m *Manager) AgentMessage(id string, req AgentMessage) error {
	m.mu.Lock()
	item, ok := m.sessions[id]
	var a *structuredAgent
	if ok && item.process != nil {
		a = item.agent
	}
	m.mu.Unlock()
	if !ok {
		return os.ErrNotExist
	}
	if a == nil {
		return errors.New("no active structured agent")
	}
	return a.message(req)
}

func (m *Manager) AgentSnapshot(id string) (map[string]any, error) {
	m.mu.Lock()
	item, ok := m.sessions[id]
	if ok && item.agent == nil && item.session.Interaction == "structured" {
		snapshot := map[string]any{"cursor": item.session.OutputBytes, "ready": false, "busy": false, "approvals": []any{}}
		m.mu.Unlock()
		return snapshot, nil
	}
	m.mu.Unlock()
	if !ok {
		return nil, os.ErrNotExist
	}
	if item.agent == nil {
		return nil, errors.New("structured agent state is unavailable")
	}
	a := item.agent
	a.mu.Lock()
	defer a.mu.Unlock()
	// The same lock covers state mutations and their durable events. Clients
	// apply the snapshot only at its exact byte cursor to avoid replay races.
	cursor, _ := item.output.stats()
	pending := make([]any, 0, len(a.approvals))
	for _, approval := range a.approvalDetails {
		detail := maps.Clone(approval)
		if changes, ok := detail["changes"].([]map[string]string); ok {
			copied := slices.Clone(changes)
			for i := range copied {
				copied[i] = maps.Clone(copied[i])
			}
			detail["changes"] = copied
		}
		pending = append(pending, detail)
	}
	controls := a.controls
	controls.Commands = slices.Clone(controls.Commands)
	controls.Options = slices.Clone(controls.Options)
	for i := range controls.Options {
		controls.Options[i].Values = slices.Clone(controls.Options[i].Values)
	}
	return map[string]any{"cursor": cursor, "ready": !a.closed && a.thread != "", "busy": a.busy || a.configuring, "approvals": pending, "controls": controls}, nil
}

func newStructuredAgent(provider string, output io.Writer, cwd string, resume ...string) *structuredAgent {
	a := &structuredAgent{
		activeItems:     make(map[string]map[string]any),
		startup:         make(chan error, 1),
		approvalDetails: make(map[string]map[string]any),
		rpc:             agentRPCState{queue: make(chan []map[string]any, 8), permissions: make(map[string]chan agentRPCReply)},
		done:            make(chan struct{}),
		output:          output,
		cwd:             cwd,
		seen:            make(map[string]bool),
		pending:         make(map[string]string),
		approvals:       make(map[string]json.RawMessage),
	}
	a.provider = provider
	if len(resume) > 0 {
		a.resumeConversation = resume[0]
	}
	if provider == "opencode" || provider == "claude" {
		a.acp = &acpState{tools: map[string]map[string]any{}, options: map[string]map[string]string{}}
	}
	return a
}
func supportsStructuredAgent(provider string) bool {
	return provider == "codex" || provider == "opencode" || provider == "claude"
}
