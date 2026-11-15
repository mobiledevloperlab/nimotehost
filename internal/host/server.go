// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
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
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mobiledevloperlab/nimotehost/internal/plugins"
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
	catalog    *plugins.Catalog
	plugins    *plugins.Runtime
	dir        string
	manager    *Manager
	token      string
	hostID     string
	instanceID string
	shutdown   func()
	requests   chan struct{}
	controls   chan struct{}
	helpers    chan struct{}
	admission  *requestAdmission
}

// Serve reserves the loopback listener before reading or recovering session
// state. A second daemon cannot mark the live daemon's processes interrupted.
func Serve(ctx context.Context, dir string, ready io.Writer) error {
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
	config, err := ReadConfig(dir)
	if err != nil {
		return err
	}
	rawListener, err := net.Listen("tcp4", config.Address())
	if err != nil {
		return fmt.Errorf("listen on Host loopback address: %w", err)
	}
	listener := newBoundedListener(rawListener, 128)
	defer listener.Close()
	token, err := loadSecret(dir, "token")
	if err != nil {
		return err
	}
	hostID, err := loadSecret(dir, "host-id")
	if err != nil {
		return err
	}
	limits, err := ReadResourceLimits(dir)
	if err != nil {
		return fmt.Errorf("read resource policy: %w", err)
	}
	manager, err := NewManagerWithLimits(filepath.Join(dir, "sessions"), limits)
	if err != nil {
		return err
	}
	defer manager.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	instanceID, err := randomID()
	if err != nil {
		return err
	}
	api := &Server{dir: dir, manager: manager, token: token, hostID: hostID, instanceID: instanceID, shutdown: cancel, requests: make(chan struct{}, 64), controls: make(chan struct{}, 16), helpers: make(chan struct{}, 4), admission: newRequestAdmission()}
	extensionDir := filepath.Join(dir, "plugins")
	api.plugins, err = plugins.New(extensionDir, instanceID, pluginLauncher(limits))
	if err != nil {
		return fmt.Errorf("load plugins: %w", err)
	}
	defer api.plugins.Close()
	api.catalog, err = plugins.NewCatalog(filepath.Join(dir, "plugin-catalogs.json"))
	if err != nil {
		slog.Warn("plugin catalogs unavailable", "code", "invalid_catalog_configuration")
	} else {
		defer api.catalog.Close()
	}

	communityAdapters.Lock()
	communityAdapters.runtime = api.plugins
	communityAdapters.dir = extensionDir
	communityAdapters.Unlock()
	defer func() { communityAdapters.Lock(); communityAdapters.runtime = nil; communityAdapters.Unlock() }()
	server := &http.Server{Handler: api, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	if runtime.GOOS == "windows" {
		if err := publishExecutable(dir); err != nil {
			return fmt.Errorf("publish SSH discovery: %w", err)
		}
	}
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
	slog.Info("NimoteHost listening", "address", config.Address(), "protocol", Protocol)
	if ready != nil {
		_, _ = fmt.Fprintf(ready, "NimoteHost %s listening on %s\n", Version, config.Address())
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
	code := map[int]string{400: "invalid_request", 401: "unauthenticated", 403: "permission_denied", 404: "not_found", 405: "method_not_allowed", 409: "conflict", 413: "request_too_large", 415: "unsupported_media_type", 426: "incompatible_protocol", 429: "resource_exhausted", 500: "internal", 503: "unavailable"}[status]
	respond(w, status, map[string]string{"error": message, "code": code})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Browser-originated requests are never part of the SSH/CLI RPC contract.
	if r.Header.Get("Origin") != "" {
		fail(w, 403, "browser origins are not allowed")
		return
	}
	provided, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	owner := bearer && s.token != "" && len(provided) == len(s.token) && subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) == 1
	var device Device
	var authorized bool
	if !owner && bearer && s.dir != "" {
		device, authorized = authenticatedDevice(s.dir, provided)
	}
	if !owner && !authorized {
		fail(w, 401, "authentication required")
		return
	}
	if !owner && !device.allows(r.Method, r.URL.Path) {
		fail(w, 403, "device permission denied")
		return
	}
	if r.Header.Get("Nimote-Host-Protocol") != strconv.Itoa(Protocol) {
		fail(w, 426, "incompatible Host protocol")
		return
	}
	if s.admission != nil {
		release, ok := s.admission.acquire(device.ID, owner, r.Method != http.MethodGet)
		if !ok {
			w.Header().Set("Retry-After", "1")
			fail(w, 429, "principal request capacity reached")
			return
		}
		defer release()
	}
	slots := s.requests
	if r.Method != http.MethodGet && s.controls != nil {
		slots = s.controls
	}
	if owner && s.admission != nil {
		slots = nil
	} // Owner capacity is reserved independently.
	if slots != nil {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			w.Header().Set("Retry-After", "1")
			fail(w, 503, "Host request capacity reached")
			return
		}
	}
	if r.URL.Path == "/v1/extensions" {
		if !owner {
			fail(w, 403, "plugin operations require Host owner authorization")
			return
		}
		s.servePlugins(w, r)
		return
	}
	scoped := !owner && device.Role != "observer"
	if device.Role == "session_controller" && strings.HasPrefix(r.URL.Path, "/v1/sessions/") {
		id := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/sessions/"), "/")[0]
		if !s.manager.controllerSessionAllowed(device, id) {
			fail(w, 403, "execution identity boundary is unavailable")
			return
		}
	}

	if r.URL.Path == "/v1/inbox" || strings.HasPrefix(r.URL.Path, "/v1/inbox/") {
		var allowed func(string) bool
		if scoped {
			allowed = func(id string) bool {
				return device.allowsSession(id) && (device.Role != "session_controller" || s.manager.controllerSessionAllowed(device, id))
			}
		}
		principal := ""
		if !owner {
			principal = "device/" + device.ID + "/"
		}
		s.serveInbox(w, r, allowed, principal)
		return
	}
	if r.URL.Path == "/v1/conflicts" && r.Method == http.MethodGet {
		var allowed func(string) bool
		if scoped {
			allowed = func(id string) bool {
				return device.allowsSession(id) && (device.Role != "session_controller" || s.manager.controllerSessionAllowed(device, id))
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		respond(w, 200, map[string]any{"warnings": s.manager.Conflicts(ctx, allowed)})
		return
	}
	if r.URL.Path == "/v1/github-tasks" || strings.HasPrefix(r.URL.Path, "/v1/github-tasks/") {
		var allowed func(string) bool
		if scoped {
			allowed = func(id string) bool {
				return device.allowsSession(id) && (device.Role != "session_controller" || s.manager.controllerSessionAllowed(device, id))
			}
		}
		s.serveGitHubTasks(w, r, allowed)
		return
	}
	operation := func(name string) string {
		if !owner {
			return "device/" + device.ID + "/" + name
		}
		return name
	}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/capabilities" {
		respond(w, 200, s.hostCapabilities())
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/status" {
		status := map[string]any{"running": true, "version": Version, "host_id": s.hostID, "instance_id": s.instanceID}
		if !scoped {
			status["request_storage"] = s.manager.RequestStorage()
			if s.manager != nil {
				status["resources"] = s.manager.Resources()
			}
		}
		respond(w, 200, status)
		return
	}
	if r.URL.Path == "/v1/agent-history" || strings.HasPrefix(r.URL.Path, "/v1/agent-history/") {
		if s.helpers != nil {
			select {
			case s.helpers <- struct{}{}:
				defer func() { <-s.helpers }()
			default:
				w.Header().Set("Retry-After", "1")
				fail(w, 503, "Host history capacity reached")
				return
			}
		}
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
				page := s.manager.Events(after)
				if scoped {
					filtered := make([]Event, 0)
					for _, e := range page.Events {
						if device.allowsSession(e.SessionID) && (device.Role != "session_controller" || s.manager.controllerSessionAllowed(device, e.SessionID)) {
							filtered = append(filtered, e)
						}
					}
					page.Events = filtered
				}
				respond(w, 200, page)
			} else {
				list := s.manager.List(after)
				if scoped {
					filtered := make([]Session, 0)
					for _, session := range list {
						if device.allowsSession(session.ID) && (device.Role != "session_controller" || s.manager.controllerSessionAllowed(device, session.ID)) {
							filtered = append(filtered, session)
						}
					}
					list = filtered
				}
				respond(w, 200, list)
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
				mutationFailure(w, err)
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
		if scoped {
			var req struct {
				RequestID string `json:"request_id"`
			}
			if !readJSON(w, r, &req) {
				return
			}
			if req.RequestID == "" {
				fail(w, 400, "request_id is required")
				return
			}
			_, err = runOnce(s.manager, req.RequestID, operation("session.cancel/"+id), req, func() (mutationReceipt, error) {
				err := s.manager.Cancel(id)
				return mutationReceipt{OK: err == nil, RequestID: req.RequestID, Stage: "accepted"}, err
			})
		} else {
			err = s.manager.Cancel(id)
		}
	case action == "activity" && r.Method == http.MethodGet:
		var after uint64
		if value := r.URL.Query().Get("after"); value != "" {
			after, err = strconv.ParseUint(value, 10, 64)
			if err != nil {
				fail(w, 400, "invalid activity sequence")
				return
			}
		}
		var page ActivityPage
		page, err = s.manager.Activity(id, after)
		if err == nil {
			respond(w, 200, page)
			return
		}
		if !errors.Is(err, os.ErrNotExist) {
			fail(w, 500, "session activity is unavailable")
			return
		}
	case action == "verification" && r.Method == http.MethodGet:
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var report VerificationReport
		report, err = s.manager.Verification(ctx, id)
		if err == nil {
			respond(w, 200, report)
			return
		}
	case action == "verification-diff" && r.Method == http.MethodGet:
		if _, err = s.manager.Get(id); err == nil {
			var diff []byte
			diff, err = readPrivateLimit(filepath.Join(s.manager.dir, id+".diff"), maxVerificationDiff)
			if err == nil {
				var report VerificationReport
				data, e := readPrivateLimit(filepath.Join(s.manager.dir, id+".verification"), maxEvidenceBytes)
				sum := sha256.Sum256(diff)
				if e != nil || decodeJSON(data, &report) != nil || report.DiffSHA != hex.EncodeToString(sum[:]) {
					fail(w, 409, "verification snapshot changed; refresh the report")
					return
				}
				offset := 0
				if raw := r.URL.Query().Get("offset"); raw != "" {
					offset, e = strconv.Atoi(raw)
					if e != nil || offset < 0 || offset > len(diff) {
						fail(w, 400, "invalid diff offset")
						return
					}
				}
				next := min(offset+(64<<10), len(diff))
				respond(w, 200, map[string]any{"diff": string(diff[offset:next]), "data": diff[offset:next], "start_offset": offset, "next_offset": next, "total_bytes": len(diff), "source": "host_git_snapshot", "diff_sha": report.DiffSHA})
				return
			}
		}
	case action == "verification" && r.Method == http.MethodPost:
		var req struct {
			RequestID string        `json:"request_id"`
			Tests     []TestCommand `json:"tests"`
		}
		if !readJSON(w, r, &req) {
			return
		}
		if req.RequestID == "" {
			fail(w, 400, "request_id is required")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var report VerificationReport
		report, err = runOnce(s.manager, req.RequestID, operation("verification/"+id), req, func() (VerificationReport, error) { return s.manager.StartVerification(ctx, id, req.Tests) })
		if err == nil {
			respond(w, 202, report)
			return
		}
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
		if device.Role == "session_operator" && (req.Action != "interrupt" && !(req.Action == "approval" && req.Decision == "decline")) {
			fail(w, 403, "session operators can only interrupt or decline approvals")
			return
		}
		if req.RequestID == "" {
			fail(w, 400, "request_id is required")
			return
		}
		var receipt mutationReceipt
		receipt, err = runOnce(s.manager, req.RequestID, operation("session.agent-message/"+id), req, func() (mutationReceipt, error) {
			err := s.manager.AgentMessage(id, req)
			return mutationReceipt{OK: err == nil, RequestID: req.RequestID, Stage: "accepted"}, err
		})
		if err == nil {
			respond(w, 200, receipt)
			return
		}
	case action == "input" && r.Method == http.MethodPost:
		var req struct {
			Data      []byte `json:"data"`
			RequestID string `json:"request_id,omitempty"`
		}
		if !readJSON(w, r, &req) {
			return
		}
		var receipt mutationReceipt
		receipt, err = runOnce(s.manager, req.RequestID, operation("session.input/"+id), req, func() (mutationReceipt, error) {
			err := s.manager.Input(id, req.Data)
			return mutationReceipt{OK: err == nil, RequestID: req.RequestID, Stage: "terminal_written"}, err
		})
		if err == nil {
			respond(w, 200, receipt)
			return
		}
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
		resync := false
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
			if err == nil {
				resync = s.manager.outputResync(id, offset, start)
			}
			if err != nil || resync || len(data) > 0 || changed == nil || !time.Now().Before(deadline) {
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
			respond(w, 200, map[string]any{"data": data, "start_offset": start, "next_offset": next, "retention_bytes": maxOutput, "resync_required": resync})
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
			mutationFailure(w, err)
		}
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

type mutationReceipt struct {
	OK        bool   `json:"ok"`
	RequestID string `json:"request_id,omitempty"`
	Stage     string `json:"stage"`
}

func mutationFailure(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, errRequestConflict) {
		status = http.StatusConflict
	} else if errors.Is(err, errRequestUncertain) {
		respond(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error(), "code": "request_uncertain"})
		return
	} else if errors.Is(err, errResourceLimit) {
		status = http.StatusTooManyRequests
		w.Header().Set("Retry-After", "1")
	}
	fail(w, status, err.Error())
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
