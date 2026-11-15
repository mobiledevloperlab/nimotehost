// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"sync"
)

type HistoryAggregate struct {
	Items            []map[string]any  `json:"items"`
	NextCursor       *string           `json:"next_cursor"`
	Providers        map[string]string `json:"providers"`
	LiveAttach       bool              `json:"live_attach"`
	LiveAttachReason string            `json:"live_attach_reason"`
}

func aggregateHistory(ctx context.Context, cwd, cursor string, installed map[string]AgentInstallation) (HistoryAggregate, error) {
	result := HistoryAggregate{Items: []map[string]any{}, Providers: map[string]string{}, LiveAttachReason: "Installed adapters create their own protocol process; attaching to arbitrary existing processes is unsupported"}
	cursors := map[string]string{}
	if cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil || len(b) > 16384 || json.Unmarshal(b, &cursors) != nil {
			return result, errors.New("invalid aggregate cursor")
		}
	}
	for k := range cursors {
		if k != "codex" && k != "claude" && k != "opencode" {
			return result, errors.New("invalid aggregate cursor provider")
		}
	}
	type part struct {
		agent string
		page  codexHistoryPage
		err   error
	}
	ch := make(chan part, 3)
	var wg sync.WaitGroup
	for _, agent := range []string{"codex", "claude", "opencode"} {
		if !installed[agent].Installed {
			result.Providers[agent] = "adapter_not_installed"
			continue
		}
		if cursors[agent] == "__done" {
			result.Providers[agent] = "exhausted"
			continue
		}
		wg.Add(1)
		go func(agent string) {
			defer wg.Done()
			var p codexHistoryPage
			var e error
			if agent == "codex" {
				p, e = listCodexHistory(ctx, cwd, cursors[agent])
			} else {
				p, e = listACPHistory(ctx, agent, cwd, cursors[agent])
			}
			ch <- part{agent, p, e}
		}(agent)
	}
	wg.Wait()
	close(ch)
	next := map[string]string{}
	more := false
	for _, agent := range []string{"codex", "claude", "opencode"} {
		next[agent] = "__done"
	}
	for p := range ch {
		if p.err != nil {
			result.Providers[p.agent] = "unavailable_or_protocol_unsupported"
			continue
		}
		result.Providers[p.agent] = "history_read"
		result.Items = append(result.Items, p.page.Items...)
		if p.page.NextCursor != nil {
			next[p.agent] = *p.page.NextCursor
			more = true
		}
	}
	sort.SliceStable(result.Items, func(i, j int) bool {
		a, _ := result.Items[i]["updated_at"].(string)
		b, _ := result.Items[j]["updated_at"].(string)
		return a > b
	})
	if more {
		b, _ := json.Marshal(next)
		encoded := base64.RawURLEncoding.EncodeToString(b)
		result.NextCursor = &encoded
	}
	return result, nil
}
func (m *Manager) resumeExisting(req StartRequest) (Session, bool, error) {
	if req.Resume == nil {
		return Session{}, false, nil
	}
	for _, s := range m.List(0) {
		if s.Kind == "agent" && s.State == "running" && s.Agent == req.Agent && s.ConversationID == req.Resume.ConversationID {
			policy := req.ExecutionPolicy
			if policy == "" {
				policy = "confirm"
			}
			current := s.ExecutionPolicy
			if current == "" {
				current = "confirm"
			}
			if !sameWorkspaceDirectory(canonicalDirectory(req.Directory), canonicalDirectory(s.Directory)) || policy != current || req.Task != nil && (s.Task == nil || req.Task.ID != s.Task.ID) {
				return Session{}, false, errors.New("conversation is already active with another workspace, policy or task")
			}
			m.mu.Lock()
			item := m.sessions[s.ID]
			var identity *ExecutionIdentity
			if item != nil && item.scope != nil {
				copied := item.scope.Identity
				identity = &copied
			}
			m.mu.Unlock()
			if (identity == nil) != (req.Identity == nil) || identity != nil && *identity != *req.Identity {
				return Session{}, false, errors.New("conversation is active with another execution identity")
			}
			return s, true, nil
		}
	}
	return Session{}, false, nil
}
