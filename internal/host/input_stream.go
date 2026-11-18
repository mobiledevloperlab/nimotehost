// SPDX-License-Identifier: Apache-2.0
package host

import (
	"crypto/sha256"
	"errors"
)

const maxInputStreams = 64

type terminalInputStream struct {
	principal string
	sequence  uint64
	digest    [32]byte
	failure   error
	used      uint64
}

type inputStreamReceipt struct {
	OK       bool   `json:"ok"`
	Stage    string `json:"stage"`
	StreamID string `json:"stream_id"`
	Sequence uint64 `json:"sequence"`
}

// Streams are allocated by the daemon, never reconstructed from client input.
// Losing a stream (restart or bounded eviction) makes every old frame invalid.
// This is safe because terminal processes do not survive a daemon restart.
func (m *Manager) OpenInputStream(id, principal string) (string, error) {
	item, err := m.inputSession(id)
	if err != nil {
		return "", err
	}
	streamID, err := randomID()
	if err != nil {
		return "", err
	}
	item.inputMu.Lock()
	defer item.inputMu.Unlock()
	if item.inputStreams == nil {
		item.inputStreams = make(map[string]*terminalInputStream)
	}
	if len(item.inputStreams) >= maxInputStreams {
		var oldest string
		for key, stream := range item.inputStreams {
			if oldest == "" || stream.used < item.inputStreams[oldest].used {
				oldest = key
			}
		}
		delete(item.inputStreams, oldest)
	}
	item.inputClock++
	item.inputStreams[streamID] = &terminalInputStream{principal: principal, used: item.inputClock}
	return streamID, nil
}

// Only the next sequence can write. The latest receipt can be queried again
// with the same bytes; older frames and changed duplicates are never replayed.
// A failed/partial write poisons the stream, including all subsequent frames.
// inputMu also serializes writes from legacy clients and other devices.
func (m *Manager) StreamInput(id, principal, streamID string, sequence uint64, data []byte) (inputStreamReceipt, error) {
	var receipt inputStreamReceipt
	if !validID(streamID) || sequence == 0 || sequence > 1<<53-1 {
		return receipt, errors.New("invalid terminal input stream or sequence")
	}
	item, err := m.inputSession(id)
	if err != nil {
		return receipt, err
	}
	item.inputMu.Lock()
	defer item.inputMu.Unlock()
	stream := item.inputStreams[streamID]
	if stream == nil || stream.principal != principal {
		return receipt, errors.New("terminal input stream expired; reconnect before typing")
	}
	digest := sha256.Sum256(data)
	if sequence == stream.sequence && digest != stream.digest {
		return receipt, errRequestConflict
	}
	if stream.failure != nil {
		return receipt, stream.failure
	}
	if sequence != stream.sequence+1 && sequence != stream.sequence {
		return receipt, errors.New("terminal input sequence is stale or out of order")
	}
	item.inputClock++
	stream.used = item.inputClock
	if sequence == stream.sequence+1 {
		stream.sequence, stream.digest = sequence, digest
		if err := m.writeInput(item, data); err != nil {
			stream.failure = errors.Join(errRequestUncertain, err)
			return receipt, stream.failure
		}
	}
	return inputStreamReceipt{OK: true, Stage: "terminal_written", StreamID: streamID, Sequence: sequence}, nil
}
