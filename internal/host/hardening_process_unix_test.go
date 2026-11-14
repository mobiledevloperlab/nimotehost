// SPDX-License-Identifier: Apache-2.0
//go:build linux || darwin

package host

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRootExitStopsSameGroupDescendants(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()
	req := StartRequest{Kind: "task", Command: "/bin/sh", Args: []string{"-c", `(while :; do printf x >> heartbeat; sleep 0.02; done) & sleep 0.1; exit 0`}, Directory: dir}
	s, err := m.Start(req)
	if err != nil {
		t.Fatal(err)
	}
	awaitSession(t, m, s.ID)
	p := filepath.Join(dir, "heartbeat")
	before, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	after, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() {
		t.Fatal("descendant kept writing after session completion")
	}
}

func TestResourceTimeoutNeverStartsTerminalFallback(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = "app-server" ]; then
 printf 'structured\n' >> launches
else
 printf 'terminal\n' >> launches
fi
exec /bin/sleep 20
`
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	limits := DefaultResourceLimits()
	limits.MaxRuntimeSeconds = 60
	m, err := NewManagerWithLimits(filepath.Join(t.TempDir(), "sessions"), limits)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	result := make(chan error, 1)
	go func() {
		_, err := m.startAuto(StartRequest{Kind: "agent", Agent: "codex", Interaction: "auto", Directory: dir}, 10*time.Second)
		result <- err
	}()
	// Synchronize the fixture before inducing exhaustion. Slow fsync/process
	// startup under race/build load must not expire before the script can run.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "launches")); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("structured fixture never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	m.enforceResources(time.Now().Add(time.Minute), nil)
	select {
	case err = <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("resource-failed startup did not return")
	}
	if !errors.Is(err, errResourceLimit) {
		t.Fatal("resource timeout treated as compatibility fallback", err)
	}
	launches, err := os.ReadFile(filepath.Join(dir, "launches"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(launches)) != "structured" {
		t.Fatal("resource failure relaunched terminal", string(launches))
	}
	records := m.List(0)
	if len(records) != 1 || records[0].Interaction != "structured" || records[0].State != "failed" {
		t.Fatal("resource failure history lost", records)
	}
}
