// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRejectUnreviewedReleaseInputs(t *testing.T) {
	for _, version := range []string{"", "v1.0", "1.0 -X bad", "1.0\nfoo", "1.0'"} {
		if validate("linux", "amd64", version) == nil {
			t.Fatalf("accepted %q", version)
		}
	}
	if validate("freebsd", "amd64", "1.0.0") == nil || validate("linux", "386", "1.0.0") == nil {
		t.Fatal("accepted unsupported target")
	}
	if validate("windows", "arm64", "1.0.0-rc.1") != nil {
		t.Fatal("rejected valid release")
	}
}
func TestDependencyLicenseBoundary(t *testing.T) {
	main := `{"Path":"github.com/mobiledevloperlab/nimotehost","Main":true}`
	expected := []dependency{{Module: "example.org/lib", Version: "v1.0", Directory: "lib"}}
	for _, data := range []string{main, main + `{"Path":"example.org/lib","Version":"v2.0"}`, main + `{"Path":"example.org/lib","Version":"v1.0","Replace":{"Path":"../private"}}`, main + `{"Path":"example.org/new","Version":"v1.0"}`} {
		if inspectModules([]byte(data), expected) == nil {
			t.Fatalf("accepted unreviewed graph %s", data)
		}
	}
	if err := inspectModules([]byte(main+`{"Path":"example.org/lib","Version":"v1.0"}`), expected); err != nil {
		t.Fatal(err)
	}
}

func TestModuleManifestOmitsLocalPaths(t *testing.T) {
	data, err := moduleManifest([]byte(`{"Path":"github.com/mobiledevloperlab/nimotehost","Main":true,"Dir":"/private/project","GoMod":"/private/project/go.mod"}`))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("private")) || !bytes.Contains(data, []byte("github.com/mobiledevloperlab/nimotehost")) {
		t.Fatalf("unexpected public manifest: %s", data)
	}
}

func TestCopyTreePreservesExecutables(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not use POSIX executable bits")
	}
	source, target := t.TempDir(), t.TempDir()
	for _, name := range []string{"nimotehost", "scripts/install.sh", "README.md"} {
		mode := os.FileMode(0755)
		if name == "README.md" {
			mode = 0644
		}
		path := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := copyTree(source, target); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"nimotehost", "scripts/install.sh", "README.md"} {
		info, err := os.Stat(filepath.Join(target, name))
		if err != nil || (info.Mode()&0111 != 0) != (name != "README.md") {
			t.Fatalf("mode not preserved for %s: %v %v", name, info, err)
		}
	}
}
