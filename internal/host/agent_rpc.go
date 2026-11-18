// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

type agentRPCReply struct {
	result any
	err    *acp.RequestError
}

func (a *structuredAgent) prepareRPCTransport() {
	a.rpc.reader, a.rpc.writer = io.Pipe()
	a.rpc.ready = make(chan struct{})
}

// Process creation is platform-specific; both platforms share the SDK lifecycle.
func (a *structuredAgent) startProtocolIO() {
	ctx, cancel := context.WithCancel(context.Background())
	a.rpc.cancel = cancel
	reader := &rpcConfiguredReader{Reader: a.rpc.reader, configured: make(chan struct{})}
	writer := &agentRPCInputWriter{agent: a, sent: make(chan struct{}, 1)}
	var inputReader io.Reader = reader
	if a.attachment != nil && a.provider == "codex" {
		inputReader = newCodexAttachmentReader(reader)
	}
	a.rpc.peer = acp.NewConnection(a.handleAgentRPC, writer, inputReader)
	a.rpc.peer.SetLogger(slog.New(slog.DiscardHandler))
	close(reader.configured)
	go a.writeAgentRPC(ctx, writer.sent)
	go func() {
		select {
		case <-a.rpc.peer.Done():
			a.fail(errors.New("agent protocol connection closed"))
		case <-ctx.Done():
		}
	}()
	a.rpc.readyOnce.Do(func() { close(a.rpc.ready) })
}

// Only domain operations cross this queue. JSON-RPC framing, correlation,
// notification ordering and peer cancellation belong to the upstream SDK.
func (a *structuredAgent) writeAgentRPC(ctx context.Context, sent <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case batch := <-a.rpc.queue:
			for _, frame := range batch {
				method, _ := frame["method"].(string)
				if method == "" {
					a.mu.Lock()
					key := string(frame["id"].(json.RawMessage))
					if reply := a.rpc.permissions[key]; reply != nil {
						response := agentRPCReply{result: frame["result"]}
						if raw, ok := frame["error"]; ok {
							data, _ := json.Marshal(raw)
							_ = json.Unmarshal(data, &response.err)
						}
						// A canceled SDK handler may have stopped waiting for this response.
						select {
						case reply <- response:
						default:
						}
					}
					a.mu.Unlock()
				} else if id, ok := frame["id"].(string); ok {
					// Prompts can wait for permission requests. Never block the dispatcher
					// that carries their approval responses and session/cancel notifications.
					go func() {
						result, err := acp.SendRequest[json.RawMessage](a.rpc.peer, ctx, method, frame["params"])
						var rpcError json.RawMessage
						if err != nil {
							var requestError *acp.RequestError
							if errors.As(err, &requestError) {
								rpcError, _ = json.Marshal(requestError)
							} else {
								rpcError, _ = json.Marshal(map[string]any{"message": err.Error()})
							}
						}
						if a.acp != nil {
							a.receiveACPResult(id, result, rpcError)
						} else {
							idRaw, _ := json.Marshal(id)
							a.receiveCodex(codexFrame{ID: idRaw, Result: result, Error: rpcError})
						}
					}()
					select {
					case <-sent:
					case <-ctx.Done():
						return
					}
				} else if err := a.rpc.peer.SendNotification(ctx, method, frame["params"]); err != nil {
					a.fail(fmt.Errorf("agent notification failed: %w", err))
					return
				}
			}
		}
	}
}

