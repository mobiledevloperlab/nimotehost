// SPDX-License-Identifier: Apache-2.0
package host

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Review markers acknowledge one observed revision, never future turns.
type InboxReview struct {
	CompletedTurnSequence uint64    `json:"completed_turn_sequence,omitempty"`
	Status                string    `json:"status"`
	Revision              uint64    `json:"revision"`
	Sequence              uint64    `json:"sequence"`
	Updated               time.Time `json:"updated"`
}
type InboxItem struct {
	SummarySource string    `json:"summary_source"`
	Session       Session   `json:"session"`
	View          string    `json:"view"`
	Updated       time.Time `json:"updated"`
	Summary       string    `json:"summary"`
	Revision      string    `json:"revision"`
	Archived      bool      `json:"archived"`
}
type InboxPage struct {
	Items  []InboxItem    `json:"items"`
	Counts map[string]int `json:"counts"`
}

func inboxView(s Session) string {
	switch {
	case s.State == "running" && s.AgentStatus == "needs_approval":
		return "approval"
	case s.State == "failed" || s.State == "interrupted" || s.State == "canceled" || s.AgentStatus == "failed":
		return "failed"
	case s.State == "succeeded" || s.AgentStatus == "completed":
		return "review"
	default:
		return "running"
	}
}
func inboxRevision(s Session) string { return fmt.Sprintf("%d:%d", s.Revision, s.ActivitySequence) }

// allowed is applied before reading auxiliary metadata or computing counts.
func (m *Manager) Inbox(allowed func(string) bool) (InboxPage, error) {
	page := InboxPage{Items: []InboxItem{}, Counts: map[string]int{"approval": 0, "failed": 0, "review": 0, "running": 0, "handled": 0, "archived": 0}}
	for _, s := range m.List(0) {
		if (s.Kind != "agent" && s.Task == nil) || allowed != nil && !allowed(s.ID) {
			continue
		}
		item := InboxItem{Session: s, View: inboxView(s), Updated: s.Started, Revision: inboxRevision(s)}
		if s.Finished != nil {
			item.Updated = *s.Finished
		}
		activity, err := m.Activity(s.ID, 0)
		if err == nil && len(activity.Events) > 0 {
			item.Updated = activity.Events[len(activity.Events)-1].Time
		}
		item.SummarySource = "host_lifecycle"
		item.Summary = s.Error
		if item.Summary == "" {
			item.Summary = s.AgentStatus
		}
		if item.Summary == "" {
			item.Summary = s.State
		}
		if item.View == "review" {
			if data, e := readPrivateLimit(filepath.Join(m.dir, s.ID+".summary"), 64<<10); e == nil {
				var summary map[string]string
				if decodeJSON(data, &summary) == nil && summary["text"] != "" {
					item.Summary = summary["text"]
					item.SummarySource = "agent_self_report"
				}
			}
		}
		data, err := readPrivate(filepath.Join(m.dir, s.ID+".review"))
		if err == nil {
			var r InboxReview
			if err := decodeJSON(data, &r); err != nil {
				return page, err
			}
			// Active approvals always remain visible, even if acknowledged earlier.
			if ((r.Revision == s.Revision && r.Sequence == s.ActivitySequence) || r.CompletedTurnSequence != 0 && r.CompletedTurnSequence == completedInboxTurn(activity)) && item.View != "approval" {
				if r.Status == "handled" || r.Status == "archived" {
					item.View = "handled"
					item.Archived = r.Status == "archived"
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return page, err
		}
		if item.Archived {
			page.Counts["archived"]++
		} else {
			page.Counts[item.View]++
		}
		page.Items = append(page.Items, item)
	}
	sort.Slice(page.Items, func(i, j int) bool {
		if page.Items[i].Updated.Equal(page.Items[j].Updated) {
			return page.Items[i].Session.ID < page.Items[j].Session.ID
		}
		return page.Items[i].Updated.After(page.Items[j].Updated)
	})
	return page, nil
}
func (m *Manager) ReviewInbox(id, revision, status string) error {
	if status != "open" && status != "handled" && status != "archived" {
		return errors.New("invalid review status")
	}
	s, err := m.Get(id)
	if err != nil {
		return err
	}
	if revision != inboxRevision(s) {
		return errRequestConflict
	}
	if inboxView(s) == "approval" && status != "open" {
		return errors.New("resolve the active approval before handling or archiving")
	}
	activity, _ := m.Activity(id, 0)
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.sessions[id]
	if item == nil {
		return os.ErrNotExist
	}
	// Do not acknowledge a process transition that raced the initial read.
	if item.session.Revision != s.Revision || item.activity.cursor() != s.ActivitySequence {
		return errRequestConflict
	}
	return saveJSON(filepath.Join(m.dir, id+".review"), InboxReview{Status: status, Revision: s.Revision, Sequence: s.ActivitySequence, Updated: time.Now().UTC(), CompletedTurnSequence: completedInboxTurn(activity)})
}
func (s *Server) serveInbox(w http.ResponseWriter, r *http.Request, allowed func(string) bool, principal string) {
	if r.URL.Path == "/v1/inbox" && r.Method == http.MethodGet {
		page, err := s.manager.Inbox(allowed)
		if err != nil {
			fail(w, 500, "inbox unavailable")
			return
		}
		view := r.URL.Query().Get("view")
		if view != "" && view != "all" {
			if _, ok := page.Counts[view]; !ok {
				fail(w, 400, "invalid inbox view")
				return
			}
		}
		rows := []InboxItem{}
		for _, item := range page.Items {
			if view == "all" || view == "archived" && item.Archived || !item.Archived && (view == "" || view == item.View) {
				rows = append(rows, item)
			}
		}
		page.Items = rows
		respond(w, 200, page)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/inbox/")
	if !validID(id) {
		fail(w, 404, "unknown inbox record")
		return
	}
	if allowed != nil && !allowed(id) {
		fail(w, 403, "device session scope denied")
		return
	}
	if r.Method != http.MethodPost {
		fail(w, 405, "method not allowed")
		return
	}
	var req struct {
		RequestID string `json:"request_id"`
		Revision  string `json:"revision"`
		Status    string `json:"status"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.RequestID == "" {
		fail(w, 400, "request_id is required")
		return
	}
	_, err := runOnce(s.manager, req.RequestID, principal+"inbox.review/"+id, req, func() (mutationReceipt, error) {
		err := s.manager.ReviewInbox(id, req.Revision, req.Status)
		return mutationReceipt{OK: err == nil, RequestID: req.RequestID, Stage: "accepted"}, err
	})
	if err != nil {
		mutationFailure(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

// A handled completed turn stays handled if its idle protocol process stops.
// New prompts, approvals and outcomes invalidate that acknowledgement. This is
// a user review marker, never a statement that code validation passed.
func completedInboxTurn(page ActivityPage) uint64 {
	var completed uint64
	for _, event := range page.Events {
		switch event.Type {
		case "prompt_accepted", "turn_started", "turn_rejected", "approval":
			completed = 0
		case "turn_completed":
			completed = 0
			if event.State == "completed" {
				completed = event.Sequence
			}
		}
	}
	return completed
}
