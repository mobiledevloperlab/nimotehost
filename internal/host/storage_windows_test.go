// SPDX-License-Identifier: Apache-2.0
//go:build windows

package host

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateAtomicReplacementPreservesPinnedReader(t *testing.T) {
	dir := t.TempDir()
	if err := privateDir(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "snapshot")
	if err := atomicWrite(path, []byte("before")); err != nil {
		t.Fatal(err)
	}
	f, err := openPrivateReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := atomicWrite(path, []byte("after")); err != nil {
		t.Fatal(err)
	}
	old, err := io.ReadAll(f)
	if err != nil || string(old) != "before" {
		t.Fatal("reader snapshot changed", string(old), err)
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != "after" {
		t.Fatal("replacement missing", string(current), err)
	}
}
