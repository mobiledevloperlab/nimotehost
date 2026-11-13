// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

// Package host implements the optional local Host. It has no dependency on the
// control plane, Tailcat, relay admission, or the client's SSH implementation.
package host

import (
	"os/exec"
	"runtime"
	"sort"
	"time"
)

const Protocol = 1

// Version is set by the release build; protocol compatibility is tracked separately.
var Version = "1.0.0-dev"

const maxOutput = 16 << 20

type Capabilities struct {
	Version     string            `json:"version"`
	Protocol    int               `json:"protocol"`
	MinProtocol int               `json:"min_protocol"`
	Platform    string            `json:"platform"`
	Arch        string            `json:"arch"`
	HostID      string            `json:"host_id"`
	Features    map[string]bool   `json:"features"`
	Tools       map[string]string `json:"tools"`
}

func capabilities(hostID string) Capabilities {
	tools := make(map[string]string)
	for _, tool := range []string{"git", "docker", "sh", "bash", "zsh", "pwsh", "powershell", "cmd", "go", "python3", "node", "codex", "claude", "opencode", "claude-agent-acp"} {
		if path, err := exec.LookPath(tool); err == nil {
			tools[tool] = path
		}
	}
	_, claudeAdapter := tools["claude-agent-acp"]
	return Capabilities{Version: Version, Protocol: Protocol, MinProtocol: Protocol, Platform: runtime.GOOS, Arch: runtime.GOARCH, HostID: hostID,
		Features: map[string]bool{"rpc_stream": true, "rolling_output": true, "idle_shutdown": true, "tasks": true, "output_replay": true, "events": true, "environment": true, "terminal": terminalSupported, "agent_terminal": terminalSupported, "agent_structured_approval": false, "public_transport": false, "codex_structured": structuredAgentSupported, "codex_structured_auto": structuredAgentSupported, "agent_events_v1": true, "agent_controls_v1": true, "agent_cold_start_wait": true, "agent_history_v1": structuredAgentSupported && (tools["codex"] != "" || tools["opencode"] != "" || claudeAdapter), "codex_history_v1": structuredAgentSupported && tools["codex"] != "", "opencode_history_v1": structuredAgentSupported && tools["opencode"] != "", "claude_history_v1": structuredAgentSupported && claudeAdapter, "opencode_structured": structuredAgentSupported, "opencode_structured_auto": structuredAgentSupported, "claude_structured": structuredAgentSupported && claudeAdapter, "claude_structured_auto": structuredAgentSupported && claudeAdapter}, Tools: tools}
}

type StartRequest struct {
	fallbackReason string
	Interaction    string       `json:"interaction,omitempty"` // terminal (default), structured, or auto
	Kind           string       `json:"kind"`                  // task, terminal, agent
	Command        string       `json:"command"`
	Args           []string     `json:"args"`
	Directory      string       `json:"directory"`
	Agent          string       `json:"agent"` // codex, claude, opencode
	Resume         *AgentResume `json:"resume,omitempty"`
}

type AgentResume struct {
	ConversationID string `json:"conversation_id"`
}

type Session struct {
	CompatibilityFallback string     `json:"compatibility_fallback,omitempty"`
	Interaction           string     `json:"interaction,omitempty"`
	OutputBytes           int64      `json:"output_bytes"`
	OutputTruncated       bool       `json:"output_truncated"`
	ID                    string     `json:"id"`
	ConversationID        string     `json:"conversation_id,omitempty"`
	Kind                  string     `json:"kind"`
	Agent                 string     `json:"agent,omitempty"`
	Command               string     `json:"command"`
	Args                  []string   `json:"args,omitempty"`
	Directory             string     `json:"directory"`
	State                 string     `json:"state"`
	Started               time.Time  `json:"started"`
	Finished              *time.Time `json:"finished,omitempty"`
	ExitCode              *int       `json:"exit_code,omitempty"`
	Error                 string     `json:"error,omitempty"`
	Revision              uint64     `json:"revision"`
}

func sortSessions(sessions []Session) {
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Started.Before(sessions[j].Started) })
}
