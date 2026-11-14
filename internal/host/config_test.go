// SPDX-License-Identifier: Apache-2.0
package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSharedConfigAndLocks(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	c, err := ReadConfig(dir)
	if err != nil || c.Address() != Address {
		t.Fatalf("defaults: %v %v", c, err)
	}
	if err = SaveConfig(dir, Config{49832, "debug"}); err != nil {
		t.Fatal(err)
	}
	c, err = ReadConfig(dir)
	if err != nil || c.Port != 49832 || c.LogLevel != "debug" {
		t.Fatalf("persist: %v %v", c, err)
	}
	for _, bad := range []Config{{0, "info"}, {65536, "info"}, {47831, "invalid"}} {
		if SaveConfig(dir, bad) == nil {
			t.Fatal("accepted invalid config")
		}
	}
	lock, err := ControlLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := ControlLock(dir); err == nil {
		other.Close()
		t.Fatal("concurrent manager allowed")
	}
	lock.Close()
	f, err := os.OpenFile(filepath.Join(dir, "host.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = lockState(f); err != nil {
		t.Fatal(err)
	}
	if idle, err := StateIdle(dir); err != nil || idle {
		t.Fatalf("busy lock reported idle: %v %v", idle, err)
	}
	if SaveConfig(dir, Config{49833, "info"}) == nil {
		t.Fatal("changed config while Host lock held")
	}
}
func TestBoundedRunLog(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	l, err := OpenRunLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	for range 10 {
		if _, err = l.Write([]byte(strings.Repeat("x", 20000))); err != nil {
			t.Fatal(err)
		}
	}
	s, err := ReadRunLog(dir)
	if err != nil || len(s) > logLimit {
		t.Fatalf("unbounded log %d %v", len(s), err)
	}
}

func TestManagementLockSurvivesStateNameMigration(t *testing.T) {
	root := t.TempDir()
	old, next := filepath.Join(root, "nimote-host"), filepath.Join(root, "nimotehost")
	if err := privateDir(old); err != nil {
		t.Fatal(err)
	}
	lock, err := ControlLock(old)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := os.Rename(old, next); err != nil {
		t.Fatal("state rename with management lock", err)
	}
	if competing, err := ControlLock(next); err == nil {
		competing.Close()
		t.Fatal("management lock lost across rename")
	}
	if err := os.Rename(next, old); err != nil {
		t.Fatal("rollback with management lock", err)
	}
	lock.Close()
	again, err := ControlLock(old)
	if err != nil {
		t.Fatal("management lock not released", err)
	}
	again.Close()
}
