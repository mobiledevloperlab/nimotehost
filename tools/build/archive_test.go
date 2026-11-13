// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeployableArchives(t *testing.T) {
	for _, targetOS := range []string{"linux", "windows"} {
		t.Run(targetOS, func(t *testing.T) {
			t.Chdir(t.TempDir())
			directory := filepath.Join("dist", targetOS+"-amd64")
			if err := os.MkdirAll(filepath.Join(directory, "scripts"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(directory, "docs"), 0755); err != nil {
				t.Fatal(err)
			}
			name := "nimote-host"
			if targetOS == "windows" {
				name += ".exe"
			}
			os.WriteFile(filepath.Join(directory, name), []byte("binary fixture"), 0755)
			os.WriteFile(filepath.Join(directory, "scripts", "install.sh"), []byte("installer fixture"), 0644)
			os.WriteFile(filepath.Join(directory, "docs", "completed.md"), []byte("deploy fixture"), 0644)
			path, err := packageDistribution(directory, targetOS, "amd64", "1.0.0-dev")
			if err != nil {
				t.Fatal(err)
			}
			first, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			checksum, err := os.ReadFile(path + ".sha256")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(checksum), fmt.Sprintf("%x  ", sha256.Sum256(first))) {
				t.Fatal("wrong archive checksum")
			}
			if _, err := packageDistribution(directory, targetOS, "amd64", "1.0.0-dev"); err != nil {
				t.Fatal(err)
			}
			second, _ := os.ReadFile(path)
			if !bytes.Equal(first, second) {
				t.Fatal("archive is not deterministic")
			}
			found := map[string]bool{}
			if targetOS == "windows" {
				archive, err := zip.OpenReader(path)
				if err != nil {
					t.Fatal(err)
				}
				defer archive.Close()
				for _, file := range archive.File {
					found[file.Name] = true
					if file.Name == "scripts/install.sh" && file.Mode().Perm() != 0755 {
						t.Fatal("installer executable mode lost in zip")
					}
				}
			} else {
				source, err := os.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				defer source.Close()
				compressed, err := gzip.NewReader(source)
				if err != nil {
					t.Fatal(err)
				}
				defer compressed.Close()
				reader := tar.NewReader(compressed)
				for {
					header, err := reader.Next()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					found[header.Name] = true
					if (header.Name == name || header.Name == "scripts/install.sh") && header.Mode&0111 == 0 {
						t.Fatal("executable mode lost")
					}
				}
			}
			if !found[name] || !found["docs/completed.md"] || !found["scripts/install.sh"] || len(found) != 3 {
				t.Fatalf("wrong contents: %v", found)
			}
		})
	}
}
