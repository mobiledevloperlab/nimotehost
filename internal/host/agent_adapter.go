// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Narrow launch interface; protocol events, approval semantics and history stay
// in the existing Codex/ACP implementations and shared structuredAgent lifecycle.
type agentAdapter interface {
	ConfigureCommand(*exec.Cmd) error
	ProtocolVersion() int
	Executable() string
	Protocol() string
}
type cliAgentAdapter struct {
	executable string
	protocol   string
	version    int
	args       []string
}

func (d cliAgentAdapter) ProtocolVersion() int { return d.version }
func (d cliAgentAdapter) Executable() string   { return d.executable }
func (d cliAgentAdapter) Protocol() string     { return d.protocol }
func (d cliAgentAdapter) ConfigureCommand(cmd *exec.Cmd) error {
	path, err := exec.LookPath(d.executable)
	if err != nil {
		if d.executable == "claude-agent-acp" {
			return fmt.Errorf("Claude ACP adapter is not installed: %w", err)
		}
		return err
	}
	cmd.Path, cmd.Err = path, nil
	cmd.Args = append([]string{path}, d.args...)
	return nil
}
func lookupAgentAdapter(provider string) (agentAdapter, bool) {
	switch provider {
	case "codex":
		return cliAgentAdapter{"codex", "codex-app-server", 0, []string{"app-server"}}, true
	case "opencode":
		return cliAgentAdapter{"opencode", "acp", 1, []string{"acp"}}, true
	case "claude":
		return cliAgentAdapter{"claude-agent-acp", "acp", 2, nil}, true
	default:
		return communityAdapter(provider)
	}
}

var _ agentAdapter = cliAgentAdapter{}

type AgentInstallation struct {
	CLIPath          string `json:"cli_path,omitempty"`
	CLIDiscovered    bool   `json:"cli_discovered"`
	History          string `json:"history"`
	Resume           string `json:"resume"`
	LiveAttach       bool   `json:"live_attach"`
	LiveAttachReason string `json:"live_attach_reason"`
	Path             string `json:"path,omitempty"`
	Protocol         string `json:"protocol"`
	ProtocolVersion  int    `json:"protocol_version,omitempty"`
	Installed        bool   `json:"installed"`
	Version          string `json:"version,omitempty"`
	ProbeStatus      string `json:"probe_status"`
}

func installedAgents() map[string]AgentInstallation {
	result := make(map[string]AgentInstallation, 3)
	for _, name := range []string{"codex", "claude", "opencode"} {
		d, _ := lookupAgentAdapter(name)
		path, err := exec.LookPath(d.Executable())
		cliPath, cliErr := exec.LookPath(name)
		result[name] = AgentInstallation{CLIPath: cliPath, CLIDiscovered: cliErr == nil, History: "requires_successful_protocol_query", Resume: "requires_successful_protocol_handshake", LiveAttachReason: "arbitrary process attachment is not supported by installed adapters", Path: path, Protocol: d.Protocol(), ProtocolVersion: d.ProtocolVersion(), Installed: err == nil, ProbeStatus: "not_probed"}
	}
	return result
}

// InspectAgents is an explicit local diagnostic, never called by capabilities
// polling. A --version result is descriptive only; live handshakes decide safety.
// Uses the same owned process backend, bounded output and tree cancellation.
func InspectAgents(ctx context.Context) map[string]AgentInstallation {
	result := installedAgents()
	for _, name := range []string{"codex", "claude", "opencode"} {
		info := result[name]
		if !info.Installed {
			info.ProbeStatus = "not_installed"
			result[name] = info
			continue
		}
		if ctx.Err() != nil {
			info.ProbeStatus = "canceled"
			result[name] = info
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		cmd := exec.Command(info.Path, "--version")
		cmd.Env = os.Environ()
		cmd.WaitDelay = time.Second
		var output agentDiagnostics
		process, terminal, err := startProcess(cmd, false, &output)
		if terminal != nil {
			_ = terminal.Close()
		}
		if err != nil {
			info.ProbeStatus = "failed"
			cancel()
			result[name] = info
			continue
		}
		done := make(chan error, 1)
		go func() {
			code, err := process.Wait()
			if code != 0 && err == nil {
				err = errors.New("version command failed")
			}
			done <- err
		}()
		select {
		case err = <-done:
			info.ProbeStatus = "ok"
			if err != nil {
				info.ProbeStatus = "failed"
			} else {
				info.Version = strings.TrimSpace(output.text())
			}
		case <-probeCtx.Done():
			_ = process.Kill()
			<-done
			info.ProbeStatus = "timeout"
			if ctx.Err() != nil {
				info.ProbeStatus = "canceled"
			}
		}
		_ = process.Close()
		cancel()
		result[name] = info
	}
	return result
}