func (a *structuredAgent) handleAgentRPC(ctx context.Context, method string, params json.RawMessage) (any, *acp.RequestError) {
	a.mu.Lock()
	if a.closed || a.stopRequested.Load() {
		a.mu.Unlock()
		return nil, acp.NewRequestCancelled(nil)
	}
	if ctx.Err() != nil {
		a.mu.Unlock()
		return nil, acp.NewRequestCancelled(nil)
	}
	if a.acp == nil {
		if method != "item/commandExecution/requestApproval" && method != "item/fileChange/requestApproval" {
			if a.attachment != nil {
				var fields map[string]json.RawMessage
				if json.Unmarshal(params, &fields) == nil && len(fields["_nimote_native_request_id"]) > 0 {
					return a.waitForDesktopRequest(ctx, string(fields["_nimote_native_request_id"]))
				}
			}
			a.receiveCodexLocked(codexFrame{Method: method, Params: params})
			a.mu.Unlock()
			return nil, acp.NewMethodNotFound(method)
		}
	} else if method == "session/update" {
		a.updateACP(params)
		a.mu.Unlock()
		return nil, nil
	} else if method != "session/request_permission" {
		a.mu.Unlock()
		return nil, acp.NewMethodNotFound(method)
	}
	if a.acp != nil && a.acp.version == 2 {
		var err error
		params, err = normalizeV2Permission(params)
		if err != nil {
			a.mu.Unlock()
			return nil, acp.NewInvalidParams(map[string]string{"error": err.Error()})
		}
	}
	if a.attachment != nil && a.acp == nil && a.busy && a.turn == "" {
		// thread/read metadata can omit turns, and replay handlers may run
		// before the resume response consumer. The authenticated native
		// request supplies the turn only after matching the selected thread.
		var native struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
		}
		if json.Unmarshal(params, &native) == nil && native.ThreadID == a.thread && native.TurnID != "" {
			a.turn = native.TurnID
		}
	}
	// The SDK dispatches requests concurrently with its ordered notification
	// queue. Wait for an owned prompt's turn identity to be observed, rather
	// than trusting the turn ID supplied by an approval request.
	if a.acp == nil && a.attachment == nil && a.busy && a.turn == "" && a.rpc.turnReady != nil {
		var permission struct {
			Thread string `json:"threadId"`
			Turn   string `json:"turnId"`
		}
		if json.Unmarshal(params, &permission) == nil && permission.Thread == a.thread && permission.Turn != "" {
			ready := a.rpc.turnReady
			a.mu.Unlock()
			timer := time.NewTimer(5 * time.Second)
			select {
			case <-ready:
			case <-ctx.Done():
				timer.Stop()
				return nil, acp.NewRequestCancelled(nil)
			case <-a.done:
				timer.Stop()
				return nil, acp.NewRequestCancelled(nil)
			case <-timer.C:
				return nil, acp.NewRequestCancelled(nil)
			}
			timer.Stop()
			a.mu.Lock()
			if a.closed || a.stopRequested.Load() || ctx.Err() != nil {
				a.mu.Unlock()
				return nil, acp.NewRequestCancelled(nil)
			}
		}
	}
	if !a.validPermissionContext(params) {
		a.mu.Unlock()
		if a.attachment != nil && a.stopProcess != nil {
			// A stale/foreign replay must never decline another desktop task.
			// Close this subscription before the SDK can send any reply.
			_ = a.stopProcess()
			return nil, acp.NewRequestCancelled(nil)
		}
		return nil, acp.NewInvalidParams(map[string]string{"error": "permission does not belong to the active session/turn"})
	}
	if result, approved := a.automaticApproval(method, params); approved {
		a.mu.Unlock()
		return result, nil
	}
	// Local approval handles are deliberately independent of remote wire IDs.
	if len(a.rpc.permissions) >= 64 {
		a.mu.Unlock()
		if a.attachment != nil && a.stopProcess != nil {
			_ = a.stopProcess()
			return nil, acp.NewRequestCancelled(nil)
		}
		if a.acp == nil {
			return map[string]string{"decision": "decline"}, nil
		}
		return map[string]any{"outcome": map[string]string{"outcome": "cancelled"}}, nil
	}
	a.rpc.nextPermission++
	id, _ := json.Marshal(fmt.Sprintf("permission-%d", a.rpc.nextPermission))
	if a.attachment != nil && a.provider == "codex" {
		var fields map[string]json.RawMessage
		if json.Unmarshal(params, &fields) == nil && len(fields["_nimote_native_request_id"]) > 0 {
			id = fields["_nimote_native_request_id"]
		}
	}
	key := string(id)
	if a.attachment != nil && a.attachmentResolved[key] {
		a.mu.Unlock()
		return nil, acp.NewRequestCancelled(nil)
	}
	reply := make(chan agentRPCReply, 1)
	a.rpc.permissions[key] = reply
	if a.acp != nil {
		if err := a.permissionACP(id, params); err != nil {
			delete(a.rpc.permissions, key)
			a.mu.Unlock()
			return nil, acp.NewInternalError(map[string]string{"error": err.Error()})
		}
	} else {
		a.receiveCodexLocked(codexFrame{ID: id, Method: method, Params: params})
	}
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		delete(a.rpc.permissions, key)
		if _, ok := a.approvals[key]; ok {
			delete(a.approvals, key)
			delete(a.approvalDetails, key)
			if a.acp != nil {
				delete(a.acp.options, key)
			}
			a.emit("approval_resolved", key)
		}
	}()
	select {
	case response := <-reply:
		return response.result, response.err
	case <-ctx.Done():
		return nil, acp.NewRequestCancelled(nil)
	case <-a.done:
		return nil, acp.NewRequestCancelled(nil)
	}
}

