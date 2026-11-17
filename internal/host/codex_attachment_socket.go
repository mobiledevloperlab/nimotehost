// SPDX-License-Identifier: Apache-2.0
package host

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/coder/websocket"
)

// JSON-RPC remains in the existing SDK. This layer only maps complete official
// WebSocket text messages to/from its newline-delimited reader/writer contract.
type codexControlSocket struct {
	connection *websocket.Conn
	ctx        context.Context
	cancel     context.CancelFunc
	remaining  []byte
}

func dialCodexControl(ctx context.Context, connection ExternalAttachment) (*codexControlSocket, error) {
	if err := connection.validate("codex"); err != nil {
		return nil, err
	}
	socketPath, release, err := validateCodexSocket(connection.Endpoint)
	if err != nil {
		return nil, err
	}
	defer release()
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	connectCtx, cancel := context.WithTimeout(ctx, externalHTTPTimeout)
	defer cancel()
	ws, _, err := websocket.Dial(connectCtx, "ws://localhost/", &websocket.DialOptions{HTTPClient: client, CompressionMode: websocket.CompressionDisabled})
	transport.CloseIdleConnections()
	if err != nil {
		return nil, errors.New("Codex control socket connection or protocol handshake failed")
	}
	ws.SetReadLimit(2 << 20)
	readCtx, stop := context.WithCancel(ctx)
	return &codexControlSocket{connection: ws, ctx: readCtx, cancel: stop}, nil
}
func (s *codexControlSocket) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(s.remaining) == 0 {
		kind, data, err := s.connection.Read(s.ctx)
		if err != nil {
			return 0, err
		}
		if kind != websocket.MessageText {
			return 0, errors.New("Codex sent a non-text protocol message")
		}
		s.remaining = append(data, '\n')
	}
	n := copy(p, s.remaining)
	s.remaining = s.remaining[n:]
	return n, nil
}
func (s *codexControlSocket) Write(p []byte) (int, error) {
	ctx, cancel := context.WithTimeout(s.ctx, externalHTTPTimeout)
	defer cancel()
	if err := s.connection.Write(ctx, websocket.MessageText, bytes.TrimSpace(p)); err != nil {
		return 0, err
	}
	return len(p), nil
}
func (s *codexControlSocket) Close() error { s.cancel(); _ = s.connection.CloseNow(); return nil }

type codexSocketAttachment struct {
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	socket *codexControlSocket
	done   chan struct{}
	err    error
}

func startCodexSocketAttachment(a *structuredAgent, connection ExternalAttachment) (processHandle, error) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &codexSocketAttachment{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	a.stopProcess = p.Kill
	a.prepareRPCTransport()
	go func() {
		defer close(p.done)
		socket, err := dialCodexControl(ctx, connection)
		if err != nil {
			p.err = err
			a.completeStartup(err)
			return
		}
		defer socket.Close()
		p.mu.Lock()
		p.socket = socket
		p.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		a.input = socket
		a.startProtocolIO()
		if err = a.initialize(); err != nil {
			p.err = err
			a.completeStartup(err)
			return
		}
		_, err = io.Copy(&agentLines{writer: a.rpc.writer, onError: a.fail, ready: a.rpc.ready}, socket)
		if ctx.Err() == nil {
			p.err = errors.New("external Codex connection was lost")
			if err != nil {
				a.completeStartup(p.err)
			}
		}
	}()
	return p, nil
}
func (p *codexSocketAttachment) Kill() error {
	p.cancel()
	p.mu.Lock()
	socket := p.socket
	p.mu.Unlock()
	if socket != nil {
		_ = socket.Close()
	}
	return nil
}
func (p *codexSocketAttachment) Close() error { _ = p.Kill(); <-p.done; return nil }
func (p *codexSocketAttachment) Wait() (int, error) {
	<-p.done
	if p.err != nil {
		return 1, p.err
	}
	return 0, nil
}
