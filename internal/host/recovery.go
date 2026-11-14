// SPDX-License-Identifier: Apache-2.0
package host

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Recovery is explicit local maintenance on a stopped Host. Unknown/unsafe files
// block repair; request receipts, credentials, outputs and activity are never reset.
type RecoveryFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Issue  string `json:"issue,omitempty"`
	Action string `json:"action,omitempty"`
}
type RecoveryPlan struct {
	ID            string         `json:"id"`
	Files         []RecoveryFile `json:"files"`
	Blocked       bool           `json:"blocked"`
	ResetRevision uint64         `json:"reset_revision"`
}
type RecoveryResult struct {
	Changed bool   `json:"changed"`
	Backup  string `json:"backup,omitempty"`
}

func stoppedStateLock(dir string) (func(), error) {
	control, err := ControlLock(dir)
	if err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "host.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		control.Close()
		return nil, err
	}
	if err = lockState(lock); err != nil {
		lock.Close()
		control.Close()
		return nil, errors.New("stop Host before inspecting or repairing state")
	}
	return func() { lock.Close(); control.Close() }, nil
}
func InspectRecovery(dir string) (RecoveryPlan, error) {
	release, err := stoppedStateLock(dir)
	if err != nil {
		return RecoveryPlan{}, err
	}
	defer release()
	return inspectRecovery(dir)
}
func inspectRecovery(dir string) (RecoveryPlan, error) {
	plan := RecoveryPlan{Files: []RecoveryFile{}}
	sessions := filepath.Join(dir, "sessions")
	entries, err := os.ReadDir(sessions)
	if errors.Is(err, os.ErrNotExist) {
		return finishRecoveryPlan(plan), nil
	}
	if err != nil {
		return plan, err
	}
	count := 0
	badSession := false
	for _, entry := range entries {
		name := entry.Name()
		if name != "events" && !strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".scope") {
			continue
		}
		file := RecoveryFile{Name: name}
		info, err := entry.Info()
		if err != nil {
			return plan, err
		}
		file.Size = info.Size()
		limit := int64(2 << 20)
		if name == "events" {
			limit = 256 << 10
		}
		if strings.HasSuffix(name, ".scope") {
			limit = 16 << 10
		}
		data, err := readPrivateLimit(filepath.Join(sessions, name), limit)
		if err != nil {
			file.Issue = "unsafe, unreadable or oversized state file"
			plan.Blocked = true
			plan.Files = append(plan.Files, file)
			continue
		}
		sum := sha256.Sum256(data)
		file.SHA256 = hex.EncodeToString(sum[:])
		if name == "events" {
			probe := &Manager{dir: sessions}
			if err := probe.loadEvents(); err != nil {
				file.Issue = "invalid event journal"
				file.Action = "rebuild_events"
			} else {
				plan.ResetRevision = max(plan.ResetRevision, probe.revision)
			}
		} else if strings.HasSuffix(name, ".scope") {
			id := strings.TrimSuffix(name, ".scope")
			if !validID(id) {
				file.Issue = "unknown execution scope filename"
				plan.Blocked = true
			} else if _, err := loadSessionScope(sessions, id); err != nil {
				file.Issue = "invalid execution scope metadata"
				file.Action = "quarantine_scope"
				badSession = true
			}
		} else {
			count++
			id := strings.TrimSuffix(name, ".json")
			if !validID(id) {
				file.Issue = "unknown session filename"
				plan.Blocked = true
			} else {
				var record Session
				if err := decodeJSON(data, &record); err != nil || record.ID != id {
					file.Issue = "invalid session metadata"
					file.Action = "quarantine_session"
					badSession = true
				} else {
					plan.ResetRevision = max(plan.ResetRevision, record.Revision)
				}
			}
		}
		plan.Files = append(plan.Files, file)
	}
	if count > 256 {
		plan.Blocked = true
	}
	if plan.ResetRevision >= ^uint64(0)-1024 {
		plan.Blocked = true
	}
	if badSession {
		found := false
		for i := range plan.Files {
			if plan.Files[i].Name == "events" {
				plan.Files[i].Action = "rebuild_events"
				found = true
			}
		}
		if !found {
			plan.Files = append(plan.Files, RecoveryFile{Name: "events", Action: "rebuild_events"})
		}
	}
	// Bind the plan to the existing monotonic reset watermark, too.
	if b, err := readPrivate(filepath.Join(sessions, "event-reset")); err == nil {
		var reset uint64
		if json.Unmarshal(b, &reset) != nil || reset >= ^uint64(0)-1024 {
			plan.Blocked = true
		} else {
			plan.ResetRevision = max(plan.ResetRevision, reset)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		plan.Blocked = true
	}
	return finishRecoveryPlan(plan), nil
}
func finishRecoveryPlan(plan RecoveryPlan) RecoveryPlan {
	sort.Slice(plan.Files, func(i, j int) bool { return plan.Files[i].Name < plan.Files[j].Name })
	b, _ := json.Marshal(plan)
	sum := sha256.Sum256(b)
	plan.ID = hex.EncodeToString(sum[:])
	return plan
}
func ApplyRecovery(dir, planID string) (RecoveryResult, error) {
	result, err := applyRecovery(dir, planID)
	if err != nil && result.Backup != "" {
		return result, fmt.Errorf("recovery incomplete; preserve backup %s: %w", result.Backup, err)
	}
	return result, err
}

