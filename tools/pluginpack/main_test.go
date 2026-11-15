// SPDX-License-Identifier: Apache-2.0
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	sdk "github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReleasePackagingKeepsCodeOutOfCatalog(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "program")
	contents := []byte("independent developer executable")
	if err := os.WriteFile(binary, contents, 0700); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "bundle")
	release := filepath.Join(root, "release")
	cmd := exec.Command("go", "run", ".", "-binary", binary, "-manifest", "../../examples/plugin-template/manifest.template.json", "-out", output, "-repository", "developer/my-plugin", "-tag", "v1.0.0", "-release-out", release)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(release, "catalog-entry.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entry sdk.CatalogEntry
	if err = json.Unmarshal(raw, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Repository != "developer/my-plugin" || entry.Tag != "v1.0.0" {
		t.Fatal(entry)
	}
	asset := entry.Assets[runtime.GOOS+"/"+runtime.GOARCH]
	manifest, err := os.ReadFile(filepath.Join(release, asset.Manifest))
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(manifest)
	if asset.SHA256 != hex.EncodeToString(hash[:]) {
		t.Fatal("catalog manifest digest mismatch")
	}
	executable, err := os.ReadFile(filepath.Join(release, asset.Binary))
	if err != nil {
		t.Fatal(err)
	}
	hash = sha256.Sum256(executable)
	var m sdk.Manifest
	if err = json.Unmarshal(manifest, &m); err != nil {
		t.Fatal(err)
	}
	if m.SHA256 != hex.EncodeToString(hash[:]) {
		t.Fatal("binary digest mismatch")
	}
	local, err := os.ReadFile(filepath.Join(output, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(local) != string(manifest) {
		t.Fatal("local/remote packaging differ")
	}
}
