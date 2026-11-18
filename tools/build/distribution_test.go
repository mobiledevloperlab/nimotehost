// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mobiledevloperlab/nimotehost/internal/hostinstall"
)

func distributionFixture(t *testing.T, targetOS string, gui bool) string {
	t.Helper()
	dir := t.TempDir()
	files := []string{"LICENSE", "NOTICE", "BUILD.json", "MODULES.json", "THIRD_PARTY_NOTICES.md", "THIRD_PARTY_NOTICES.zh-CN.md", "licenses/test/LICENSE", "scripts/" + distributionInstaller(targetOS)}
	binary := "nimotehost"
	if targetOS == "windows" {
		binary += ".exe"
	}
	files = append(files, binary)
	if gui {
		files = append(files, "GUI-MODULES.json", "licenses-gui/test/LICENSE")
		switch targetOS {
		case "darwin":
			files = append(files, "NimoteHost.app/Contents/MacOS/nimotehost-gui", "NimoteHost.app/Contents/MacOS/nimotehost", "NimoteHost.app/Contents/Info.plist", "NimoteHost.app/Contents/Resources/nimotehost.icns")
		case "windows":
			files = append(files, "nimotehost-gui.exe")
		case "linux":
			files = append(files, "nimotehost-gui", "nimotehost.png", "nimotehost.desktop")
		}
	}
	for _, name := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeDistributionReadmes(dir, targetOS, "arm64", "2.4.1-dev", gui); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDistributionLayoutAndInstallerCompatibility(t *testing.T) {
	for _, targetOS := range []string{"linux", "darwin", "windows"} {
		for _, gui := range []bool{false, true} {
			name := targetOS + "/cli"
			if gui {
				name = targetOS + "/gui"
			}
			t.Run(name, func(t *testing.T) {
				dir := distributionFixture(t, targetOS, gui)
				if err := validateDistribution(dir, targetOS, gui); err != nil {
					t.Fatal(err)
				}
				if err := writeGUISums(dir); err != nil {
					t.Fatal(err)
				}
				if err := hostinstall.Verify(context.Background(), dir); err != nil {
					t.Fatalf("minimal bundle is not installable: %v", err)
				}
				data, _ := os.ReadFile(filepath.Join(dir, "README.md"))
				if !strings.Contains(string(data), "2.4.1-dev") || !strings.Contains(string(data), targetOS+"/arm64") {
					t.Fatal("wrong bundle identity")
				}
				if gui && !strings.Contains(string(data), "Open the desktop manager") {
					t.Fatal("missing desktop instructions")
				}
				for _, forbidden := range []string{"docs", "CONTRIBUTING.md", ".github", "cmd", "nimotehost.ico"} {
					if _, err := os.Stat(filepath.Join(dir, forbidden)); !os.IsNotExist(err) {
						t.Fatalf("unexpected %s", forbidden)
					}
				}
			})
		}
	}
}

func TestDistributionRejectsExtraFilesAndBrokenDocumentation(t *testing.T) {
	for _, fault := range []string{"source-docs", "other-platform-installer", "missing-license", "broken-markdown-link", "broken-image"} {
		t.Run(fault, func(t *testing.T) {
			dir := distributionFixture(t, "linux", false)
			switch fault {
			case "source-docs":
				os.Mkdir(filepath.Join(dir, "docs"), 0755)
			case "other-platform-installer":
				os.WriteFile(filepath.Join(dir, "scripts", "install.ps1"), []byte("fixture"), 0644)
			case "missing-license":
				os.Remove(filepath.Join(dir, "LICENSE"))
			case "broken-markdown-link":
				os.WriteFile(filepath.Join(dir, "README.md"), []byte("[SDK](pkg/hostclient)"), 0644)
			case "broken-image":
				os.WriteFile(filepath.Join(dir, "README.md"), []byte(`<img src="cmd/assets/logo.png">`), 0644)
			}
			if validateDistribution(dir, "linux", false) == nil {
				t.Fatal("invalid distribution accepted")
			}
		})
	}
}

func TestDesktopExternalIconIsLinuxOnly(t *testing.T) {
	t.Chdir(t.TempDir())
	source := filepath.Join("cmd", "nimotehost-gui", "assets")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "icon.png"), []byte("icon fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, targetOS := range []string{"linux", "darwin", "windows"} {
		dir := t.TempDir()
		if err := copyDesktopIcon(dir, targetOS); err != nil {
			t.Fatal(err)
		}
		entries, _ := os.ReadDir(dir)
		if targetOS == "linux" {
			if len(entries) != 1 || entries[0].Name() != "nimotehost.png" {
				t.Fatal("Linux desktop icon missing")
			}
		} else if len(entries) != 0 {
			t.Fatalf("unneeded icons for %s", targetOS)
		}
	}
}
