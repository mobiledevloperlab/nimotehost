// SPDX-License-Identifier: Apache-2.0
package host

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const externalHTTPTimeout = 4 * time.Second

type openCodeHTTP struct {
	connection ExternalAttachment
	client     *http.Client
	directory  string
}

func newOpenCodeHTTP(connection ExternalAttachment) *openCodeHTTP {
	return &openCodeHTTP{connection: connection, client: &http.Client{
		Transport:     &http.Transport{Proxy: nil, ResponseHeaderTimeout: externalHTTPTimeout, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c *openCodeHTTP) request(ctx context.Context, method, path string, body any, result any) error {
	if c.connection.Transport == "opencode-http-v1" {
		return c.requestV1(ctx, method, path, body, result)
	}
	return c.requestRaw(ctx, method, path, body, result)
}

func (c *openCodeHTTP) requestRaw(ctx context.Context, method, path string, body any, result any) error {
	ctx, cancel := context.WithTimeout(ctx, externalHTTPTimeout)
	defer cancel()
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.connection.Endpoint, "/")+path, bytes.NewReader(data))
	if err != nil {
		return errors.New("invalid external request")
	}
	req.Header.Set("Content-Type", "application/json")
	if c.connection.Password != "" {
		username := c.connection.Username
		if username == "" {
			username = "opencode"
		}
		req.SetBasicAuth(username, c.connection.Password)
	}
	response, err := c.client.Do(req)
	if err != nil {
		if method != http.MethodGet {
			return errRequestUncertain
		}
		return errors.New("external Agent is unreachable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// Provider error bodies can contain prompts or credentials. Do not
		// propagate them into persistent errors or the Host log.
		if response.StatusCode == 404 {
			return errors.New("external session or API is unavailable")
		}
		if response.StatusCode >= 500 && method != http.MethodGet {
			return errRequestUncertain
		}
		return fmt.Errorf("external Agent rejected request (HTTP %d)", response.StatusCode)
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		if method != http.MethodGet {
			return errRequestUncertain
		}
		return errors.New("external Agent response is incomplete or oversized")
	}
	if result != nil && json.Unmarshal(data, result) != nil {
		if method != http.MethodGet {
			return errRequestUncertain
		}
		return errors.New("external Agent protocol is incompatible")
	}
	return nil
}

type openCodeSession struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Location struct {
		Directory string `json:"directory"`
	} `json:"location"`
	Time struct {
		Created int64 `json:"created"`
		Updated int64 `json:"updated"`
	} `json:"time"`
}

func listOpenCodeSessions(ctx context.Context, connection ExternalAttachment, cwd, cursor string) (codexHistoryPage, error) {
	c := newOpenCodeHTTP(connection)
	c.directory = cwd
	defer c.client.CloseIdleConnections()
	query := url.Values{"limit": {"50"}}
	if cursor != "" {
		if len(cursor) > 4096 {
			return codexHistoryPage{}, errors.New("invalid external cursor")
		}
		query.Set("cursor", cursor)
	} else {
		query.Set("order", "desc")
	}
	if cwd != "" {
		query.Set("directory", cwd)
	}
	var page struct {
		Data   []openCodeSession `json:"data"`
		Cursor struct {
			Next *string `json:"next"`
		} `json:"cursor"`
	}
	if err := c.request(ctx, "GET", "/api/session?"+query.Encode(), nil, &page); err != nil {
		return codexHistoryPage{}, err
	}
	if page.Data == nil || len(page.Data) > maxHistoryPage {
		return codexHistoryPage{}, errors.New("external session list is incompatible")
	}
	result := codexHistoryPage{Items: []map[string]any{}, NextCursor: page.Cursor.Next}
	var active struct {
		Data map[string]struct {
			Type string `json:"type"`
		} `json:"data"`
	}
	activeErr := c.request(ctx, "GET", "/api/session/active", nil, &active)
	if result.NextCursor != nil && len(*result.NextCursor) > 4096 {
		return codexHistoryPage{}, errors.New("external cursor exceeds budget")
	}
	for _, s := range page.Data {
		if !validExternalConversationID(s.ID) {
			return codexHistoryPage{}, errors.New("invalid external session ID")
		}
		status := "unknown"
		if activeErr == nil && active.Data != nil {
			status = "idle"
			if active.Data[s.ID].Type == "running" {
				status = "running"
			}
		}
		result.Items = append(result.Items, map[string]any{"agent": "opencode", "conversation_id": s.ID, "directory": s.Location.Directory, "title": boundedHistoryText(s.Title, 4096), "updated_at": time.UnixMilli(s.Time.Updated).UTC().Format(time.RFC3339), "resumable": true, "origin": "external_live", "execution_status": status, "status_source": "external_protocol", "live_control": "conditional"})
	}
	return result, nil
}

// This handle owns a subscription, not the Agent process. Kill/Close only detach;
// interrupt is an explicit AgentMessage and never part of Host shutdown.
type openCodeAttachment struct {
	agent    *structuredAgent
	http     *openCodeHTTP
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	workers  sync.WaitGroup
	control  sync.Mutex
	hashes   map[string][32]byte
	outcomes map[string]error
	wake     chan struct{}
}

func startOpenCodeAttachment(a *structuredAgent, connection ExternalAttachment) (processHandle, error) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &openCodeAttachment{agent: a, http: newOpenCodeHTTP(connection), ctx: ctx, cancel: cancel, done: make(chan struct{}), hashes: map[string][32]byte{}, outcomes: map[string]error{}, wake: make(chan struct{}, 1)}
	p.http.directory = a.cwd
	a.openCode = p
	a.stopProcess = p.Kill
	a.thread = a.resumeConversation
	// All network I/O runs after Manager admission releases its mutex.
	p.workers.Add(2)
	go p.run()
	go p.watch()
	go func() { p.workers.Wait(); p.http.client.CloseIdleConnections(); close(p.done) }()
	return p, nil
}
func (p *openCodeAttachment) Kill() error        { p.cancel(); return nil }
func (p *openCodeAttachment) Close() error       { p.cancel(); <-p.done; return nil }
func (p *openCodeAttachment) Wait() (int, error) { <-p.done; return 0, nil }

