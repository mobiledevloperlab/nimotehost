// SPDX-License-Identifier: Apache-2.0
// pluginpack creates a platform-specific, hash-pinned local plugin bundle.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	sdk "github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk"
	"os"
	"path/filepath"
	"runtime"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	binary := flag.String("binary", "", "built executable")
	manifest := flag.String("manifest", "", "manifest template")
	out := flag.String("out", "", "new output directory")
	flag.Parse()
	b, err := os.ReadFile(*manifest)
	if err != nil {
		return err
	}
	var m sdk.Manifest
	if err = json.Unmarshal(b, &m); err != nil {
		return err
	}
	executable, err := os.ReadFile(*binary)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(executable)
	m.SHA256 = hex.EncodeToString(hash[:])
	m.Entrypoint = "plugin"
	if runtime.GOOS == "windows" {
		m.Entrypoint = "plugin.exe"
	}
	m.Platforms = []string{runtime.GOOS + "/" + runtime.GOARCH}
	if err = m.Validate(); err != nil {
		return err
	}
	if err = os.Mkdir(*out, 0700); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(*out, m.Entrypoint), executable, 0700); err != nil {
		return err
	}
	b, err = json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(*out, "manifest.json"), b, 0600); err != nil {
		return err
	}
	hash = sha256.Sum256(b)
	fmt.Println("manifest_sha256=" + hex.EncodeToString(hash[:]))
	return nil
}
