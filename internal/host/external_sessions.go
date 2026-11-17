// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"errors"
	"strings"
)

// External discovery shares the launcher registry and the existing protocol
// clients. It never owns or signals an independently started Agent process.
// Future adapters can implement this interface without changing the Manager.
type externalSessionAdapter interface {
	ListSessions(context.Context, string, string) (codexHistoryPage, error)
	ReadSession(context.Context, string, string, string) (map[string]any, error)
	SessionCapabilities() ExternalSessionCapabilities
}

// Optional live extension of the discovery adapter. A history-only adapter
// cannot accidentally fall through to launching a fresh protocol process.
type externalLiveSessionAdapter interface {
	AttachSession(*structuredAgent, ExternalAttachment) (processHandle, error)
}

func (a protocolSessionAdapter) AttachSession(agent *structuredAgent, connection ExternalAttachment) (processHandle, error) {
	if err := connection.validate(a.agent); err != nil {
		return nil, err
	}
	switch a.agent {
	case "codex":
		return startCodexSocketAttachment(agent, connection)
	case "opencode":
		return startOpenCodeAttachment(agent, connection)
	default:
		return nil, errors.New("external live attachment is unsupported")
	}
}

// Values are supported, conditional, or unsupported. Conditional requires a
// successful native request; installation or a saved ID is not sufficient.
type ExternalSessionCapabilities struct {
	DiscoveryTransports []string `json:"discovery_transports,omitempty"`
	LiveTransports      []string `json:"live_transports,omitempty"`
	Discovery           string   `json:"discovery"`
	History             string   `json:"history"`
	Resume              string   `json:"resume"`
	LiveControl         string   `json:"live_control"`
	HistoryMode         string   `json:"history_mode"`
	Reason              string   `json:"reason"`
}

type protocolSessionAdapter struct {
	agent string
}

func lookupExternalSessionAdapter(agent string) (externalSessionAdapter, bool) {
	if agent != "codex" && agent != "claude" && agent != "opencode" {
		return nil, false
	}
	if _, ok := lookupAgentAdapter(agent); !ok {
		return nil, false
	}
	return protocolSessionAdapter{agent: agent}, true
}

func (a protocolSessionAdapter) SessionCapabilities() ExternalSessionCapabilities {
	var transports []string
	var discoveryTransports []string
	if a.agent == "claude" {
		discoveryTransports = []string{"claude-cli"}
	}
	if a.agent == "codex" {
		transports = []string{"codex-control"}
	}
	if a.agent == "opencode" {
		transports = []string{"opencode-http-v1", "opencode-http-v2"}
	}
	mode := "native_read"
	if a.agent != "codex" {
		// ACP has no passive transcript-read method. Load/resume is an explicit
		// provider operation, not a read-only file scan or live-process attach.
		mode = "native_load_replay"
	}
	return ExternalSessionCapabilities{
		DiscoveryTransports: discoveryTransports,
		LiveTransports:      transports,
		Discovery:           "conditional", History: "conditional", Resume: "conditional",
		LiveControl: "unsupported", HistoryMode: mode,
		Reason: "Native protocol support must be negotiated; independently running processes and their pending approvals cannot be attached by this adapter",
	}
}

func (a protocolSessionAdapter) ListSessions(ctx context.Context, cwd, cursor string) (codexHistoryPage, error) {
	if a.agent == "codex" {
		return listCodexHistory(ctx, cwd, cursor)
	}
	return listACPHistory(ctx, a.agent, cwd, cursor)
}

func (a protocolSessionAdapter) ReadSession(ctx context.Context, id, cwd, cursor string) (map[string]any, error) {
	if a.agent == "codex" {
		return readCodexHistory(ctx, id, cursor)
	}
	if cursor != "" {
		return nil, errors.New("ACP transcript pagination is unsupported")
	}
	return readACPHistory(ctx, a.agent, id, cwd)
}

// Additive metadata on existing history rows. Historical timestamps and a
// freshly launched query process cannot establish an external task's status.
func describeExternalSessions(items []map[string]any, sessions []Session) {
	for _, item := range items {
		agent, _ := item["agent"].(string)
		id, _ := item["conversation_id"].(string)
		cwd, _ := item["directory"].(string)
		adapter, ok := lookupExternalSessionAdapter(agent)
		if !ok {
			continue
		}
		caps := adapter.SessionCapabilities()
		caps.Discovery = "supported" // This individual native query succeeded.
		nativeClaude := agent == "claude" && item["status_source"] == "claude_agents_json"
		connection, liveQuery := item["external_connection"].(ExternalConnection)
		if liveQuery {
			caps.LiveControl = "conditional"
			caps.HistoryMode = "native_read"
			caps.Reason = "Explicit connection to an existing service; session identity and control support require a successful attachment"
		}
		item["session_capabilities"] = caps
		if nativeClaude {
			caps.History, caps.Resume = "unsupported", "unsupported"
			caps.HistoryMode = "unavailable"
			caps.Reason = "Official CLI discovery reports state only; no structured approval IDs or safe attach-only control is exposed"
			item["session_capabilities"] = caps
		} else if !liveQuery {
			item["origin"] = "external_history"
			item["execution_status"] = "unknown"
			item["live_control"] = "unsupported"
			item["status_source"] = "unavailable"
		}
		delete(item, "host_session_id")
		if id == "" || cwd == "" {
			continue
		}
		for _, session := range sessions {
			if liveQuery && (session.External == nil || *session.External != connection) {
				continue
			}
			if session.Kind != "agent" || session.Interaction != "structured" || session.State != "running" ||
				session.Agent != agent || session.ConversationID != id ||
				!sameWorkspaceDirectory(canonicalDirectory(cwd), canonicalDirectory(session.Directory)) {
				continue
			}
			item["origin"] = "host_managed"
			item["host_session_id"] = session.ID
			item["execution_status"] = session.AgentStatus
			if session.AgentStatus == "" {
				item["execution_status"] = "unknown"
			}
			item["status_source"] = "host_protocol"
			item["live_control"] = "supported"
			if session.AgentStatus == "" || session.AgentStatus == "unknown" || session.AgentStatus == "disconnected" {
				item["live_control"] = "conditional"
			}
			// Pending approvals and the output cursor remain authoritative at
			// /v1/sessions/{host_session_id}/agent-state, avoiding stale copies.
			break
		}
	}
}

// Use the same ID boundary as StartRequest.Resume: listed IDs must be usable by
// the existing resume path without allowing path-like identifiers.
func validExternalConversationID(id string) bool {
	return id != "" && len(id) <= 128 && !strings.ContainsAny(id, "/\\\x00")
}

var _ externalSessionAdapter = protocolSessionAdapter{}
