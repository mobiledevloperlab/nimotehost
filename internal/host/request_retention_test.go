// SPDX-License-Identifier: Apache-2.0
package host

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func expiringTestID(now time.Time, char string) string {
	return fmt.Sprintf("v2.%d.%s", now.Unix(), strings.Repeat(char, 64))
}

func receiptPath(l *requestLedger, id string) string {
	hash := sha256.Sum256([]byte(id))
	return filepath.Join(l.dir, hex.EncodeToString(hash[:]))
}

func TestRequestExpiryPrunesWithoutDelayedReplayAfterClockRollback(t *testing.T) {
	m := newTestManager(t)
	now := time.Now().Truncate(time.Second)
	m.requests.now = func() time.Time { return now }
	id := expiringTestID(now, "a")
	calls := 0
	run := func() (bool, error) { calls++; return true, nil }
	for range 2 {
		if _, err := runOnce(m, id, "op", "payload", run); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("retry executed twice")
	}
	if _, err := runOnce(m, "legacy", "op", "payload", run); err != nil {
		t.Fatal(err)
	}
	now = now.Add(requestRetention + time.Second)
	if _, err := runOnce(m, expiringTestID(now, "b"), "op", "payload", run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(receiptPath(m.requests, id)); !os.IsNotExist(err) {
		t.Fatal("expired receipt was not pruned", err)
	}
	if _, err := os.Stat(receiptPath(m.requests, "legacy")); err != nil {
		t.Fatal("legacy receipt was pruned", err)
	}
	if m.requests.count != 2 || m.requests.legacy != 1 {
		t.Fatal("incorrect capacity accounting", m.RequestStorage())
	}
	// Restart with the real clock now behind the persisted retirement watermark.
	l, err := newRequestLedger(m.requests.dir)
	if err != nil {
		t.Fatal(err)
	}
	m.requests = l
	before := calls
	if _, err := runOnce(m, id, "op", "payload", run); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatal("expired key accepted after restart/clock rollback", err)
	}
	if _, err := runOnce(m, "legacy", "op", "payload", run); err != nil || calls != before {
		t.Fatal("legacy replay protection lost", err, calls)
	}
}

func TestRequestExpiryValidatesBeforeSideEffects(t *testing.T) {
	m := newTestManager(t)
	now := time.Now()
	m.requests.now = func() time.Time { return now }
	ids := []string{
		"v2.bad." + strings.Repeat("a", 64),
		"v2.1.short", "v2.01." + strings.Repeat("a", 64),
		expiringTestID(now.Add(-requestRetention-time.Second), "a"),
		expiringTestID(now.Add(requestClockSkew+time.Minute), "a"),
	}
	for _, id := range ids {
		if _, err := runOnce(m, id, "op", nil, func() (bool, error) {
			t.Error("invalid key allowed a side effect")
			return true, nil
		}); err == nil {
			t.Fatal("accepted invalid key", id)
		}
	}
	id, err := NewRequestID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runOnce(m, id, "op", nil, func() (bool, error) { return true, nil }); err != nil {
		t.Fatal("generated key was rejected", err)
	}
}

func TestRequestSweepRetainsUnfinishedAndDamagedReceipts(t *testing.T) {
	m := newTestManager(t)
	now := time.Now().Truncate(time.Second)
	m.requests.now = func() time.Time { return now }
	id := expiringTestID(now, "c")
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := runOnce(m, id, "op", nil, func() (bool, error) {
			close(entered)
			<-release
			return true, nil
		})
		done <- err
	}()
	<-entered
	l := m.requests
	// A partial JSON object must not be interpreted as a prunable receipt.
	damaged := filepath.Join(l.dir, strings.Repeat("d", 64))
	if err := os.WriteFile(damaged, []byte(`{"created_at":1,"complete":`), 0600); err != nil {
		t.Fatal(err)
	}
	l.mu.Lock()
	err := l.sweepLocked(now.Add(requestRetention + time.Second))
	l.mu.Unlock()
	if err != nil {
		t.Error(err)
	}
	for _, path := range []string{damaged, receiptPath(l, id)} {
		if _, err := os.Stat(path); err != nil {
			t.Error("unsafe pruning", path, err)
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	l.mu.Lock()
	err = l.sweepLocked(now.Add(requestRetention + time.Second))
	l.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(receiptPath(l, id)); !os.IsNotExist(err) {
		t.Fatal("finished expired receipt was retained", err)
	}
	if l.count != 1 || l.legacy != 1 {
		t.Fatal("damaged reservation was not counted", m.RequestStorage())
	}
}

func TestRequestSweepMustCommitWatermarkBeforeRemovingReceipts(t *testing.T) {
	m := newTestManager(t)
	now := time.Now().Truncate(time.Second)
	m.requests.now = func() time.Time { return now }
	id := expiringTestID(now, "e")
	if _, err := runOnce(m, id, "op", nil, func() (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
	watermark := filepath.Join(m.requests.dir, "retired-before")
	if err := os.Remove(watermark); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(watermark, 0700); err != nil {
		t.Fatal(err)
	}
	now = now.Add(requestRetention + time.Second)
	if _, err := runOnce(m, expiringTestID(now, "f"), "op", nil, func() (bool, error) {
		t.Error("side effect was allowed after a failed retirement commit")
		return true, nil
	}); err == nil {
		t.Fatal("watermark failure was ignored")
	}
	if _, err := os.Stat(receiptPath(m.requests, id)); err != nil {
		t.Fatal("receipt was removed without a durable retirement watermark", err)
	}
}
