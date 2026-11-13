// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const Address = "127.0.0.1:47831"
const maxRequest = 1 << 20

func decodeJSON(data []byte, target any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return errors.New("expected one JSON value")
	}
	return nil
}

type Server struct {
	manager  *Manager
	token    string
	hostID   string
	shutdown func()
}

// Serve reserves the loopback listener before reading or recovering session
// state. A second daemon cannot mark the live daemon's processes interrupted.
func Serve(ctx context.Context, dir string, ready io.Writer) error {
	listener, err := net.Listen("tcp4", Address)
	if err != nil {
		return fmt.Errorf("listen on Host loopback address: %w", err)
	}
	defer listener.Close()
	if err := privateDir(dir); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "host.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := lockState(lock); err != nil {
		return fmt.Errorf("Host state is already in use: %w", err)
	}
	token, err := loadSecret(dir, "token")
	if err != nil {
		return err
	}
	hostID, err := loadSecret(dir, "host-id")
	if err != nil {
		return err
	}
	manager, err := NewManager(filepath.Join(dir, "sessions"))
	if err != nil {
		return err
	}
	defer manager.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	api := &Server{manager: manager, token: token, hostID: hostID, shutdown: cancel}
	server := &http.Server{Handler: api, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		shutdownCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
		}
	}()
	slog.Info("Nimote Host listening", "address", Address, "protocol", Protocol)
	if ready != nil {
		_, _ = fmt.Fprintf(ready, "Nimote Host %s listening on %s\n", Version, Address)
	}
	err = server.Serve(listener)
	cancel()
	<-stopped
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Browser-originated requests are never part of the SSH/CLI RPC contract.
	if r.Header.Get("Origin") != "" {
		fail(w, 403, "browser origins are not allowed")
		return
	}
	provided, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !bearer || s.token == "" || len(provided) != len(s.token) || subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) != 1 {
		fail(w, 401, "authentication required")
		return
	}
	if r.Header.Get("Nimote-Host-Protocol") != strconv.Itoa(Protocol) {
		fail(w, 426, "incompatible Host protocol")
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/capabilities" {
		respond(w, 200, capabilities(s.hostID))
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/status" {
		respond(w, 200, map[string]any{"running": true, "version": Version, "host_id": s.hostID})
		return
	}
	if r.URL.Path == "/v1/agent-history" || strings.HasPrefix(r.URL.Path, "/v1/agent-history/") {
		// History queries may legitimately outlive the normal RPC write deadline.
		err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(agentHistoryResponseTimeout))
		if err != nil && !errors.Is(err, http.ErrNotSupported) {
			fail(w, 500, "cannot extend history response deadline")
			return
		}
		s.serveAgentHistory(w, r)
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/v1/shutdown-if-idle" {
		if err := s.manager.PrepareIdleShutdown(); err != nil {
			fail(w, 409, err.Error())
			return
		}
		respond(w, 200, map[string]bool{"stopping": true})
		if s.shutdown != nil {
			s.shutdown()
		}
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/v1/shutdown" {
		respond(w, 200, map[string]bool{"stopping": true})
		if s.shutdown != nil {
			s.shutdown()
		}
		return
	}
	if r.URL.Path == "/v1/sessions" || r.URL.Path == "/v1/events" {
		switch r.Method {
		case http.MethodGet:
			var after uint64
			if text := r.URL.Query().Get("after"); text != "" {
				var err error
				after, err = strconv.ParseUint(text, 10, 64)
				if err != nil {
					fail(w, 400, "invalid revision")
					return
				}
			}
			if r.URL.Path == "/v1/events" {
				respond(w, 200, s.manager.Events(after))
			} else {
				respond(w, 200, s.manager.List(after))
			}
			return
		case http.MethodPost:
			if r.URL.Path != "/v1/sessions" {
				break
			}
			var req StartRequest
			if !readJSON(w, r, &req) {
				return
			}
			if req.Kind == "agent" && req.Interaction == "auto" {
				// The server's normal 10-second write deadline must not cut off
				// a valid cold start. Read and all other RPC deadlines stay short.
				err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(agentCreateResponseTimeout))
				if err != nil && !errors.Is(err, http.ErrNotSupported) {
					fail(w, 500, "cannot extend agent startup deadline")
					return
				}
			}
			record, err := s.manager.Start(req)
			if err != nil {
				fail(w, 400, err.Error())
				return
			}
			respond(w, 201, record)
			return
		}
		fail(w, 405, "method not allowed")
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/sessions/"), "/")
	if !strings.HasPrefix(r.URL.Path, "/v1/sessions/") || len(parts) > 2 || !validID(parts[0]) {
		fail(w, 404, "unknown endpoint")
		return
	}
	id := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}
	var err error
	switch {
	case action == "" && r.Method == http.MethodGet:
		var record Session
		record, err = s.manager.Get(id)
		if err == nil {
			respond(w, 200, record)
			return
		}
	case action == "" && r.Method == http.MethodDelete:
		err = s.manager.Delete(id)
	case action == "cancel" && r.Method == http.MethodPost:
		err = s.manager.Cancel(id)
	case action == "agent-state" && r.Method == http.MethodGet:
		var snapshot map[string]any
		snapshot, err = s.manager.AgentSnapshot(id)
		if err == nil {
			respond(w, 200, snapshot)
			return
		}
	case action == "agent-message" && r.Method == http.MethodPost:
		var req AgentMessage
		if !readJSON(w, r, &req) {
			return
		}
		err = s.manager.AgentMessage(id, req)
	case action == "input" && r.Method == http.MethodPost:
		var req struct {
			Data []byte `json:"data"`
		}
		if !readJSON(w, r, &req) {
			return
		}
		err = s.manager.Input(id, req.Data)
	case action == "resize" && r.Method == http.MethodPost:
		var req struct {
			Rows uint16 `json:"rows"`
			Cols uint16 `json:"cols"`
		}
		if !readJSON(w, r, &req) {
			return
		}
		err = s.manager.Resize(id, req.Rows, req.Cols)
	case action == "output" && r.Method == http.MethodGet:
		offset := int64(0)
		if text := r.URL.Query().Get("offset"); text != "" {
			offset, err = strconv.ParseInt(text, 10, 64)
			if err != nil {
				fail(w, 400, "invalid offset")
				return
			}
		}
		var data []byte
		var start, next int64
		wait := 0
		if value := r.URL.Query().Get("wait_ms"); value != "" {
			wait, err = strconv.Atoi(value)
			if err != nil || wait < 0 || wait > 1000 {
				fail(w, 400, "invalid output wait")
				return
			}
		}
		deadline := time.Now().Add(time.Duration(wait) * time.Millisecond)
		for {
			// Capture notification before reading to avoid losing a write
			// between an empty read and subscribing for the next byte.
			changed := s.manager.outputSignal(id)
			data, start, next, err = s.manager.OutputPage(id, offset)
			if err != nil || len(data) > 0 || changed == nil || !time.Now().Before(deadline) {
				break
			}
			timer := time.NewTimer(time.Until(deadline))
			select {
			case <-r.Context().Done():
				timer.Stop()
				return
			case <-changed:
				timer.Stop()
			case <-timer.C:
			}
		}
		if err == nil {
			respond(w, 200, map[string]any{"data": data, "start_offset": start, "next_offset": next, "retention_bytes": maxOutput})
			return
		}
	default:
		fail(w, 405, "method not allowed")
		return
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fail(w, 404, "session not found")
		} else {
			fail(w, 400, err.Error())
		}
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func readJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		fail(w, 415, "application/json required")
		return false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequest))
	if err != nil {
		fail(w, 413, "request too large")
		return false
	}
	if err := decodeJSON(data, value); err != nil {
		fail(w, 400, "invalid JSON request: "+err.Error())
		return false
	}
	return true
}
