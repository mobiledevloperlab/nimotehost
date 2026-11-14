// SPDX-License-Identifier: Apache-2.0
package host

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// ResourceLimits governs Host admission and supervised process lifetimes. It is
// separate from GUI-owned config.json, so old clients cannot silently erase it.
// Zero runtime means unlimited. CPU/RSS are rejected until containment exists.
type ResourceLimits struct {
	MaxActiveSessions   int    `json:"max_active_sessions"`
	MaxRetainedSessions int    `json:"max_retained_sessions"`
	MaxRuntimeSeconds   int64  `json:"max_runtime_seconds"`
	MinFreeDiskBytes    uint64 `json:"min_free_disk_bytes"`
	CPUPercent          int    `json:"cpu_percent,omitempty"`
	MemoryBytes         uint64 `json:"memory_bytes,omitempty"`
}

func DefaultResourceLimits() ResourceLimits {
	return ResourceLimits{MaxActiveSessions: 32, MaxRetainedSessions: 256, MinFreeDiskBytes: 64 << 20}
}
func (l ResourceLimits) Validate() error {
	if l.MaxActiveSessions < 1 || l.MaxActiveSessions > 32 || l.MaxRetainedSessions < l.MaxActiveSessions || l.MaxRetainedSessions > 256 {
		return errors.New("resource session limits must satisfy 1 <= active <= retained <= 256, active <= 32")
	}
	if l.MaxRuntimeSeconds < 0 || l.MaxRuntimeSeconds > 365*24*60*60 {
		return errors.New("runtime limit must be between zero and one year")
	}
	if l.CPUPercent != 0 || l.MemoryBytes != 0 {
		return errors.New("hard CPU and memory quotas are unsupported; use OS containment")
	}
	return nil
}
func ReadResourceLimits(dir string) (ResourceLimits, error) {
	l := DefaultResourceLimits()
	p := filepath.Join(dir, "resources.json")
	b, err := readPrivate(p)
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return l, err
	}
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		return l, errors.New("resource policy must be an object")
	}
	if err = decodeJSON(b, &l); err != nil {
		return l, err
	}
	return l, l.Validate()
}

var errResourceLimit = errors.New("Host resource limit reached")

func (m *Manager) checkDisk() error {
	if m.limits.MinFreeDiskBytes == 0 {
		return nil
	}
	free, err := availableDiskBytes(m.dir)
	if err != nil {
		return fmt.Errorf("%w: cannot inspect free disk: %v", errResourceLimit, err)
	}
	if free < m.limits.MinFreeDiskBytes {
		return fmt.Errorf("%w: insufficient free disk", errResourceLimit)
	}
	return nil
}

// One bounded monitor belongs to each Manager, never to a client connection.
// Kill and final persistence use the same ownership and completion paths as Cancel.
func (m *Manager) monitorResources() {
	defer close(m.monitorDone)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var lastDisk time.Time
	var diskErr error
	for {
		select {
		case <-m.monitorStop:
			return
		case now := <-ticker.C:
			if now.Sub(lastDisk) >= time.Second {
				diskErr = m.checkDisk()
				lastDisk = now
			}
			m.enforceResources(now, diskErr)
		}
	}
}
func (m *Manager) enforceResources(now time.Time, diskErr error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, item := range m.sessions {
		if item.process == nil {
			continue
		}
		reason := diskErr
		if m.limits.MaxRuntimeSeconds > 0 && now.Sub(item.session.Started) >= time.Duration(m.limits.MaxRuntimeSeconds)*time.Second {
			reason = fmt.Errorf("%w: session runtime exceeded", errResourceLimit)
		}
		if outputErr := item.output.failure(); outputErr != nil {
			reason = fmt.Errorf("session output failed: %w", outputErr)
		}
		if reason == nil {
			continue
		}
		if err := item.process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			slog.Error("resource termination failed", "session", item.session.ID, "error", err)
			continue
		}
		item.resourceError = reason
	}
}

type ResourceStatus struct {
	Limits        ResourceLimits `json:"limits"`
	Active        int            `json:"active"`
	Retained      int            `json:"retained"`
	FreeDiskBytes uint64         `json:"free_disk_bytes"`
	DiskAvailable bool           `json:"disk_available"`
}

func (m *Manager) Resources() ResourceStatus {
	m.mu.Lock()
	status := ResourceStatus{Limits: m.limits, Retained: len(m.sessions)}
	for _, item := range m.sessions {
		if item.process != nil {
			status.Active++
		}
	}
	m.mu.Unlock()
	free, err := availableDiskBytes(m.dir)
	status.FreeDiskBytes, status.DiskAvailable = free, err == nil
	return status
}
