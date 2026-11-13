// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package hostinstall

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func bundle(t *testing.T, version string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{"nimote-host": "binary " + version, "LICENSE": "Apache-2.0", "NOTICE": "Nimote contributors", "BUILD.json": "{}", "MODULES.json": "[]", "docs/readme.md": "fixture " + version}
	var names []string
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var sums strings.Builder
	for _, name := range names {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0644)
		if name == "nimote-host" {
			mode = 0755
		}
		if err := os.WriteFile(path, []byte(files[name]), mode); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256([]byte(files[name])), name)
	}
	if err := os.WriteFile(filepath.Join(root, "SHA256SUMS"), []byte(sums.String()), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestInstallUpgradeAndRollbackPreserveWholeBundle(t *testing.T) {
	ctx := context.Background()
	target := filepath.Join(t.TempDir(), "installation with spaces")
	first := bundle(t, "old")
	tx, err := Prepare(ctx, first, target)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Activate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Verify(ctx, target); err != nil {
		t.Fatal(err)
	}
	second := bundle(t, "new")
	tx, err = Prepare(ctx, second, target)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	if tx.Unchanged(ctx) {
		t.Fatal("different release considered identical")
	}
	if err := tx.Activate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := Verify(ctx, tx.Backup); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(target, "nimote-host"))
	if err != nil || string(data) != "binary new" {
		t.Fatalf("%s %v", data, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(target, "nimote-host"))
	if err != nil || string(data) != "binary old" {
		t.Fatalf("%s %v", data, err)
	}
	if err := Verify(ctx, target); err != nil {
		t.Fatal(err)
	}
}

func TestRejectedBundlesLeaveInstallationUntouched(t *testing.T) {
	for _, name := range []string{"modified", "missing", "unlisted", "traversal", "duplicate", "symlink", "manifest symlink", "incomplete"} {
		t.Run(name, func(t *testing.T) {
			source := bundle(t, "new")
			target := bundle(t, "old")
			switch name {
			case "modified":
				os.WriteFile(filepath.Join(source, "nimote-host"), []byte("tampered"), 0755)
			case "missing":
				os.Remove(filepath.Join(source, "LICENSE"))
			case "unlisted":
				os.WriteFile(filepath.Join(source, "secret.env"), []byte("do not copy"), 0600)
			case "incomplete":
				os.WriteFile(filepath.Join(source, "SHA256SUMS"), []byte(""), 0644)
			case "traversal", "duplicate":
				f, err := os.OpenFile(filepath.Join(source, "SHA256SUMS"), os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				path := "../outside"
				if name == "duplicate" {
					path = "LICENSE"
				}
				fmt.Fprintf(f, "%064x  %s\n", 0, path)
				f.Close()
			case "symlink", "manifest symlink":
				if runtime.GOOS == "windows" {
					t.Skip("symlink creation requires Windows developer privileges")
				}
				path := filepath.Join(source, "NOTICE")
				if name == "manifest symlink" {
					path = filepath.Join(source, "SHA256SUMS")
				}
				os.Remove(path)
				if err := os.Symlink(filepath.Join(target, "NOTICE"), path); err != nil {
					t.Fatal(err)
				}
			}
			_, err := Prepare(context.Background(), source, target)
			if err == nil {
				t.Fatal("unsafe bundle accepted")
			}
			data, err := os.ReadFile(filepath.Join(target, "nimote-host"))
			if err != nil || string(data) != "binary old" {
				t.Fatalf("existing installation changed: %s %v", data, err)
			}
			if _, err := os.Stat(target + ".setup-lock"); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("setup lock was not released", err)
			}
		})
	}
}

func TestExclusiveSetupAndIdempotence(t *testing.T) {
	ctx := context.Background()
	source := bundle(t, "same")
	target := bundle(t, "same")
	tx, err := Prepare(ctx, source, target)
	if err != nil {
		t.Fatal(err)
	}
	if !tx.Unchanged(ctx) {
		t.Fatal("identical installation not recognized")
	}
	if _, err := Prepare(ctx, source, target); err == nil {
		t.Fatal("concurrent setup acquired lock")
	}
	if err := os.WriteFile(filepath.Join(target, "NOTICE"), []byte("corrupt"), 0644); err != nil {
		t.Fatal(err)
	}
	if tx.Unchanged(ctx) {
		t.Fatal("corrupt installation considered healthy")
	}
	if err := tx.Close(); err != nil {
		t.Fatal(err)
	}
	tx, err = Prepare(ctx, source, target)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFirstInstallRollbackAndCancellation(t *testing.T) {
	source := bundle(t, "first")
	target := filepath.Join(t.TempDir(), "host")
	tx, err := Prepare(context.Background(), source, target)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := tx.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Prepare(ctx, source, target); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(target + ".setup-lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

func TestActivateFailureRestoresOldDirectory(t *testing.T) {
	source := bundle(t, "new")
	target := bundle(t, "old")
	tx, err := Prepare(context.Background(), source, target)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	// Simulate staging storage being removed before activation.
	if err := os.RemoveAll(tx.stage); err != nil {
		t.Fatal(err)
	}
	if err := tx.Activate(context.Background()); err == nil {
		t.Fatal("missing staging accepted")
	}
	data, err := os.ReadFile(filepath.Join(target, "nimote-host"))
	if err != nil || string(data) != "binary old" {
		t.Fatalf("old bundle not restored: %s %v", data, err)
	}
}
