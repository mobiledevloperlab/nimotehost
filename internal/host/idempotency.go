// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var errRequestConflict = errors.New("request_id was already used for a different operation")
var errRequestUncertain = errors.New("request outcome is uncertain; inspect the session before continuing")

// A reservation without a result is never executed again. Exactly-once execution
// cannot be promised across a process/terminal write and a filesystem transaction.
type requestRecord struct {
	Fingerprint string          `json:"fingerprint"`
	Complete    bool            `json:"complete"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	Uncertain   bool            `json:"uncertain,omitempty"`
	NotFound    bool            `json:"not_found,omitempty"`
}

type requestLedger struct {
	mu       sync.Mutex
	dir      string
	count    int
	active   int
	bytes    int64
	reserved int64
}

const maxReceipt = 2 << 20
const maxRequestStorage = 64 << 20

func newRequestLedger(dir string) (*requestLedger, error) {
	if err := privateDir(dir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	ledger := &requestLedger{dir: dir, count: len(entries)}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		ledger.bytes += info.Size()
	}
	return ledger, nil
}

// Independent mutations never hold this mutex during process or terminal I/O.
// Concurrent retries return an uncertain response and can query/retry later.
func runOnce[T any](m *Manager, id, operation string, payload any, run func() (T, error)) (T, error) {
	var zero T
	if id == "" { // Old protocol-1 clients have no retry guarantee.
		return run()
	}
	if len(id) > 128 {
		return zero, errors.New("request_id exceeds 128 bytes")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return zero, err
	}
	hash := sha256.Sum256(append([]byte(operation+"\x00"), encoded...))
	key := sha256.Sum256([]byte(id))
	ledger := m.requests
	path := filepath.Join(ledger.dir, hex.EncodeToString(key[:]))
	fingerprint := hex.EncodeToString(hash[:])
	ledger.mu.Lock()
	data, err := os.ReadFile(path)
	if err == nil {
		var record requestRecord
		err = json.Unmarshal(data, &record)
		ledger.mu.Unlock()
		if err != nil {
			return zero, errors.Join(errRequestUncertain, fmt.Errorf("read request record: %w", err))
		}
		if record.Fingerprint != fingerprint {
			return zero, errRequestConflict
		}
		if !record.Complete {
			return zero, errRequestUncertain
		}
		if record.Error != "" {
			if record.Uncertain {
				return zero, errors.Join(errRequestUncertain, errors.New(record.Error))
			}
			if record.NotFound {
				return zero, os.ErrNotExist
			}
			return zero, errors.New(record.Error)
		}
		err = json.Unmarshal(record.Result, &zero)
		if err != nil {
			return zero, errors.Join(errRequestUncertain, err)
		}
		return zero, err
	}
	if !errors.Is(err, os.ErrNotExist) {
		ledger.mu.Unlock()
		return zero, errors.Join(errRequestUncertain, err)
	}
	// Never evict a receipt and silently permit a delayed replay.
	if ledger.count >= 65536 {
		ledger.mu.Unlock()
		return zero, errors.New("Host request retention limit reached")
	}
	if ledger.active >= 64 {
		ledger.mu.Unlock()
		return zero, errors.New("too many pending Host mutations")
	}
	if ledger.bytes+ledger.reserved+maxReceipt > maxRequestStorage {
		ledger.mu.Unlock()
		return zero, errors.New("Host request storage limit reached")
	}
	record := requestRecord{Fingerprint: fingerprint}
	if err := saveJSON(path, record); err != nil {
		ledger.mu.Unlock()
		return zero, err
	}
	ledger.count++
	ledger.active++
	initial, _ := json.Marshal(record)
	ledger.bytes += int64(len(initial))
	ledger.reserved += maxReceipt
	ledger.mu.Unlock()
	defer func() {
		ledger.mu.Lock()
		ledger.active--
		ledger.reserved -= maxReceipt
		ledger.mu.Unlock()
	}()
	value, runErr := run()
	record.Result, err = json.Marshal(value)
	if err != nil {
		return zero, errors.Join(errRequestUncertain, err)
	}
	if runErr != nil {
		record.Error = runErr.Error()
		record.Uncertain = errors.Is(runErr, errRequestUncertain)
		record.NotFound = errors.Is(runErr, os.ErrNotExist)
	}
	record.Complete = true
	final, err := json.Marshal(record)
	if err != nil || len(final) > maxReceipt {
		return zero, errors.Join(errRequestUncertain, errors.New("request receipt exceeds storage budget"))
	}
	if err := atomicWrite(path, final); err != nil {
		return zero, errors.Join(errRequestUncertain, err)
	}
	ledger.mu.Lock()
	ledger.bytes += int64(len(final) - len(initial))
	ledger.mu.Unlock()
	return value, runErr
}
