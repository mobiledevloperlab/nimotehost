// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"bytes"
	"errors"
	"io"
	"sync"
)

// os/exec serializes calls to this writer. Limit a single protocol record so
// malformed subprocess output cannot grow host memory without bound.
type agentLines struct {
	ready   <-chan struct{}
	writer  io.Writer
	onError func(error)
	buffer  []byte
}

func (w *agentLines) Write(p []byte) (n int, err error) {
	if w.ready != nil {
		<-w.ready
	}
	defer func() {
		if err != nil && w.onError != nil {
			w.onError(err)
		}
	}()
	n = len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		count := len(p)
		if i >= 0 {
			count = i + 1
		}
		if len(w.buffer)+count > 2<<20 {
			return 0, errors.New("app-server frame exceeds 2 MiB")
		}
		w.buffer = append(w.buffer, p[:count]...)
		p = p[count:]
		if i >= 0 {
			if line := bytes.TrimSpace(w.buffer); len(line) > 0 {
				if _, err := w.writer.Write(append(append([]byte(nil), line...), '\n')); err != nil {
					return 0, err
				}
			}
			w.buffer = nil
		}
	}
	return n, nil
}

// Stderr is diagnostic data, separate from stdout's JSON-RPC records.
type agentDiagnostics struct {
	mu   sync.Mutex
	tail []byte
}

func (d *agentDiagnostics) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := len(p)
	if len(p) > 8192 {
		p = p[len(p)-8192:]
	}
	d.tail = append(d.tail, p...)
	if len(d.tail) > 8192 {
		d.tail = append([]byte(nil), d.tail[len(d.tail)-8192:]...)
	}
	return n, nil
}
func (d *agentDiagnostics) text() string { d.mu.Lock(); defer d.mu.Unlock(); return string(d.tail) }
