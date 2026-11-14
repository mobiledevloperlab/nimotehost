// SPDX-License-Identifier: Apache-2.0
// Package desktop is the headless management facade shared by the desktop UI.
// Existing host.Call remains the authenticated local IPC; no second API is exposed.
package desktop

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mobiledevloperlab/nimote-host/internal/host"
	"github.com/mobiledevloperlab/nimote-host/internal/hostservice"
)

type Manager struct{ Dir, Binary string }
type Snapshot struct {
	RequestStorage *host.RequestStorageStatus
	State          string
	Version        string
	Name           string
	Platform       string
	Address        string
	LAN            []string
	Autostart      bool
	ServiceHelp    string
}

func New(binary string) (*Manager, error) {
	dir, err := host.DefaultDir()
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(binary) {
		return nil, errors.New("bundled CLI path must be absolute")
	}
	return &Manager{dir, binary}, nil
}
func Unavailable(err error) bool {
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial" && errors.Is(err, connectionRefused)
}
func (m *Manager) service() (*hostservice.Service, error) {
	return hostservice.NewHostExecutable(m.Binary)
}
func (m *Manager) Status(ctx context.Context) (Snapshot, error) {
	name, err := os.Hostname()
	if err != nil {
		return Snapshot{}, err
	}
	s := Snapshot{Name: name, Platform: runtime.GOOS + "/" + runtime.GOARCH, State: "Unknown"}
	c, err := host.ReadConfig(m.Dir)
	if err != nil {
		return s, err
	}
	s.Address = c.Address()
	service, err := m.service()
	if err != nil {
		return s, err
	}
	s.Autostart, err = service.Installed()
	if err != nil {
		return s, err
	}
	s.ServiceHelp = service.Diagnostics()
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, a := range addrs {
			if ip, ok := a.(*net.IPNet); ok && !ip.IP.IsLoopback() && ip.IP.To4() != nil {
				s.LAN = append(s.LAN, ip.IP.String())
			}
		}
	}
	b, err := host.Call(ctx, m.Dir, host.RPCRequest{Method: "GET", Path: "/v1/status"})
	if Unavailable(err) {
		idle, lockErr := host.StateIdle(m.Dir)
		if lockErr != nil {
			return s, lockErr
		}
		if !idle {
			return s, errors.New("Host still owns its state but local RPC is unavailable; inspect Diagnostics or retry after shutdown")
		}
		s.State = "Stopped"
		return s, nil
	}
	if err != nil {
		return s, err
	}
	var status struct {
		RequestStorage *host.RequestStorageStatus `json:"request_storage"`
		Running        bool                       `json:"running"`
		Version        string                     `json:"version"`
	}
	if err = json.Unmarshal(b, &status); err != nil {
		return s, err
	}
	if !status.Running {
		return s, errors.New("Host returned an invalid running status")
	}
	s.State = "Running"
	s.Version = status.Version
	s.RequestStorage = status.RequestStorage
	return s, nil
}
func (m *Manager) Sessions(ctx context.Context) ([]host.Session, error) {
	b, err := host.Call(ctx, m.Dir, host.RPCRequest{Method: "GET", Path: "/v1/sessions"})
	if err != nil {
		return nil, err
	}
	var sessions []host.Session
	err = json.Unmarshal(b, &sessions)
	return sessions, err
}
func (m *Manager) wait(ctx context.Context, running bool) error {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		_, err := host.Call(ctx, m.Dir, host.RPCRequest{Method: "GET", Path: "/v1/status"})
		if running && err == nil {
			return nil
		}
		if !running && Unavailable(err) {
			idle, lockErr := host.StateIdle(m.Dir)
			if lockErr != nil {
				return lockErr
			}
			if idle {
				return nil
			}
		}
		if err != nil && !Unavailable(err) {
			var op *net.OpError
			if !errors.As(err, &op) && !errors.Is(err, io.EOF) {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

// Change holds a process-wide OS lock across check, mutation and readiness.
func (m *Manager) Change(ctx context.Context, action string) error {
	lock, err := host.ControlLock(m.Dir)
	if err != nil {
		return err
	}
	defer lock.Close()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	status, err := m.Status(ctx)
	if err != nil {
		return err
	}
	switch action {
	case "start":
		if status.State == "Running" {
			return nil
		}
		if err = m.wait(ctx, false); err != nil {
			return err
		}
		service, err := m.service()
		if err != nil {
			return err
		}
		if status.Autostart {
			err = service.Restart(ctx)
		} else {
			err = m.launch()
		}
		if err != nil {
			return err
		}
		return m.wait(ctx, true)
	case "stop", "restart":
		if status.State == "Running" {
			_, err = host.Call(ctx, m.Dir, host.RPCRequest{Method: "POST", Path: "/v1/shutdown"})
			if err != nil {
				return err
			}
		}
		if err = m.wait(ctx, false); err != nil {
			return err
		}
		if action == "stop" {
			return nil
		}
		service, err := m.service()
		if err != nil {
			return err
		}
		if status.Autostart {
			err = service.Restart(ctx)
		} else {
			err = m.launch()
		}
		if err != nil {
			return err
		}
		return m.wait(ctx, true)
	case "enable-autostart":
		if status.Autostart {
			return nil
		}
		if status.State == "Running" {
			return errors.New("stop Host before enabling login startup")
		}
		service, err := m.service()
		if err != nil {
			return err
		}
		if err = service.Install(ctx); err != nil {
			return err
		}
		return m.wait(ctx, true)
	case "disable-autostart":
		if !status.Autostart {
			return nil
		}
		if status.State == "Running" {
			return errors.New("stop Host before disabling login startup")
		}
		service, err := m.service()
		if err != nil {
			return err
		}
		return service.Uninstall(ctx)
	default:
		return errors.New("unknown management action")
	}
}
func (m *Manager) SaveConfig(c host.Config) error {
	lock, err := host.ControlLock(m.Dir)
	if err != nil {
		return err
	}
	defer lock.Close()
	return host.SaveConfig(m.Dir, c)
}
func (m *Manager) launch() error {
	// No CommandContext, pipes, or GUI-owned job object: GUI exit cannot kill Host.
	info, err := os.Stat(m.Binary)
	if err != nil {
		return fmt.Errorf("locate bundled CLI: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("bundled CLI is not a regular file")
	}
	cmd := exec.Command(m.Binary, "start")
	detach(cmd)
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	cmd.Stdin = null
	cmd.Stdout = null
	cmd.Stderr = null
	if err = cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// Diagnostics intentionally excludes tokens, session contents, commands, and environment.
func (m *Manager) Diagnostics(ctx context.Context) string {
	s, err := m.Status(ctx)
	storage := "Request storage: unavailable"
	if r := s.RequestStorage; r != nil {
		storage = fmt.Sprintf("Request storage: %d/%d receipts (%d legacy); %d/%d bytes; capacity warning: %t", r.Receipts, r.ReceiptLimit, r.LegacyReceipts, r.Bytes+r.ReservedBytes, r.ByteLimit, r.CapacityWarning)
	}
	return fmt.Sprintf("Nimote Host Desktop\nObserved: %s\nState: %s\nHost: %s\nPlatform: %s\nHost version: %s\nLocal IPC: %s\nLAN: %s (SSH only; IPC is loopback)\nLogin service: %t\nSSH: externally managed; availability not probed\n%s\n%s\nError: %v\n", time.Now().Format(time.RFC3339), s.State, s.Name, s.Platform, s.Version, s.Address, strings.Join(s.LAN, ", "), s.Autostart, s.ServiceHelp, storage, err)
}

// AgentState is a fresh provider snapshot, never inferred from terminal text.
func (m *Manager) AgentState(ctx context.Context, id string) (json.RawMessage, error) {
	if !sessionID(id) {
		return nil, errors.New("invalid session ID")
	}
	return host.Call(ctx, m.Dir, host.RPCRequest{Method: "GET", Path: "/v1/sessions/" + id + "/agent-state"})
}

// MutationRequestID negotiates the retry contract on the current connection.
// Old Hosts continue to receive opaque keys with their original semantics.
func (m *Manager) MutationRequestID(ctx context.Context) (string, error) {
	raw, err := host.Call(ctx, m.Dir, host.RPCRequest{Method: "GET", Path: "/v1/capabilities"})
	if err != nil {
		return "", err
	}
	var caps host.Capabilities
	if err := json.Unmarshal(raw, &caps); err != nil {
		return "", err
	}
	if caps.Features["mutation_expiry_v1"] {
		return host.NewRequestID()
	}
	return rand.Text(), nil
}

func sessionID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'f' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// Decide uses the existing receipt ledger. A failed response is never replayed
// with a new request ID; the user must inspect current provider state.
func (m *Manager) Decide(ctx context.Context, id, approval, decision, requestID string) error {
	if !sessionID(id) {
		return errors.New("invalid session ID")
	}
	body, err := json.Marshal(host.AgentMessage{Action: "approval", RequestID: requestID, ApprovalID: approval, Decision: decision})
	if err != nil {
		return err
	}
	_, err = host.Call(ctx, m.Dir, host.RPCRequest{Method: "POST", Path: "/v1/sessions/" + id + "/agent-message", Body: body})
	return err
}
