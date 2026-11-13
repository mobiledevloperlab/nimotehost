// SPDX-License-Identifier: Apache-2.0
package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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
	queue              chan []byte
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

func newCodexAgent(output io.Writer, cwd string) *structuredAgent {
	return &structuredAgent{activeItems: map[string]map[string]any{}, startup: make(chan error, 1), approvalDetails: map[string]map[string]any{}, queue: make(chan []byte, 8), done: make(chan struct{}), output: output, cwd: cwd, seen: map[string]bool{}, pending: map[string]string{}, approvals: map[string]json.RawMessage{}}
}
func (a *structuredAgent) emit(kind string, data any) {
	b, _ := json.Marshal(map[string]any{"type": kind, "data": data})
	_, _ = a.output.Write(append(b, '\n'))
}

// Queue bounded frames instead of blocking on subprocess stdin while holding
// protocol state. Agents may write notifications before reading another request.
func (a *structuredAgent) send(v any) error {
	if a.acp != nil {
		if frame, ok := v.(map[string]any); ok {
			frame["jsonrpc"] = "2.0"
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return a.enqueue(append(b, '\n'))
}
func (a *structuredAgent) enqueue(data []byte) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if a.inputClosed || a.input == nil {
		return errors.New("agent input unavailable")
	}
	select {
	case a.queue <- data:
		return nil
	default:
		return errors.New("agent input queue is full")
	}
}
func (a *structuredAgent) writeLoop() {
	for {
		select {
		case <-a.done:
			return
		case data := <-a.queue:
			n, err := a.input.Write(data)
			if err == nil && n != len(data) {
				err = io.ErrShortWrite
			}
			if err != nil {
				a.fail(fmt.Errorf("agent input failed: %w", err))
				return
			}
		}
	}
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
func (a *structuredAgent) receive(line []byte) {
	if a.acp != nil {
		a.receiveACP(line)
		return
	}
	var f struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if json.Unmarshal(line, &f) != nil {
		a.completeStartup(errors.New("invalid app-server JSON"))
		a.emit("error", "Invalid app-server JSON")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	if f.Method != "" {
		if len(f.ID) > 0 {
			key := string(f.ID)
			switch f.Method {
			case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
				a.approvals[key] = append(json.RawMessage(nil), f.ID...)
				detail := approvalDetail(key, f.Method, f.Params)
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
			a.compacting = false
			a.turn = ""
			a.busy = false
			a.emit("turn_completed", map[string]string{"status": "completed"})
		}
		if f.Method == "turn/completed" {
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
	if len(f.Error) > 0 && string(f.Error) != "null" {
		if id == "host-models" {
			a.loadingModels = false
			a.modelPage = nil
		}
		if id == "host-init" || id == "host-thread" {
			a.completeStartup(fmt.Errorf("app-server handshake rejected: %s", f.Error))
		}
		if a.pending[id] == "prompt" || a.pending[id] == "compact" {
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
	case "host-init":
		if err := a.send(map[string]any{"method": "initialized"}); err != nil {
			a.emit("error", err.Error())
			return
		}
		method := "thread/start"
		params := map[string]any{"cwd": a.cwd}
		if a.resumeConversation != "" {
			method = "thread/resume"
			params["threadId"] = a.resumeConversation
		}
		if err := a.send(map[string]any{"id": "host-thread", "method": method, "params": params}); err != nil {
			a.emit("error", err.Error())
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
		a.thread = result.Thread.ID
		a.model, a.effort = result.Model, result.ReasoningEffort
		if a.thread == "" {
			a.completeStartup(errors.New("app-server did not return a thread ID"))
			a.emit("error", "app-server did not return a thread ID")
		} else {
			if a.onConversationID != nil {
				a.onConversationID(a.thread)
			}
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
	if a.acp != nil {
		return a.messageACP(req)
	}
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
	// Stop new work at the limit, but allow the active turn to be drained.
	// Keep control IDs too: a late retry must never interrupt a later turn.
	if req.Action != "interrupt" && req.Action != "approval" && len(a.seen) >= 4096 {
		return errors.New("session request limit reached; start a new session")
	}
	var frame map[string]any
	a.next++
	id := fmt.Sprintf("host-%d", a.next)
	switch req.Action {
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
		if a.thread == "" || a.busy || a.configuring {
			return errors.New("agent is not idle")
		}
		if err := a.configureCodex(req.OptionID, req.Value); err != nil {
			return err
		}
		a.seen[req.RequestID] = true
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
		frame = map[string]any{"id": rpcID, "result": map[string]string{"decision": req.Decision}}
	default:
		return errors.New("unknown agent action")
	}
	if err := a.send(frame); err != nil {
		return err
	}
	a.seen[req.RequestID] = true
	switch req.Action {
	case "discover":
		a.loadingModels = true
		a.modelPage = nil
	case "compact":
		a.compacting = true
		a.busy = true
		a.pending[id] = "compact"
		a.emit("turn_started", map[string]string{"id": id})
	case "prompt":
		a.busy = true
		a.pending[id] = "prompt"
		a.emit("user", req.Text)
	case "interrupt":
		a.pending[id] = "interrupt"
	case "approval":
		delete(a.approvals, req.ApprovalID)
		delete(a.approvalDetails, req.ApprovalID)
		a.emit("approval_resolved", req.ApprovalID)
	}
	return nil
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

// os/exec serializes calls to this writer. Limit a single protocol record so
// malformed subprocess output cannot grow host memory without bound.
type agentLines struct {
	agent  *structuredAgent
	buffer []byte
}

func (w *agentLines) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		count := len(p)
		if i >= 0 {
			count = i + 1
		}
		if len(w.buffer)+count > 2<<20 {
			return 0, errors.New("app-server frame exceeds 2 MiB")
		}
		w.buffer = append(w.buffer, p[:count]...)
		p = p[count:]
		if i >= 0 {
			if line := bytes.TrimSpace(w.buffer); len(line) > 0 {
				w.agent.receive(line)
			}
			w.buffer = nil
		}
	}
	return n, nil
}

// Stderr is diagnostic data, separate from stdout's JSON-RPC records.
type agentDiagnostics struct {
	mu   sync.Mutex
	tail []byte
}

func (d *agentDiagnostics) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := len(p)
	if len(p) > 8192 {
		p = p[len(p)-8192:]
	}
	d.tail = append(d.tail, p...)
	if len(d.tail) > 8192 {
		d.tail = append([]byte(nil), d.tail[len(d.tail)-8192:]...)
	}
	return n, nil
}
func (d *agentDiagnostics) text() string { d.mu.Lock(); defer d.mu.Unlock(); return string(d.tail) }

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
		pending = append(pending, approval)
	}
	return map[string]any{"cursor": cursor, "ready": !a.closed && a.thread != "", "busy": a.busy || a.configuring, "approvals": pending, "controls": a.controls}, nil
}

func newStructuredAgent(provider string, output io.Writer, cwd string, resume ...string) *structuredAgent {
	a := newCodexAgent(output, cwd)
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
