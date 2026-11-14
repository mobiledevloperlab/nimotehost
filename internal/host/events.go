// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Event is a durable state-change notification, independent of client lifetime.
// Output remains in the per-session byte log and is retrieved with its cursor.
type Event struct {
	Revision  uint64    `json:"revision"`
	SessionID string    `json:"session_id"`
	State     string    `json:"state"`
	Time      time.Time `json:"time"`
}
type EventPage struct {
	Events         []Event `json:"events"`
	Cursor         uint64  `json:"cursor"`
	ResyncRequired bool    `json:"resync_required"`
}

func (m *Manager) loadEvents() error {
	f, err := os.Open(filepath.Join(m.dir, "events"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (256<<10)+1))
	if err != nil {
		return err
	}
	if len(data) > 256<<10 {
		return errors.New("Host events exceed retention budget")
	}
	if err := json.Unmarshal(data, &m.events); err != nil {
		return err
	}
	if len(m.events) > 1024 {
		return errors.New("too many retained Host events")
	}
	var previous uint64
	for _, event := range m.events {
		if !validID(event.SessionID) || event.Revision <= previous || event.Revision == ^uint64(0) {
			return fmt.Errorf("invalid Host event sequence at revision %d", event.Revision)
		}
		previous = event.Revision
		if event.Revision >= m.revision {
			m.revision = event.Revision + 1
		}
	}
	return nil
}

// Called while holding the manager mutex. Session metadata is authoritative if
// an event write fails; the failure is logged and a full session resync works.
func (m *Manager) emit(s Session) {
	m.events = append(m.events, Event{Revision: s.Revision, SessionID: s.ID, State: s.State, Time: time.Now().UTC()})
	if len(m.events) > 1024 {
		m.events = append([]Event(nil), m.events[len(m.events)-1024:]...)
	}
	if err := saveJSON(filepath.Join(m.dir, "events"), m.events); err != nil {
		slog.Error("persist Host event", "error", err)
	}
}
func (m *Manager) Events(after uint64) EventPage {
	m.mu.Lock()
	defer m.mu.Unlock()
	page := EventPage{Events: make([]Event, 0), Cursor: m.revision}
	page.ResyncRequired = after > page.Cursor || after < m.eventReset
	if len(m.events) > 0 && after > 0 && after < m.events[0].Revision {
		page.ResyncRequired = true
	}
	for _, event := range m.events {
		if event.Revision > after {
			page.Events = append(page.Events, event)
		}
	}
	return page
}
