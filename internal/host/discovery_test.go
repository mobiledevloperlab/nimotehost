// SPDX-License-Identifier: Apache-2.0
package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPublishExecutableRefreshesStaleDiscovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "discovery.json")
	if err := os.WriteFile(path, []byte(`{"executable":"removed-old-installation.exe"}`), 0600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := publishExecutable(dir); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var record struct {
			Executable string `json:"executable"`
		}
		if err := json.Unmarshal(data, &record); err != nil {
			t.Fatal(err)
		}
		want, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		if record.Executable != want {
			t.Fatalf("discovered %q, want %q", record.Executable, want)
		}
	}
}
