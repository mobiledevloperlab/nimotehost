// SPDX-License-Identifier: Apache-2.0
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenamePreservesPreferencesAndDoesNotReplaceNewChoices(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "com.nimote.host.desktop", "preferences.json")
	target := filepath.Join(root, "com.nimotehost.desktop", "preferences.json")
	if err := os.MkdirAll(filepath.Dir(old), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"language":"zh-Hans","theme":"Dark","linux-tray":true}`)
	if err := os.WriteFile(old, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := copyLegacyPreferences(root); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != string(original) {
		t.Fatal("preferences were lost", err)
	}
	if err := os.WriteFile(target, []byte(`{"theme":"Light"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := copyLegacyPreferences(root); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(target)
	if string(got) != `{"theme":"Light"}` {
		t.Fatal("new preferences were overwritten")
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("original preference backup was removed")
	}
}
