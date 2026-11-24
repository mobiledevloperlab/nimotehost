// SPDX-License-Identifier: Apache-2.0
package hostcli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/mobiledevloperlab/nimotehost/internal/host"
)

func TestNameMigrationPreservesStateAndRollsBack(t *testing.T) {
	root := t.TempDir()
	oldDir, newDir := filepath.Join(root, legacyName), filepath.Join(root, "nimotehost")
	if err := os.Mkdir(oldDir, 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte("existing identity, credentials and session records")
	if err := os.WriteFile(filepath.Join(oldDir, "state.fixture"), data, 0600); err != nil {
		t.Fatal(err)
	}
	binary, definition := filepath.Join(root, "legacy-cli"), filepath.Join(root, "legacy-service")
	for _, path := range []string{binary, definition} {
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	running := true
	var commands []string
	call := func(_ context.Context, dir string, req host.RPCRequest) ([]byte, error) {
		if dir != oldDir {
			t.Fatal("wrong authentication directory", dir)
		}
		if req.Path == "/v1/shutdown-if-idle" {
			running = false
			return nil, nil
		}
		if running {
			return []byte(`{}`), nil
		}
		return nil, os.ErrNotExist
	}
	command := func(_ context.Context, path, action string) error {
		if path != binary {
			t.Fatal("wrong legacy executable")
		}
		commands = append(commands, action)
		return nil
	}
	m, err := migrateName(context.Background(), oldDir, newDir, binary, definition, call, command)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(newDir, "state.fixture"))
	if err != nil || string(got) != string(data) {
		t.Fatal("migration lost existing state", err)
	}
	if _, err := os.Stat(oldDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("legacy directory still active")
	}
	if err := m.rollbackWith(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(filepath.Join(oldDir, "state.fixture"))
	if err != nil || string(got) != string(data) {
		t.Fatal("rollback lost state", err)
	}
	if !reflect.DeepEqual(commands, []string{"uninstall", "install"}) {
		t.Fatal("legacy service recovery was incorrect", commands)
	}
}

func TestNameMigrationRefusesActiveSessionsAndConflictingState(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "active sessions", true: "conflicting state"}[conflict], func(t *testing.T) {
			root := t.TempDir()
			oldDir, newDir := filepath.Join(root, legacyName), filepath.Join(root, "nimotehost")
			if err := os.Mkdir(oldDir, 0700); err != nil {
				t.Fatal(err)
			}
			if conflict {
				if err := os.Mkdir(newDir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			binary, definition := filepath.Join(root, "cli"), filepath.Join(root, "service")
			for _, path := range []string{binary, definition} {
				if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			call := func(_ context.Context, _ string, req host.RPCRequest) ([]byte, error) {
				if req.Path == "/v1/status" {
					return []byte(`{}`), nil
				}
				if req.Path != "/v1/shutdown-if-idle" {
					t.Fatal("migration used an unconditional shutdown")
				}
				return nil, errors.New("active sessions")
			}
			commands := 0
			command := func(context.Context, string, string) error { commands++; return nil }
			if _, err := migrateName(context.Background(), oldDir, newDir, binary, definition, call, command); err == nil {
				t.Fatal("unsafe migration succeeded")
			}
			if commands != 0 {
				t.Fatal("legacy service was changed")
			}
			if _, err := os.Stat(oldDir); err != nil {
				t.Fatal("legacy state was moved")
			}
		})
	}
}

func TestLegacyProbeDoesNotCreateASecondIdentity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	dir, err := host.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(filepath.Dir(dir), legacyName)
	if err := os.MkdirAll(old, 0700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"status"}, {"devices", "list"}} {
		if err := runWithExtension(context.Background(), args, strings.NewReader(""), io.Discard, host.Extension{}); err == nil {
			t.Fatal("probe ignored legacy state", args)
		}
		if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("probe created a conflicting new state directory")
		}
	}
}

func TestLegacyAbsoluteSSHPathAndCommandAliasFollowNewBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable aliases")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	oldBinary := filepath.Join(home, ".local", "share", legacyName, legacyName)
	alias := filepath.Join(home, ".local", "bin", legacyName)
	target := filepath.Join(home, ".local", "share", "nimotehost", "nimotehost")
	for _, path := range []string{oldBinary, alias, target} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(oldBinary, []byte("original executable"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new executable"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldBinary, alias); err != nil {
		t.Fatal(err)
	}
	m := &nameMigration{binary: oldBinary, moved: true}
	if err := m.updateAlias(target); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{oldBinary, alias} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != "new executable" {
			t.Fatal("legacy SSH entry was not preserved", path, err)
		}
	}
	got, err := os.ReadFile(oldBinary + ".before-nimotehost")
	if err != nil || string(got) != "original executable" {
		t.Fatal("original executable was not retained", err)
	}
}