type openCodePermission struct {
	ID        string   `json:"id"`
	SessionID string   `json:"sessionID"`
	Action    string   `json:"action"`
	Resources []string `json:"resources"`
}
type openCodeMessage struct {
	ID      string            `json:"id"`
	Type    string            `json:"type"`
	Text    string            `json:"text"`
	Content []json.RawMessage `json:"content"`
}

func (p *openCodeAttachment) snapshot() error {
	ctx, cancel := context.WithTimeout(p.ctx, externalHTTPTimeout)
	defer cancel()
	base := "/api/session/" + url.PathEscape(p.agent.resumeConversation)
	var session struct {
		Data openCodeSession `json:"data"`
	}
	if err := p.http.request(ctx, "GET", base, nil, &session); err != nil {
		return err
	}
	if session.Data.ID != p.agent.resumeConversation || !sameWorkspaceDirectory(canonicalDirectory(session.Data.Location.Directory), canonicalDirectory(p.agent.cwd)) {
		return errors.New("external session identity or workspace does not match")
	}
	var active struct {
		Data map[string]struct {
			Type string `json:"type"`
		} `json:"data"`
	}
	if err := p.http.request(ctx, "GET", "/api/session/active", nil, &active); err != nil {
		return err
	}
	if active.Data == nil {
		return errors.New("external active session protocol is incompatible")
	}
	var pending struct {
		Data []openCodePermission `json:"data"`
	}
	if err := p.http.request(ctx, "GET", base+"/permission", nil, &pending); err != nil {
		return err
	}
	if pending.Data == nil || len(pending.Data) > 64 {
		return errors.New("external approval protocol is incompatible")
	}
	for _, permission := range pending.Data {
		if permission.SessionID != session.Data.ID || !validExternalConversationID(permission.ID) {
			return errors.New("external permission belongs to another session")
		}
	}
	// Authoritative paginated messages recover every persisted change after an
	// SSE/network gap. SDK/event arrival order never drives the state machine.
	messages := []openCodeMessage{}
	totalBytes := 0
	cursor := ""
	visited := map[string]bool{}
	for {
		query := url.Values{"limit": {"50"}}
		if cursor == "" {
			query.Set("order", "asc")
		} else {
			query.Set("cursor", cursor)
		}
		var page struct {
			Data   []openCodeMessage `json:"data"`
			Cursor struct {
				Next *string `json:"next"`
			} `json:"cursor"`
		}
		if err := p.http.request(ctx, "GET", base+"/message?"+query.Encode(), nil, &page); err != nil {
			return err
		}
		pageLimit := 50
		if p.http.connection.Transport == "opencode-http-v1" {
			pageLimit = maxHistoryUpdates
		}
		if page.Data == nil || len(page.Data) > pageLimit || len(messages)+len(page.Data) > maxHistoryUpdates {
			return errors.New("external transcript exceeds snapshot budget")
		}
		for _, message := range page.Data {
			if !validExternalConversationID(message.ID) {
				return errors.New("external message ID is missing")
			}
			encoded, _ := json.Marshal(message)
			totalBytes += len(encoded)
			if totalBytes > maxACPHistoryReplayBytes {
				return errors.New("external transcript exceeds snapshot byte budget")
			}
		}
		messages = append(messages, page.Data...)
		if page.Cursor.Next == nil || *page.Cursor.Next == "" {
			break
		}
		cursor = *page.Cursor.Next
		if len(cursor) > 4096 || visited[cursor] {
			return errors.New("external transcript cursor is invalid")
		}
		visited[cursor] = true
	}
	a := p.agent
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || p.ctx.Err() != nil {
		return errors.New("external attachment closed")
	}
	initial := !a.attachmentReady
	if len(p.hashes)+len(messages) > 8192 {
		return errors.New("external message retention limit reached")
	}
	for _, message := range messages {
		if message.ID == "" {
			return errors.New("external message ID is missing")
		}
		encoded, _ := json.Marshal(message)
		hash := sha256.Sum256(encoded)
		if previous, ok := p.hashes[message.ID]; ok && previous == hash {
			continue
		}
		p.hashes[message.ID] = hash
		text := message.Text
		for _, raw := range message.Content {
			var content struct {
				Type  string `json:"type"`
				Text  string `json:"text"`
				ID    string `json:"id"`
				Name  string `json:"name"`
				State struct {
					Status  string         `json:"status"`
					Input   map[string]any `json:"input"`
					Content any            `json:"content"`
				} `json:"state"`
			}
			if json.Unmarshal(raw, &content) != nil {
				continue
			}
			if content.Type == "text" {
				text += content.Text
			}
			if content.Type == "tool" && content.ID != "" {
				a.emit("tool", boundHistoryItem(map[string]any{"id": content.ID, "kind": "tool", "title": boundedHistoryText(content.Name, 1024), "status": boundedHistoryText(content.State.Status, 128), "tool": map[string]any{"input": content.State.Input, "output": content.State.Content}, "message_id": message.ID}))
			}
		}
		if message.Type == "user" || message.Type == "assistant" {
			if !initial && message.Type == "assistant" {
				a.emit("message_completed", map[string]string{"id": message.ID, "text": boundedHistoryText(text, 16<<10)})
			} else {
				a.emit("history", []map[string]any{boundHistoryItem(map[string]any{"id": message.ID, "role": message.Type, "kind": message.Type, "text": text})})
			}
		}
	}
	nativePending := map[string]bool{}
	for _, permission := range pending.Data {
		nativePending[permission.ID] = true
		if _, ok := a.approvals[permission.ID]; ok {
			continue
		}
		raw, _ := json.Marshal(permission.ID)
		a.approvals[permission.ID] = raw
		detail := map[string]any{"id": permission.ID, "kind": "tool", "title": boundedHistoryText(permission.Action, 4096), "command": boundedHistoryText(strings.Join(permission.Resources, "\n"), 8192), "accept_allowed": true}
		a.approvalDetails[permission.ID] = detail
		a.emit("approval", detail)
	}
	for id := range a.approvals {
		if !nativePending[id] {
			delete(a.approvals, id)
			delete(a.approvalDetails, id)
			a.emit("approval_resolved", id)
		}
	}
	wasBusy := a.busy
	a.busy = active.Data[a.thread].Type == "running"
	if wasBusy != a.busy {
		if a.busy {
			a.lastTurnStatus = ""
			a.emit("turn_started", map[string]string{"id": a.thread})
		} else {
			a.lastTurnStatus = ""
			a.emit("turn_completed", map[string]string{"id": a.thread, "status": "unknown"})
		}
	}
	if a.externalDisconnected {
		a.emit("connection", map[string]string{"status": "connected"})
	}
	a.externalDisconnected = false
	a.attachmentReady = true
	if initial {
		a.controls = agentControls{Commands: []agentCommand{}, Options: []agentOption{}}
		a.emit("controls", a.controls)
		a.emit("ready", map[string]string{"thread_id": a.thread})
		a.completeStartup(nil)
	}
	return nil
}

