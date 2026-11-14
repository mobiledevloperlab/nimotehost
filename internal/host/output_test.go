// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRingRetainsLatestOutputAndAbsoluteCursors(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "ring")
	if err != nil {
		t.Fatal(err)
	}
	w := &outputWriter{file: f}
	defer w.Close()
	// Cross the physical end of the ring with nonuniform bytes.
	original := make([]byte, maxOutput+12345)
	for i := range original {
		original[i] = byte(i % 251)
	}
	for offset := 0; offset < len(original); offset += 7919 {
		if _, err := w.Write(original[offset:min(offset+7919, len(original))]); err != nil {
			t.Fatal(err)
		}
	}
	data, start, next, err := w.read(0)
	oldest := int64(len(original) - maxOutput)
	if err != nil || start != oldest || !bytes.Equal(data, original[start:next]) {
		t.Fatalf("start=%d next=%d err=%v", start, next, err)
	}
	cursor := start
	var replay []byte
	for cursor < int64(len(original)) {
		data, _, next, err = w.read(cursor)
		if err != nil || next <= cursor {
			t.Fatalf("cursor=%d err=%v", cursor, err)
		}
		replay = append(replay, data...)
		cursor = next
	}
	if !bytes.Equal(replay, original[oldest:]) {
		t.Fatal("rolling replay differs")
	}
	// New output remains visible after retention fills.
	if _, err = w.Write([]byte("LIVE")); err != nil {
		t.Fatal(err)
	}
	data, start, next, err = w.read(int64(len(original)))
	if err != nil || string(data) != "LIVE" || next != int64(len(original))+4 {
		t.Fatalf("live: %q %d %d %v", data, start, next, err)
	}
}

func TestRingOversizedWriteAndRestart(t *testing.T) {
	m := newTestManager(t)
	id := string(bytes.Repeat([]byte("a"), 64))
	f, err := os.Create(filepath.Join(m.dir, id+".ring"))
	if err != nil {
		t.Fatal(err)
	}
	w := &outputWriter{file: f}
	original := append(bytes.Repeat([]byte("x"), maxOutput+100), []byte("LATEST")...)
	if _, err = w.Write(original); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	m.sessions[id] = &managed{session: Session{ID: id}}
	data, start, next, err := m.OutputPage(id, int64(len(original)-6))
	if err != nil || string(data) != "LATEST" || start != int64(len(original)-6) || next != int64(len(original)) {
		t.Fatalf("%q %d %d %v", data, start, next, err)
	}
	if err = saveJSON(filepath.Join(m.dir, id+".json"), m.sessions[id].session); err != nil {
		t.Fatal(err)
	}
	if err = m.Delete(id); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyPrefixLogRemainsReadable(t *testing.T) {
	m := newTestManager(t)
	id := string(bytes.Repeat([]byte("b"), 64))
	m.sessions[id] = &managed{session: Session{ID: id}}
	if err := os.WriteFile(filepath.Join(m.dir, id+".log"), []byte("legacy"), 0600); err != nil {
		t.Fatal(err)
	}
	data, start, next, err := m.OutputPage(id, 2)
	if err != nil || string(data) != "gacy" || start != 2 || next != 6 {
		t.Fatalf("%q %d %d %v", data, start, next, err)
	}
}

func TestEmptyWriteDoesNotWakeAndClosedWriterRejectsWrites(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "ring")
	if err != nil {
		t.Fatal(err)
	}
	w := &outputWriter{file: f}
	signal := w.signal()
	if n, err := w.Write(nil); n != 0 || err != nil {
		t.Fatalf("empty write: n=%d err=%v", n, err)
	}
	select {
	case <-signal:
		t.Fatal("empty write woke output waiter")
	default:
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if n, err := w.Write([]byte("later")); n != 0 || !errors.Is(err, os.ErrClosed) {
		t.Fatalf("write after close: n=%d err=%v", n, err)
	}
}

func TestRingRecoversOutputStatsAfterHostRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	id := string(bytes.Repeat([]byte("c"), 64))
	record := Session{ID: id, State: "running"}
	if err = saveJSON(filepath.Join(dir, id+".json"), record); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, id+".ring"))
	if err != nil {
		t.Fatal(err)
	}
	w := &outputWriter{file: f}
	if _, err = w.Write(bytes.Repeat([]byte("x"), maxOutput+7)); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	recovered, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	session, err := recovered.Get(id)
	if err != nil || session.OutputBytes != maxOutput+7 || !session.OutputTruncated || session.State != "interrupted" {
		t.Fatal(session, err)
	}
}

func TestDamagedSessionOutputDoesNotBlockHostRecovery(t *testing.T) {
	for _, damage := range []string{"partial", "invalid-header-and-cursor"} {
		t.Run(damage, func(t *testing.T) {
			m := newTestManager(t)
			healthy, err := m.Start(childRequest(t, "output"))
			if err != nil {
				t.Fatal(err)
			}
			awaitSession(t, m, healthy.ID)
			m.Close()
			id := string(bytes.Repeat([]byte("d"), 64))
			if err := saveJSON(filepath.Join(m.dir, id+".json"), Session{ID: id, State: "succeeded"}); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(m.dir, id+".ring"), []byte(damage), 0600); err != nil {
				t.Fatal(err)
			}
			recovered, err := NewManager(m.dir)
			if err != nil {
				t.Fatal("damaged auxiliary data prevented startup:", err)
			}
			defer recovered.Close()
			bad, err := recovered.Get(id)
			if err != nil || bad.Error == "" || bad.State != "succeeded" {
				t.Fatal("damage was not surfaced without changing the task outcome", bad, err)
			}
			if _, _, err := recovered.Output(id, 0); err == nil {
				t.Fatal("damaged output was presented as valid")
			}
			data, _, err := recovered.Output(healthy.ID, 0)
			if err != nil || !bytes.Equal(data, []byte{0, 'h', 'i', 255}) {
				t.Fatal("healthy output was lost", data, err)
			}
		})
	}
}
