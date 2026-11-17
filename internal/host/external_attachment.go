// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

// Only non-secret connection identity is persisted with Session. Credentials
// travel in authenticated POST bodies, never query strings, args or Session.
type ExternalConnection struct {
	Transport string `json:"transport"`
	Endpoint  string `json:"endpoint"`
}

type ExternalAttachment struct {
	ExternalConnection
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// Discovery does not imply control. Claude's documented JSON listing needs no
// endpoint and must never pass the live-attachment validator below.
func (c ExternalAttachment) validateDiscovery(agent string) error {
	if c.Transport == "claude-cli" {
		if agent != "claude" || c.Endpoint != "" || c.Username != "" || c.Password != "" {
			return errors.New("Claude CLI discovery requires no endpoint or credentials")
		}
		return nil
	}
	return c.validate(agent)
}

func (c ExternalAttachment) validate(agent string) error {
	if agent == "claude" {
		return errors.New("structured live attachment is unsupported for Claude Code: CLI attachment is terminal-only and may restart stopped sessions")
	}
	if len(c.Endpoint) > 4096 || strings.ContainsAny(c.Endpoint, "\x00\r\n") || len(c.Password) > 8192 || len(c.Username) > 128 || strings.ContainsAny(c.Username, ":\x00\r\n") {
		return errors.New("invalid external connection")
	}
	switch c.Transport {
	case "codex-control":
		if agent != "codex" || !filepath.IsAbs(c.Endpoint) || c.Password != "" || c.Username != "" {
			return errors.New("Codex control attachment requires an absolute socket path and local socket authorization")
		}
	case "opencode-http-v1", "opencode-http-v2":
		u, err := url.Parse(c.Endpoint)
		if agent != "opencode" || err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return errors.New("OpenCode attachment requires a loopback HTTP origin")
		}
		ip := net.ParseIP(u.Hostname())
		port, err := strconv.Atoi(u.Port())
		if ip == nil || !ip.IsLoopback() || err != nil || port < 1024 || port > 65535 {
			return errors.New("OpenCode endpoint must use a literal loopback IP and an explicit unprivileged port")
		}
	default:
		return errors.New("external live control is unsupported for this transport")
	}
	return nil
}

func (a *structuredAgent) checkCodexAttachment(raw json.RawMessage) error {
	var result struct {
		Thread struct {
			ID     string `json:"id"`
			CWD    string `json:"cwd"`
			Status struct {
				Type string `json:"type"`
			} `json:"status"`
			Turns []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"turns"`
		} `json:"thread"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Thread.ID != a.resumeConversation ||
		!sameWorkspaceDirectory(canonicalDirectory(result.Thread.CWD), canonicalDirectory(a.cwd)) {
		return errors.New("external Codex thread identity or workspace does not match")
	}
	if result.Thread.Status.Type != "idle" && result.Thread.Status.Type != "active" {
		return errors.New("Codex thread is not loaded by this server or its live protocol is unsupported")
	}
	a.thread = result.Thread.ID
	if !a.attachmentStateObserved {
		a.busy = result.Thread.Status.Type == "active"
		a.turn = ""
		for _, turn := range result.Thread.Turns {
			if turn.Status == "inProgress" {
				a.turn = turn.ID
			}
		}
	}
	return nil
}

// Explicit owner query of an existing service, using the history response
// contract and deadline. No process/port scanning or automatic service start.
func listAttachedSessions(ctx context.Context, agent, cwd, cursor string, connection ExternalAttachment) (codexHistoryPage, error) {
	if err := connection.validateDiscovery(agent); err != nil {
		return codexHistoryPage{}, err
	}
	if agent == "claude" {
		return listClaudeLiveSessions(ctx, cwd, cursor)
	}
	if agent == "opencode" {
		page, err := listOpenCodeSessions(ctx, connection, cwd, cursor)
		for _, item := range page.Items {
			item["external_connection"] = connection.ExternalConnection
		}
		describeExternalSessions(page.Items, nil)
		return page, err
	}
	native, err := decodeCodexCursor(cursor)
	if err != nil {
		return codexHistoryPage{}, err
	}
	params := map[string]any{"limit": maxHistoryPage, "sortKey": "updated_at", "sortDirection": "desc", "modelProviders": []string{}, "useStateDbOnly": false}
	if native != "" {
		params["cursor"] = native
	}
	if cwd != "" {
		params["cwd"] = cwd
	}
	raw, err := codexRPCWithAttachment(ctx, cwdOrHome(cwd), "thread/list", params, &connection)
	if err != nil {
		return codexHistoryPage{}, err
	}
	page, err := projectCodexSessions(raw)
	if err != nil {
		return page, err
	}
	// thread/list status describes this actual server, unlike a new query
	// app-server's view of another process's persisted records.
	var response struct {
		Data []struct {
			ID     string `json:"id"`
			Status struct {
				Type string `json:"type"`
			} `json:"status"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &response)
	for i, thread := range response.Data {
		status := "unknown"
		if thread.Status.Type == "active" {
			status = "running"
		}
		if thread.Status.Type == "idle" {
			status = "idle"
		}
		page.Items[i]["execution_status"] = status
		page.Items[i]["status_source"] = "external_protocol"
		page.Items[i]["live_control"] = "conditional"
		page.Items[i]["origin"] = "external_live"
		page.Items[i]["external_connection"] = connection.ExternalConnection
	}
	describeExternalSessions(page.Items, nil)
	return page, nil
}