func (p *openCodeAttachment) run() {
	defer p.workers.Done()
	first := true
	for {
		p.control.Lock()
		err := p.snapshot()
		p.control.Unlock()
		if err != nil {
			if first {
				p.agent.mu.Lock()
				p.agent.failure = err
				p.agent.mu.Unlock()
				p.agent.completeStartup(err)
				p.cancel()
				return
			}
			a := p.agent
			a.mu.Lock()
			if !a.closed && !a.externalDisconnected {
				a.externalDisconnected = true
				a.emit("connection", map[string]string{"status": "disconnected"})
			}
			a.mu.Unlock()
		}
		first = false
		select {
		case <-p.ctx.Done():
			return
		case <-p.wake:
			select {
			case <-p.ctx.Done():
				return
			case <-time.After(250 * time.Millisecond):
			}
		case <-time.After(time.Second):
		}
	}
}

// SSE only wakes snapshot reconciliation. No replay IDs or ordering guarantees
// are assumed. A periodic snapshot also recovers a silently lost SSE connection.
func (p *openCodeAttachment) watch() {
	defer p.workers.Done()
	for p.ctx.Err() == nil {
		eventPath := "/api/event"
		if p.http.connection.Transport == "opencode-http-v1" {
			eventPath = "/event?" + url.Values{"directory": {p.http.directory}}.Encode()
		}
		req, _ := http.NewRequestWithContext(p.ctx, "GET", strings.TrimRight(p.http.connection.Endpoint, "/")+eventPath, nil)
		if p.http.connection.Password != "" {
			u := p.http.connection.Username
			if u == "" {
				u = "opencode"
			}
			req.SetBasicAuth(u, p.http.connection.Password)
		}
		response, err := p.http.client.Do(req)
		if err == nil {
			if response.StatusCode == 200 {
				scanner := bufio.NewScanner(response.Body)
				scanner.Buffer(make([]byte, 4096), 2<<20)
				for scanner.Scan() {
					if bytes.HasPrefix(scanner.Bytes(), []byte("data:")) {
						select {
						case p.wake <- struct{}{}:
						default:
						}
					}
				}
			}
			response.Body.Close()
		}
		select {
		case <-p.ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (p *openCodeAttachment) message(req AgentMessage) error {
	p.control.Lock()
	defer p.control.Unlock()
	a := p.agent
	a.mu.Lock()
	if a.closed || p.ctx.Err() != nil {
		a.mu.Unlock()
		return errors.New("external attachment stopped")
	}
	if req.RequestID == "" || len(req.RequestID) > 128 {
		a.mu.Unlock()
		return errors.New("request_id is required")
	}
	if replayed, err := a.replayedMessage(req); replayed {
		a.mu.Unlock()
		if err != nil {
			return err
		}
		return p.outcomes[req.RequestID]
	}
	if len(a.seen) >= 4096 {
		a.mu.Unlock()
		return errors.New("external request retention limit reached")
	}
	a.mu.Unlock()
	// Refresh original pending IDs/status immediately before every mutation.
	if err := p.snapshot(); err != nil {
		return err
	}
	a.mu.Lock()
	var path string
	var body any
	base := "/api/session/" + url.PathEscape(a.thread)
	switch req.Action {
	case "discover":
		a.emit("controls", a.controls)
		a.rememberMessage(req)
		a.mu.Unlock()
		return nil
	case "prompt":
		if a.busy || len(a.approvals) > 0 || len(bytes.TrimSpace([]byte(req.Text))) == 0 || len(req.Text) > 256<<10 {
			a.mu.Unlock()
			return errors.New("external session is busy, awaiting approval, or prompt is invalid")
		}
		sum := sha256.Sum256([]byte(a.thread + "\x00" + req.RequestID))
		body = map[string]string{"id": "msg_" + hex.EncodeToString(sum[:]), "text": req.Text}
		path = base + "/prompt"
	case "interrupt":
		if !a.busy {
			a.mu.Unlock()
			return errors.New("no external turn is running")
		}
		path = base + "/interrupt?resume=false"
	case "approval":
		if _, ok := a.approvals[req.ApprovalID]; !ok {
			a.mu.Unlock()
			return errors.New("native approval is no longer pending")
		}
		decision := "reject"
		if req.Decision == "accept" {
			decision = "once"
		} else if req.Decision != "decline" {
			a.mu.Unlock()
			return errors.New("invalid approval decision")
		}
		path = base + "/permission/" + url.PathEscape(req.ApprovalID) + "/reply"
		body = map[string]string{"decision": decision}
	default:
		a.mu.Unlock()
		return errors.New("action is unsupported by the external Agent transport")
	}
	a.rememberMessage(req)
	p.outcomes[req.RequestID] = errRequestUncertain
	a.mu.Unlock()
	err := p.http.request(p.ctx, "POST", path, body, nil)
	p.outcomes[req.RequestID] = err
	if err == nil {
		a.mu.Lock()
		if req.Action == "prompt" {
			a.busy = true
		}
		if req.Action == "approval" {
			delete(a.approvals, req.ApprovalID)
			delete(a.approvalDetails, req.ApprovalID)
			a.emit("approval_resolved", req.ApprovalID)
		}
		a.mu.Unlock()
	}
	select {
	case p.wake <- struct{}{}:
	default:
	}
	return err
}
