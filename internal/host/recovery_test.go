// SPDX-License-Identifier: Apache-2.0
package host

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func recoveryFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	sessions := filepath.Join(dir, "sessions")
	if err := privateDir(sessions); err != nil {
		t.Fatal(err)
	}
	return dir, sessions
}
func TestExplicitRecoveryPreservesEvidenceAndReceipts(t *testing.T) {
	dir, sessions := recoveryFixture(t)
	good, bad := strings.Repeat("a", 64), strings.Repeat("b", 64)
	if err := saveJSON(filepath.Join(sessions, good+".json"), Session{ID: good, State: "succeeded", Revision: 9}); err != nil {
		t.Fatal(err)
	}
	broken := []byte("broken metadata")
	if err := os.WriteFile(filepath.Join(sessions, bad+".scope"), []byte("broken scope"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessions, bad+".json"), broken, 0600); err != nil {
		t.Fatal(err)
	}
	events := []byte("broken events")
	if err := os.WriteFile(filepath.Join(sessions, "events"), events, 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{bad + ".ring", bad + ".activity", "requests/sentinel"} {
		p := filepath.Join(sessions, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := InspectRecovery(dir)
	if err != nil || plan.Blocked {
		t.Fatal(plan, err)
	}
	again, err := InspectRecovery(dir)
	if err != nil || plan.ID != again.ID {
		t.Fatal("inspection is not stable", err)
	}
	result, err := ApplyRecovery(dir, plan.ID)
	if err != nil || !result.Changed {
		t.Fatal(result, err)
	}
	for name, want := range map[string][]byte{bad + ".json": broken, bad + ".json.quarantined": broken, bad + ".scope": []byte("broken scope"), bad + ".scope.quarantined": []byte("broken scope"), "events": events} {
		got, err := os.ReadFile(filepath.Join(result.Backup, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("recovery evidence lost", name, err)
		}
	}
	for _, name := range []string{bad + ".ring", bad + ".activity", "requests/sentinel"} {
		got, err := os.ReadFile(filepath.Join(sessions, name))
		if err != nil || string(got) != "preserve" {
			t.Fatal("unrelated state changed", name, err)
		}
	}
	m, err := NewManager(sessions)
	if err != nil {
		t.Fatal("repaired state cannot load", err)
	}
	defer m.Close()
	if _, err := m.Get(good); err != nil {
		t.Fatal("healthy session lost", err)
	}
	if !m.Events(1).ResyncRequired || m.Events(m.revision).ResyncRequired {
		t.Fatal("event reset does not force exactly a cursor resync")
	}
	if _, err := ApplyRecovery(dir, plan.ID); err == nil {
		t.Fatal("stale recovery plan applied twice")
	}
}
func TestRecoveryRefusesRunningHostStalePlanAndUnsafeFiles(t *testing.T) {
	dir, sessions := recoveryFixture(t)
	lock, err := os.OpenFile(filepath.Join(dir, "host.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := lockState(lock); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectRecovery(dir); err == nil {
		t.Fatal("inspected live Host")
	}
	lock.Close()
	p := filepath.Join(sessions, "events")
	if err := os.WriteFile(p, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := InspectRecovery(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyRecovery(dir, plan.ID); err == nil {
		t.Fatal("stale plan modified state")
	}
	if err := os.WriteFile(filepath.Join(sessions, "unknown.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err = InspectRecovery(dir)
	if err != nil || !plan.Blocked {
		t.Fatal("unknown file not blocked", plan, err)
	}
	if _, err := ApplyRecovery(dir, plan.ID); err == nil {
		t.Fatal("blocked plan applied")
	}
	data, _ := os.ReadFile(p)
	if string(data) != "changed" {
		t.Fatal("blocked recovery changed source")
	}
}
func TestHealthyStateRecoveryIsNoOpAndResetPersists(t *testing.T) {
	dir, sessions := recoveryFixture(t)
	if err := saveJSON(filepath.Join(sessions, "events"), []Event{}); err != nil {
		t.Fatal(err)
	}
	plan, err := InspectRecovery(dir)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ApplyRecovery(dir, plan.ID)
	if err != nil || result.Changed || result.Backup != "" {
		t.Fatal("healthy state changed", result, err)
	}
	if err := saveJSON(filepath.Join(sessions, "event-reset"), uint64(100)); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(sessions)
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
	if m.eventReset != 100 || !m.Events(99).ResyncRequired {
		t.Fatal("watermark not retained")
	}
	raw, _ := json.Marshal(uint64(100))
	data, _ := os.ReadFile(filepath.Join(sessions, "event-reset"))
	if !bytes.Equal(raw, data) {
		t.Fatal("loading reset changed it")
	}
}

func TestRecoveryResetExceedsJournalEvenAfterClockRollback(t *testing.T) {
	dir, sessions := recoveryFixture(t)
	high := uint64(1) << 63 // Greater than the current Unix-nanosecond clock.
	id := strings.Repeat("c", 64)
	if err := saveJSON(filepath.Join(sessions, "events"), []Event{{Revision: high, SessionID: id, State: "running"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessions, id+".json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := InspectRecovery(dir)
	if err != nil || plan.Blocked || plan.ResetRevision <= high {
		t.Fatal("journal watermark not retained", plan, err)
	}
	if _, err := ApplyRecovery(dir, plan.ID); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(sessions)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if m.eventReset <= high || !m.Events(high).ResyncRequired {
		t.Fatal("old journal cursor survived reset", m.eventReset)
	}
}
