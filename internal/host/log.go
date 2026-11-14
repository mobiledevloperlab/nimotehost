// SPDX-License-Identifier: Apache-2.0
package host

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const logLimit = 64 << 10

// RunLog retains a bounded native-service log, never terminal or Agent output.
type RunLog struct {
	mu   sync.Mutex
	file *os.File
}

func OpenRunLog(dir string) (*RunLog, error) {
	if err := privateDir(dir); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, "host.log")
	if info, err := os.Lstat(p); err == nil {
		if !info.Mode().IsRegular() {
			return nil, errors.New("log must be a regular file")
		}
		if err = secureFile(p, info); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	return &RunLog{file: f}, nil
}
func (l *RunLog) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	info, err := l.file.Stat()
	if err != nil {
		return 0, err
	}
	if info.Size()+int64(len(b)) > logLimit {
		if err = l.file.Truncate(0); err != nil {
			return 0, err
		}
	}
	original := len(b)
	if len(b) > logLimit {
		b = b[len(b)-logLimit:]
	}
	// Seek under the writer lock: Windows append-only handles cannot truncate.
	if _, err = l.file.Seek(0, io.SeekEnd); err != nil {
		return 0, err
	}
	_, err = l.file.Write(b)
	if err != nil {
		return 0, err
	}
	return original, nil
}
func (l *RunLog) Close() error { return l.file.Close() }
func ReadRunLog(dir string) (string, error) {
	if err := privateDir(dir); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "host.log")
	info, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return "No runtime log yet.", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("invalid log file")
	}
	if err = secureFile(p, info); err != nil {
		return "", err
	}
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, logLimit))
	return string(b), err
}
