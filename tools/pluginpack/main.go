// SPDX-License-Identifier: Apache-2.0
// pluginpack creates a platform-specific, hash-pinned local plugin bundle.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	sdk "github.com/mobiledevloperlab/nimotehost/pkg/pluginsdk"
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
	out := flag.String("out", "", "new local bundle directory")
	repository := flag.String("repository", "", "GitHub owner/repo for release metadata")
	tag := flag.String("tag", "", "GitHub release tag, e.g. v1.0.0")
	releaseOut := flag.String("release-out", "", "optional new directory for GitHub release assets and catalog-entry.json")
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
	digest := hex.EncodeToString(hash[:])
	fmt.Println("manifest_sha256=" + digest)
	if *releaseOut != "" {
		parts := strings.Split(*repository, "/")
		valid := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)
		if len(parts) != 2 || !valid.MatchString(parts[0]) || !valid.MatchString(parts[1]) || !valid.MatchString(*tag) {
			return fmt.Errorf("valid -repository owner/repo and -tag required for release assets")
		}
		if err = os.Mkdir(*releaseOut, 0700); err != nil {
			return err
		}
		stem := m.ID + "-" + runtime.GOOS + "-" + runtime.GOARCH
		binaryAsset := stem
		if runtime.GOOS == "windows" {
			binaryAsset += ".exe"
		}
		manifestAsset := stem + ".manifest.json"
		if err = os.WriteFile(filepath.Join(*releaseOut, binaryAsset), executable, 0700); err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(*releaseOut, manifestAsset), b, 0600); err != nil {
			return err
		}
		entry := sdk.CatalogEntry{ID: m.ID, Name: m.Capabilities[0].Title, Description: m.Capabilities[0].Description, Repository: *repository, Version: m.Version, Tag: *tag, Assets: map[string]sdk.CatalogAsset{runtime.GOOS + "/" + runtime.GOARCH: {Manifest: manifestAsset, SHA256: digest, Binary: binaryAsset}}}
		raw, err := json.MarshalIndent(entry, "", "  ")
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(*releaseOut, "catalog-entry.json"), raw, 0600); err != nil {
			return err
		}
		fmt.Println("Release assets and metadata-only catalog-entry.json written to " + *releaseOut)
	}
	return nil
}
