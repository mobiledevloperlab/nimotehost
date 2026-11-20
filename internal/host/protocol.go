// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

// Package host implements the optional local Host. It has no dependency on the
// control plane, Tailcat, relay admission, or the client's SSH implementation.
package host

import (
	"runtime"
	"sort"
	"time"
)

const Protocol = 1

// Version is set by the release build; protocol compatibility is tracked separately.
var Version = "2.4.0"

const maxOutput = 16 << 20

type Capabilities struct {
	Enforcement *ResourceEnforcement         `json:"enforcement,omitempty"`
	Version     string                       `json:"version"`
	Protocol    int                          `json:"protocol"`
	MinProtocol int                          `json:"min_protocol"`
	Platform    string                       `json:"platform"`
	Arch        string                       `json:"arch"`
	HostID      string                       `json:"host_id"`
	Features    map[string]bool              `json:"features"`
	Tools       map[string]string            `json:"tools"`
	Agents      map[string]AgentInstallation `json:"agents,omitempty"`
}

func capabilities(hostID string) Capabilities {
	tools := make(map[string]string)
	for _, tool := range []string{"git", "docker", "sh", "bash", "zsh", "pwsh", "powershell", "cmd", "go", "python3", "node", "codex", "claude", "opencode", "claude-agent-acp", "gh"} {
		if path, _, err := resolveTool(tool, false); err == nil {
			tools[tool] = path
		}
	}
	_, claudeAdapter := tools["claude-agent-acp"]
	caps := Capabilities{Version: Version, Protocol: Protocol, MinProtocol: Protocol, Platform: runtime.GOOS, Arch: runtime.GOARCH, HostID: hostID,
		Features: map[string]bool{"rpc_error_details_v1": true, "github_issue_tasks_v1": true, "github_issue_dispatch_v1": tools["gh"] != "" && tools["git"] != "", "agent_history_aggregate_v1": true, "workspace_conflicts_v1": true, "isolated_worktree_v1": true, "task_verification_v1": true, "agent_inbox_v1": true, "output_resync_v1": true, "device_session_acl_v1": true, "device_safe_control_v1": true, "hard_process_limit": nativeQuotasSupported, "state_recovery_v1": true, "resource_limits_v1": true, "hard_cpu_limit": nativeQuotasSupported, "hard_memory_limit": nativeQuotasSupported, "session_activity_v1": true, "session_task_v1": true, "mutation_idempotency_v1": true, "mutation_expiry_v1": true, "agent_status_v1": true, "session_disconnect_survival": true, "session_restart_survival": false, "rpc_stream": true, "rolling_output": true, "idle_shutdown": true, "tasks": true, "output_replay": true, "events": true, "environment": true, "terminal": terminalSupported, "agent_terminal": terminalSupported, "agent_structured_approval": false, "public_transport": false, "codex_structured": structuredAgentSupported, "codex_structured_auto": structuredAgentSupported, "agent_events_v1": true, "agent_controls_v1": true, "agent_execution_policy_v1": true, "agent_cold_start_wait": true, "agent_history_v1": structuredAgentSupported && (tools["codex"] != "" || tools["opencode"] != "" || claudeAdapter), "codex_history_v1": structuredAgentSupported && tools["codex"] != "", "opencode_history_v1": structuredAgentSupported && tools["opencode"] != "", "claude_history_v1": structuredAgentSupported && claudeAdapter, "opencode_structured": structuredAgentSupported, "opencode_structured_auto": structuredAgentSupported, "claude_structured": structuredAgentSupported && claudeAdapter, "claude_structured_auto": structuredAgentSupported && claudeAdapter}, Tools: tools, Agents: installedAgents()}
	caps.Features["external_session_metadata_v1"] = true
	caps.Features["tool_environment_v1"] = activeToolEnvironment.Load() != nil
	caps.Features["terminal_input_stream_v1"] = true
	caps.Features["external_session_attach_v1"] = true
	caps.Features["claude_live_discovery_v1"] = tools["claude"] != ""
	return caps
}

type StartRequest struct {
	Attach *ExternalAttachment `json:"attach,omitempty"`

	sourceDirectory string
	baseCommit      string
	Worktree        *WorktreeRequest   `json:"worktree,omitempty"`
	Identity        *ExecutionIdentity `json:"execution_identity,omitempty"`
	ExecutionPolicy string             `json:"execution_policy,omitempty"`
	Task            *TaskReference     `json:"task,omitempty"`
	RequestID       string             `json:"request_id,omitempty"`
	fallbackReason  string
	Interaction     string       `json:"interaction,omitempty"` // terminal (default), structured, or auto
	Kind            string       `json:"kind"`                  // task, terminal, agent
	Command         string       `json:"command"`
	Args            []string     `json:"args"`
	Directory       string       `json:"directory"`
	Agent           string       `json:"agent"` // codex, claude, opencode
	Resume          *AgentResume `json:"resume,omitempty"`
}

type AgentResume struct {
	ConversationID string `json:"conversation_id"`
}

type Session struct {
	External *ExternalConnection `json:"external,omitempty"`

	SourceDirectory       string         `json:"source_directory,omitempty"`
	BaseCommit            string         `json:"base_commit,omitempty"`
	ExecutionPolicy       string         `json:"execution_policy,omitempty"`
	ActivitySequence      uint64         `json:"activity_sequence,omitempty"`
	Task                  *TaskReference `json:"task,omitempty"`
	AgentStatus           string         `json:"agent_status,omitempty"`
	StartRequestID        string         `json:"start_request_id,omitempty"`
	CompatibilityFallback string         `json:"compatibility_fallback,omitempty"`
	Interaction           string         `json:"interaction,omitempty"`
	OutputBytes           int64          `json:"output_bytes"`
	OutputTruncated       bool           `json:"output_truncated"`
	ID                    string         `json:"id"`
	ConversationID        string         `json:"conversation_id,omitempty"`
	Kind                  string         `json:"kind"`
	Agent                 string         `json:"agent,omitempty"`
	Command               string         `json:"command"`
	Args                  []string       `json:"args,omitempty"`
	Directory             string         `json:"directory"`
	State                 string         `json:"state"`
	Started               time.Time      `json:"started"`
	Finished              *time.Time     `json:"finished,omitempty"`
	ExitCode              *int           `json:"exit_code,omitempty"`
	Error                 string         `json:"error,omitempty"`
	Revision              uint64         `json:"revision"`
}

// A client Task reference scoped by the session's Host and workspace.
// It never authorizes executing that Task or bypassing approvals.
type TaskReference struct {
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
}

func sortSessions(sessions []Session) {
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Started.Before(sessions[j].Started) })
}
