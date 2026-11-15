// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	maxActivityEvents = 256
	maxActivityBytes  = 256 << 10
)

// Activity is a small history reference, never a second copy of terminal/tool
// content. OutputOffset points into the existing rolling byte log.
type Activity struct {
	Sequence     uint64    `json:"sequence"`
	Type         string    `json:"type"`
	State        string    `json:"state,omitempty"`
	ReferenceID  string    `json:"reference_id,omitempty"`
	OutputOffset int64     `json:"output_offset"`
	Time         time.Time `json:"time"`
}

type ActivityPage struct {
	Events         []Activity `json:"events"`
	Cursor         uint64     `json:"cursor"`
	ResyncRequired bool       `json:"resync_required"`
}

// Separate from Manager's lock: protocol callbacks already hold the Agent lock.
// No callback may acquire the Manager lock in reverse order.
type activityJournal struct {
	mu      sync.Mutex
	path    string
	events  []Activity
	failure error
}

func (j *activityJournal) cursor() uint64 {
	if j == nil {
		return 0
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.events) == 0 {
		return 0
	}
	return j.events[len(j.events)-1].Sequence
}

func loadActivity(path string) (*activityJournal, error) {
	j := &activityJournal{path: path, events: []Activity{}}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return j, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxActivityBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxActivityBytes {
		return nil, errors.New("session activity exceeds retention limit")
	}
	if err := json.Unmarshal(data, &j.events); err != nil {
		return nil, err
	}
	if len(j.events) > maxActivityEvents {
		return nil, errors.New("too many session activity events")
	}
	var previous uint64
	for _, event := range j.events {
		if event.Sequence <= previous || previous > 0 && event.Sequence != previous+1 {
			return nil, errors.New("invalid session activity sequence")
		}
		previous = event.Sequence
	}
	return j, nil
}

func (j *activityJournal) append(kind, state, reference string, offset int64) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.failure != nil {
		return j.failure
	}
	seq := uint64(1)
	if len(j.events) > 0 {
		seq = j.events[len(j.events)-1].Sequence + 1
	}
	if len(reference) > 128 {
		reference = ""
	}
	event := Activity{Sequence: seq, Type: kind, State: state, ReferenceID: reference, OutputOffset: offset, Time: time.Now().UTC()}
	encodedEvent, err := json.Marshal(event)
	if err != nil {
		return err
	}
	// Reject one oversized event without losing history or advancing its cursor.
	if len(encodedEvent)+2 > maxActivityBytes {
		return errors.New("session activity event exceeds retention limit")
	}
	next := append(append([]Activity(nil), j.events...), event)
	if len(next) > maxActivityEvents {
		next = next[len(next)-maxActivityEvents:]
	}
	encoded, err := json.Marshal(next)
	if err != nil {
		return err
	}
	size, drop := len(encoded), 0
	for size > maxActivityBytes {
		oldest, err := json.Marshal(next[drop])
		if err != nil {
			return err
		}
		size -= len(oldest) + 1 // The removed event and its separating comma.
		drop++
	}
	if drop > 0 {
		next = append([]Activity(nil), next[drop:]...)
		encoded, err = json.Marshal(next)
		if err != nil {
			return err
		}
	}
	if err := atomicWrite(j.path, encoded); err != nil {
		j.failure = err
		slog.Warn("persist session activity", "error", err)
		return err
	}
	j.events = next
	return nil
}

func (j *activityJournal) page(after uint64) (ActivityPage, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	p := ActivityPage{Events: []Activity{}}
	if j.failure != nil {
		return p, j.failure
	}
	if len(j.events) == 0 {
		p.ResyncRequired = after != 0
		return p, nil
	}
	p.Cursor = j.events[len(j.events)-1].Sequence
	p.ResyncRequired = after > p.Cursor || after < j.events[0].Sequence-1
	for _, event := range j.events {
		if p.ResyncRequired || event.Sequence > after {
			p.Events = append(p.Events, event)
		}
	}
	return p, nil
}

func (m *Manager) Activity(id string, after uint64) (ActivityPage, error) {
	m.mu.Lock()
	item := m.sessions[id]
	m.mu.Unlock()
	if item == nil {
		return ActivityPage{}, os.ErrNotExist
	}
	return item.activity.page(after)
}

func (item *managed) recordActivity(kind, state, reference string) {
	if item.activity == nil {
		return
	}
	var offset int64
	if item.output != nil {
		offset, _ = item.output.stats()
	}
	// History failure does not kill an executing task; the read API reports it.
	_ = item.activity.append(kind, state, reference, offset)
}

func (item *managed) agentActivity(kind string, data any) {
	if kind == "turn_started" {
		item.summary = ""
	}
	if value, ok := data.(map[string]string); ok && kind == "text_delta" && len(item.summary) < 8192 {
		item.summary += value["text"]
		if len(item.summary) > 8192 {
			item.summary = item.summary[:8192]
		}
	}
	if value, ok := data.(map[string]string); ok && kind == "message_completed" && value["text"] != "" {
		item.summary = value["text"]
		if len(item.summary) > 8192 {
			item.summary = item.summary[:8192]
		}
	}
	if kind == "turn_completed" && item.edits != nil {
		_ = saveJSON(strings.TrimSuffix(item.edits.path, ".edits")+".summary", map[string]string{"text": item.summary, "source": "agent_self_report"})
	}

	if kind == "tool" && item.edits != nil {
		item.edits.record(data)
	}
	state, reference := "", ""
	if value, ok := data.(map[string]string); ok {
		state, reference = value["status"], value["id"]
	}
	if value, ok := data.(map[string]any); ok {
		state, _ = value["status"].(string)
		reference, _ = value["id"].(string)
	}
	switch kind {
	case "ready":
		state = "idle"
	case "user":
		kind, state = "prompt_accepted", "running"
	case "turn_started":
		state = "running"
	case "turn_completed":
		switch state {
		case "completed", "end_turn":
			state = "completed"
		case "failed":
			state = "failed"
		case "cancelled", "interrupted":
			state = "idle"
		default:
			state = "unknown"
		}
	case "approval":
		state = "needs_approval"
	case "approval_resolved":
		reference, _ = data.(string)
	case "approval_accept_requested", "approval_decline_requested", "approval_auto_accepted":
		state = ""
	case "turn_rejected":
		state = "failed"
	default:
		return // Text, tool output, configuration and diagnostics stay in the log.
	}
	item.recordActivity(kind, state, reference)
	if kind == "turn_completed" && item.onTurnComplete != nil {
		item.onTurnComplete()
	}
}