// Caller holds a.mu. Unimplemented native questions stay owned by the desktop;
// sending MethodNotFound here would incorrectly fail the original Agent tool.
func (a *structuredAgent) waitForDesktopRequest(ctx context.Context, key string) (any, *acp.RequestError) {
	if a.attachmentResolved[key] {
		a.mu.Unlock()
		return nil, acp.NewRequestCancelled(nil)
	}
	if len(a.rpc.permissions) >= 64 {
		a.mu.Unlock()
		if a.stopProcess != nil {
			_ = a.stopProcess()
		}
		return nil, acp.NewRequestCancelled(nil)
	}
	reply := make(chan agentRPCReply, 1)
	a.rpc.permissions[key] = reply
	a.emit("error", "This native request is unsupported by Host; complete it on the desktop")
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.rpc.permissions, key); a.mu.Unlock() }()
	select {
	case <-reply:
	case <-ctx.Done():
	case <-a.done:
	}
	return nil, acp.NewRequestCancelled(nil)
}

// The SDK writes requests synchronously before waiting for responses. Observe
// that write so session/cancel can never overtake an accepted prompt while
// its request goroutine is being scheduled. Responses to inbound requests do
// not advance this barrier. Wire IDs remain entirely owned by the SDK.
type agentRPCInputWriter struct {
	agent *structuredAgent
	sent  chan struct{}
}

func (w *agentRPCInputWriter) Write(data []byte) (int, error) {
	originalLength := len(data)
	if w.agent.acp == nil {
		var err error
		data, err = codexWireFrame(data)
		if err != nil {
			return 0, err
		}
	}
	n, err := w.agent.input.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.agent.fail(fmt.Errorf("agent input failed: %w", err))
		return n, err
	}
	var header struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if json.Unmarshal(data, &header) == nil && len(header.ID) != 0 && header.Method != "" {
		w.sent <- struct{}{}
	}
	return originalLength, nil
}

// Codex app-server uses JSON-RPC messages without the jsonrpc member. The SDK's
// generic Connection handles correlation and dispatch; only this wire detail
// differs from ACP. All serialization uses encoding/json.
func codexWireFrame(data []byte) ([]byte, error) {
	var frame map[string]json.RawMessage
	if err := json.Unmarshal(data, &frame); err != nil {
		return nil, err
	}
	delete(frame, "jsonrpc")
	result, err := json.Marshal(frame)
	return append(result, '\n'), err
}

type agentRPCState struct {
	turnReady      chan struct{}
	ready          chan struct{}
	readyOnce      sync.Once
	queue          chan []map[string]any
	peer           *acp.Connection
	reader         *io.PipeReader
	writer         *io.PipeWriter
	cancel         context.CancelFunc
	permissions    map[string]chan agentRPCReply
	nextPermission uint64
}

// Cancellation and its permission responses are queued atomically, so queue
// pressure cannot leave the agent waiting forever on an unanswered approval.
func (a *structuredAgent) sendRPCBatch(frames []map[string]any) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if a.inputClosed || a.input == nil {
		return errors.New("agent input unavailable")
	}
	select {
	case a.rpc.queue <- frames:
		return nil
	default:
		return errors.New("agent input queue is full")
	}
}

type rpcConfiguredReader struct {
	io.Reader
	configured chan struct{}
}

func (r *rpcConfiguredReader) Read(p []byte) (int, error) {
	<-r.configured
	return r.Reader.Read(p)
}

// Caller holds a.mu. Only observed protocol state can release pending approvals.
func (a *structuredAgent) signalTurnReady() {
	if a.rpc.turnReady != nil {
		close(a.rpc.turnReady)
		a.rpc.turnReady = nil
	}
}
