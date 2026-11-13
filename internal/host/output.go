// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"sync"
)

const ringHeader = 24

var ringMagic = []byte("NIMOTE-RING-v1\x00\x00")

// The file is a bounded circular byte log. Cursors are absolute bytes, so a
// reconnect can distinguish missing history from an empty live terminal.
type outputWriter struct {
	mu      sync.Mutex
	file    *os.File
	total   int64
	err     error
	closed  bool
	changed chan struct{}
}

func (w *outputWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	if w.err != nil {
		return 0, w.err
	}
	count := len(data)
	if count == 0 {
		return 0, nil
	}
	end := w.total + int64(count)
	if len(data) > maxOutput {
		data = data[len(data)-maxOutput:]
	}
	position := (end - int64(len(data))) % maxOutput
	first := min(len(data), maxOutput-int(position))
	if _, w.err = w.file.WriteAt(data[:first], ringHeader+position); w.err != nil {
		return 0, w.err
	}
	if first < len(data) {
		if _, w.err = w.file.WriteAt(data[first:], ringHeader); w.err != nil {
			return 0, w.err
		}
	}
	header := make([]byte, ringHeader)
	copy(header, ringMagic)
	binary.LittleEndian.PutUint64(header[16:], uint64(end))
	if _, w.err = w.file.WriteAt(header, 0); w.err != nil {
		return 0, w.err
	}
	w.total = end
	if w.changed != nil {
		close(w.changed)
	}
	w.changed = make(chan struct{})
	return count, nil
}
func ringTotal(f *os.File) (int64, error) {
	header := make([]byte, ringHeader)
	n, err := f.ReadAt(header, 0)
	if errors.Is(err, io.EOF) && n == 0 {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !bytes.Equal(header[:16], ringMagic) {
		return 0, errors.New("invalid output log header")
	}
	total := int64(binary.LittleEndian.Uint64(header[16:]))
	if total < 0 {
		return 0, errors.New("invalid output log cursor")
	}
	return total, nil
}
func (w *outputWriter) signal() <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	if w.changed == nil {
		w.changed = make(chan struct{})
	}
	return w.changed
}
func readRing(f *os.File, offset int64) ([]byte, int64, int64, error) {
	total, err := ringTotal(f)
	if err != nil {
		return nil, offset, offset, err
	}
	start := max(offset, total-maxOutput)
	if start > total {
		return nil, start, start, nil
	}
	data := make([]byte, min(int64(64<<10), total-start))
	position := start % maxOutput
	first := min(len(data), maxOutput-int(position))
	if first > 0 {
		if _, err := f.ReadAt(data[:first], ringHeader+position); err != nil {
			return nil, start, start, err
		}
	}
	if first < len(data) {
		if _, err := f.ReadAt(data[first:], ringHeader); err != nil {
			return nil, start, start, err
		}
	}
	return data, start, start + int64(len(data)), nil
}
func (w *outputWriter) read(offset int64) ([]byte, int64, int64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		f, err := os.Open(w.file.Name())
		if err != nil {
			return nil, offset, offset, err
		}
		defer f.Close()
		return readRing(f, offset)
	}
	return readRing(w.file, offset)
}
func (w *outputWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	if w.changed != nil {
		close(w.changed)
		w.changed = nil
	}
	return errors.Join(w.err, w.file.Sync(), w.file.Close())
}
func (w *outputWriter) stats() (int64, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.total, w.total > maxOutput
}
