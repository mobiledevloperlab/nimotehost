// SPDX-License-Identifier: Apache-2.0
package host

import (
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// Startup can include plugin discovery, authentication checks and model catalogs.
// Keep every enclosing transport deadline longer than this cold-start budget.
const agentStartupTimeout = 45 * time.Second
const agentCreateRPCTimeout = 60 * time.Second
const agentCreateResponseTimeout = 65 * time.Second

var errProbeCanceled = errors.New("agent probe was canceled by the user")

func (a *structuredAgent) completeStartup(err error) {
	a.startupOnce.Do(func() { a.startup <- err })
}

// Auto mode probes a live stable-protocol handshake before a prompt is accepted.
// This wrapper never holds the manager lock while waiting for the agent, so an
// upgrade-incompatible CLI cannot freeze other terminals. No user turn is sent
// during probing, and running turns are never retried through another adapter.
func (m *Manager) Start(req StartRequest) (Session, error) {
	if req.Interaction != "auto" {
		return m.start(req)
	}
	return m.startAuto(req, agentStartupTimeout)
}
func (m *Manager) startAuto(req StartRequest, timeout time.Duration) (Session, error) {
	if req.Kind != "agent" || !supportsStructuredAgent(req.Agent) || len(req.Args) != 0 {
		return Session{}, errors.New("auto interaction requires a supported agent without CLI arguments")
	}
	if !structuredAgentSupported {
		req.Interaction = "terminal"
		return m.start(req)
	}
	req.Interaction = "structured"
	record, err := m.start(req)
	if err == nil {
		m.mu.Lock()
		item := m.sessions[record.ID]
		m.mu.Unlock()
		if item == nil {
			return Session{}, errors.New("agent startup session no longer exists")
		}
		timer := time.NewTimer(timeout)
		select {
		case err = <-item.agent.startup:
		case <-item.done:
			err = errors.New("agent protocol process exited during initialization")
		case <-timer.C:
			// A ready result may already be queued when both select cases wake.
			select {
			case err = <-item.agent.startup:
			default:
				err = fmt.Errorf("agent protocol initialization timed out after %s", timeout)
			}
		}
		timer.Stop()
		if err == nil {
			return record, nil
		}
		// An explicit cancellation during probing must never launch a new CLI.
		m.mu.Lock()
		canceled := item.userCanceled
		closed := m.closed
		m.mu.Unlock()
		if canceled {
			return m.Get(record.ID)
		}
		if closed {
			return Session{}, errors.New("Host stopped during agent initialization")
		}
		// Wait for complete process/pipe shutdown before starting the terminal.
		if stopErr := m.cancel(record.ID, false); stopErr != nil {
			return Session{}, fmt.Errorf("stop incompatible agent process: %w", stopErr)
		}
		<-item.done
		if deleteErr := m.delete(record.ID, true); deleteErr != nil {
			if errors.Is(deleteErr, errProbeCanceled) {
				return m.Get(record.ID)
			}
			return Session{}, deleteErr
		}
	}
	if req.Resume != nil {
		// Never fall back to a fresh terminal conversation when the requested
		// native conversation could not be resumed.
		return Session{}, fmt.Errorf("native conversation resume failed: %w", err)
	}
	req.Interaction = "terminal"
	reason := err.Error()
	if len(reason) > 512 {
		reason = reason[:512]
	}
	req.fallbackReason = req.Agent + " structured interface unavailable; using terminal: " + reason
	return m.start(req)
}

// Resolve maintained adapters at session creation, never with npx/latest or
// automatic installation. Upgrades are checked by the live ACP handshake.
func configureStructuredCommand(cmd *exec.Cmd, a *structuredAgent) error {
	switch a.provider {
	case "claude":
		path, err := exec.LookPath("claude-agent-acp")
		if err != nil {
			return fmt.Errorf("Claude ACP adapter is not installed: %w", err)
		}
		cmd.Path = path
		cmd.Err = nil
		cmd.Args = []string{path}
	case "opencode":
		cmd.Args = []string{cmd.Path, "acp"}
	default:
		cmd.Args = []string{cmd.Path, "app-server"}
	}
	return nil
}
