// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

//go:build linux || darwin

package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTerminalReconnectInputAndResize(t *testing.T) {
	m := newTestManager(t)
	s, err := m.Start(StartRequest{Kind: "terminal", Command: "/bin/sh", Args: []string{"-c", "read value; printf 'received:%s' \"$value\""}, Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Resize(s.ID, 40, 120); err != nil {
		t.Fatal(err)
	}
	// No client handle owns the process. Reattach by ID and supply terminal input.
	time.Sleep(20 * time.Millisecond)
	if err := m.Input(s.ID, []byte("from-second-device\n")); err != nil {
		t.Fatal(err)
	}
	got := awaitSession(t, m, s.ID)
	if got.State != "succeeded" {
		t.Fatalf("%+v", got)
	}
	data, _, err := m.Output(s.ID, 0)
	if err != nil || !strings.Contains(string(data), "received:from-second-device") {
		t.Fatalf("%q %v", data, err)
	}
}

func TestTerminalInputStreamRealPTY(t *testing.T) {
	m := newTestManager(t)
	s, err := m.Start(StartRequest{Kind: "terminal", Command: "/bin/sh", Args: []string{"-c", "read value; printf 'received:%s' \"$value\""}, Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := m.OpenInputStream(s.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Resize(s.ID, 40, 120); err != nil {
		t.Fatal(err)
	}
	// Split a multibyte code point exactly as a chunked client paste can do.
	data := []byte("中文😀")
	for i, chunk := range [][]byte{data[:1], data[1:], []byte("\n")} {
		if _, err := m.StreamInput(s.ID, "owner", stream, uint64(i+1), chunk); err != nil {
			t.Fatal(err)
		}
	}
	if got := awaitSession(t, m, s.ID); got.State != "succeeded" {
		t.Fatal(got)
	}
	output, _, err := m.Output(s.ID, 0)
	if err != nil || !strings.Contains(string(output), "received:中文😀") {
		t.Fatalf("%q %v", output, err)
	}
	if _, err := m.StreamInput(s.ID, "owner", stream, 4, []byte("late")); err == nil {
		t.Fatal("finished terminal accepted input")
	}
}
func TestUnsafeStateAndSymlinksRejected(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "unsafe")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := privateDir(dir); err == nil {
		t.Fatal("shared state accepted")
	}
	target := filepath.Join(t.TempDir(), "target")
	os.WriteFile(target, []byte(strings.Repeat("a", 64)), 0600)
	safe := filepath.Join(t.TempDir(), "safe")
	os.Mkdir(safe, 0700)
	os.Symlink(target, filepath.Join(safe, "token"))
	if _, err := loadSecret(safe, "token"); err == nil {
		t.Fatal("symlink secret accepted")
	}
}