func applyRecovery(dir, planID string) (RecoveryResult, error) {
	release, err := stoppedStateLock(dir)
	if err != nil {
		return RecoveryResult{}, err
	}
	defer release()
	plan, err := inspectRecovery(dir)
	if err != nil {
		return RecoveryResult{}, err
	}
	if !validID(planID) || planID != plan.ID {
		return RecoveryResult{}, errors.New("state changed; inspect a fresh recovery plan")
	}
	if plan.Blocked {
		return RecoveryResult{}, errors.New("recovery is blocked by unsafe or unsupported state; preserve files for manual review")
	}
	var bytes uint64
	changes := 0
	for _, f := range plan.Files {
		if f.Action != "" {
			changes++
			bytes += uint64(f.Size)
		}
	}
	if changes == 0 {
		return RecoveryResult{}, nil
	}
	free, err := availableDiskBytes(dir)
	if err != nil {
		return RecoveryResult{}, err
	}
	if free < bytes+(64<<20) {
		return RecoveryResult{}, fmt.Errorf("%w: insufficient space for recovery backups", errResourceLimit)
	}
	id, err := randomID()
	if err != nil {
		return RecoveryResult{}, err
	}
	root := filepath.Join(dir, "recovery")
	if err := privateDir(root); err != nil {
		return RecoveryResult{}, err
	}
	backup := filepath.Join(root, id)
	if err := privateDir(backup); err != nil {
		return RecoveryResult{}, err
	}
	result := RecoveryResult{Backup: backup}
	if err := saveJSON(filepath.Join(backup, "plan.json"), plan); err != nil {
		return result, err
	}
	sessions := filepath.Join(dir, "sessions")
	// Commit durable copies of ALL affected originals before changing any source.
	for _, f := range plan.Files {
		if f.Action == "" || f.SHA256 == "" {
			continue
		}
		data, err := readPrivateLimit(filepath.Join(sessions, f.Name), 2<<20)
		if err != nil {
			return result, err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != f.SHA256 {
			return result, errors.New("state changed while backing up recovery")
		}
		if err := atomicWrite(filepath.Join(backup, f.Name), data); err != nil {
			return result, err
		}
	}
	// Watermark precedes replacements. A crash during repair forces clients to
	// resync; it can never turn an old event cursor into fabricated continuity.
	reset := max(plan.ResetRevision+1, uint64(time.Now().UnixNano()))
	if err := saveJSON(filepath.Join(sessions, "event-reset"), reset); err != nil {
		return result, err
	}
	for _, f := range plan.Files {
		if f.Action == "quarantine_session" || f.Action == "quarantine_scope" {
			if err := os.Rename(filepath.Join(sessions, f.Name), filepath.Join(backup, f.Name+".quarantined")); err != nil {
				return result, err
			}
		}
	}
	if err := syncDirectory(sessions); err != nil {
		return result, err
	}
	if err := saveJSON(filepath.Join(sessions, "events"), []Event{}); err != nil {
		return result, err
	}
	if err := syncDirectory(backup); err != nil {
		return result, err
	}
	result.Changed = true
	return result, nil
}
