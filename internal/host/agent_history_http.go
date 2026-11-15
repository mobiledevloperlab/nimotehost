// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const agentHistoryTimeout = 20 * time.Second
const agentHistoryResponseTimeout = 25 * time.Second
const agentHistoryRPCTimeout = 30 * time.Second

func (s *Server) serveAgentHistory(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/agent-history" {
		if r.Method != http.MethodGet {
			fail(w, 405, "method not allowed")
			return
		}
		caps := capabilities(s.hostID)
		if !caps.Features["agent_history_v1"] {
			fail(w, 404, "native Agent history is unavailable")
			return
		}
		agent := r.URL.Query().Get("agent")
		if agent == "all" {
			directory := r.URL.Query().Get("directory")
			if directory != "" {
				if !filepath.IsAbs(directory) {
					fail(w, 400, "history directory must be absolute")
					return
				}
				if info, e := os.Stat(directory); e != nil || !info.IsDir() {
					fail(w, 400, "history directory is unavailable")
					return
				}
			}
			ctx, cancel := context.WithTimeout(r.Context(), agentHistoryTimeout)
			defer cancel()
			page, err := aggregateHistory(ctx, directory, r.URL.Query().Get("cursor"), caps.Agents)
			if err != nil {
				fail(w, 400, "invalid aggregate history request")
				return
			}
			respond(w, 200, page)
			return
		}
		if agent == "" {
			agent = "codex"
			if !caps.Features["codex_history_v1"] {
				if caps.Features["opencode_history_v1"] {
					agent = "opencode"
				} else if caps.Features["claude_history_v1"] {
					agent = "claude"
				}
			}
		}
		feature := map[string]string{"codex": "codex_history_v1", "opencode": "opencode_history_v1", "claude": "claude_history_v1"}[agent]
		if feature == "" || !caps.Features[feature] {
			fail(w, 400, "native history is not supported for this Agent")
			return
		}
		directory := r.URL.Query().Get("directory")
		if directory != "" {
			if !filepath.IsAbs(directory) {
				fail(w, 400, "history directory must be absolute")
				return
			}
			info, err := os.Stat(directory)
			if err != nil || !info.IsDir() {
				fail(w, 400, "history directory is unavailable")
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), agentHistoryTimeout)
		defer cancel()
		var err error
		var page codexHistoryPage
		if agent == "codex" {
			page, err = listCodexHistory(ctx, directory, r.URL.Query().Get("cursor"))
		} else {
			page, err = listACPHistory(ctx, agent, directory, r.URL.Query().Get("cursor"))
		}
		if err != nil {
			fail(w, 503, agent+" history is temporarily unavailable")
			return
		}
		respond(w, 200, page)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/agent-history/") {
		if r.Method != http.MethodGet {
			fail(w, 405, "method not allowed")
			return
		}
		caps := capabilities(s.hostID)
		if !caps.Features["agent_history_v1"] {
			fail(w, 404, "native Agent history is unavailable")
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/v1/agent-history/")
		if id == "" || strings.Contains(id, "/") {
			fail(w, 404, "conversation not found")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), agentHistoryTimeout)
		defer cancel()
		agent := r.URL.Query().Get("agent")
		if agent == "" {
			agent = "codex"
		}
		directory := r.URL.Query().Get("directory")
		var page map[string]any
		var err error
		if agent == "codex" && caps.Features["codex_history_v1"] {
			page, err = readCodexHistory(ctx, id, r.URL.Query().Get("cursor"))
		} else if (agent == "opencode" && caps.Features["opencode_history_v1"]) || (agent == "claude" && caps.Features["claude_history_v1"]) {
			if !filepath.IsAbs(directory) {
				err = errors.New("history directory must be absolute")
			} else if info, statErr := os.Stat(directory); statErr != nil || !info.IsDir() {
				err = errors.New("history directory is unavailable")
			} else {
				page, err = readACPHistory(ctx, agent, id, directory)
			}
		} else {
			err = errors.New("unsupported provider")
		}
		if err != nil {
			fail(w, 404, "conversation is unavailable")
			return
		}
		respond(w, 200, page)
		return
	}
}
