// SPDX-License-Identifier: Apache-2.0
package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const requestRetention = 7 * 24 * time.Hour
const requestClockSkew = 5 * time.Minute
const requestSweepInterval = time.Hour
const maxRequestRecords = 65536

// NewRequestID creates an expiring, cryptographically random mutation key.
// Reuse it unchanged for retries. After seven days the Host rejects the key,
// allowing its receipt to be removed without permitting delayed execution.
// Clients must check mutation_expiry_v1 before using this format with old Hosts.
func NewRequestID() (string, error) {
	key, err := randomID()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("v2.%d.%s", time.Now().Unix(), key), nil
}

func requestTimestamp(id string) (int64, error) {
	if !strings.HasPrefix(id, "v2.") {
		return 0, nil // Legacy keys retain their original, non-expiring guarantee.
	}
	parts := strings.Split(id, ".")
	if len(parts) != 3 || !validID(parts[2]) {
		return 0, errors.New("invalid expiring request_id")
	}
	stamp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || stamp <= 0 || strconv.FormatInt(stamp, 10) != parts[1] {
		return 0, errors.New("invalid request_id timestamp")
	}
	return stamp, nil
}

// The retirement watermark is committed BEFORE any receipts are removed. It
// never decreases, including after a wall-clock rollback or daemon restart.
// Legacy receipts and in-flight mutations are never evicted.
func (l *requestLedger) sweepLocked(now time.Time) error {
	cutoff := max(l.retiredBefore, now.Add(-requestRetention).Unix())
	if cutoff > l.retiredBefore {
		if err := saveJSON(filepath.Join(l.dir, "retired-before"), cutoff); err != nil {
			return err
		}
		l.retiredBefore = cutoff
	}
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return err
	}
	count, legacy := 0, 0
	var bytes int64
	for _, entry := range entries {
		if !validID(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		path := filepath.Join(l.dir, entry.Name())
		var record requestRecord
		valid := false
		// A damaged receipt stays reserved. Never turn corruption into permission
		// to replay an operation, or follow symlinks while pruning state.
		if info.Mode().IsRegular() && info.Size() <= maxReceipt {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			valid = json.Unmarshal(data, &record) == nil
		}
		if valid && record.CreatedAt > 0 && record.CreatedAt <= l.retiredBefore && !l.inflight[entry.Name()] {
			if err := os.Remove(path); err != nil {
				return err
			}
			continue
		}
		count++
		bytes += info.Size()
		if !valid || record.CreatedAt == 0 {
			legacy++
		}
	}
	l.count, l.bytes, l.legacy = count, bytes, legacy
	l.lastSweep = now
	return nil
}

type RequestStorageStatus struct {
	Receipts         int   `json:"receipts"`
	LegacyReceipts   int   `json:"legacy_receipts"`
	Bytes            int64 `json:"bytes"`
	ReservedBytes    int64 `json:"reserved_bytes"`
	Active           int   `json:"active"`
	ReceiptLimit     int   `json:"receipt_limit"`
	ByteLimit        int64 `json:"byte_limit"`
	RetentionSeconds int64 `json:"retention_seconds"`
	CapacityWarning  bool  `json:"capacity_warning"`
}

// RequestStorage is local operational metadata; it contains no mutation payloads.
// Legacy receipts cannot be safely discarded without changing the old protocol.
func (m *Manager) RequestStorage() *RequestStorageStatus {
	if m == nil || m.requests == nil {
		return nil
	}
	l := m.requests
	l.mu.Lock()
	defer l.mu.Unlock()
	return &RequestStorageStatus{
		Receipts: l.count, LegacyReceipts: l.legacy,
		Bytes: l.bytes, ReservedBytes: l.reserved, Active: l.active,
		ReceiptLimit: maxRequestRecords, ByteLimit: maxRequestStorage,
		RetentionSeconds: int64(requestRetention / time.Second),
		CapacityWarning:  l.count >= maxRequestRecords*4/5 || l.bytes+l.reserved >= maxRequestStorage*4/5,
	}
}
