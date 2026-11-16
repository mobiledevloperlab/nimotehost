// SPDX-License-Identifier: Apache-2.0
package main

import (
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// GUI distribution targets have native desktop SDKs and CI runners.
func validateGUITarget(targetOS, arch string) error {
	if err := validate(targetOS, arch, "1.0.0"); err != nil {
		return err
	}
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("GUI supports amd64 and arm64; %s/%s is CLI only", targetOS, arch)
	}
	return nil
}

// GUI builds require native desktop SDKs. macOS SDKs support both CPU targets.
// CLI cross compilation remains CGO-free.
func buildGUI(targetOS, arch, version string) error {
	if err := validateGUITarget(targetOS, arch); err != nil {
		return err
	}
	if targetOS != runtime.GOOS || (arch != runtime.GOARCH && targetOS != "darwin") {
		return fmt.Errorf("GUI requires a native %s/%s runner with C compiler and desktop SDK; CLI cross-build succeeded separately", targetOS, arch)
	}
	source := filepath.Join("dist", targetOS+"-"+arch)
	output := filepath.Join("dist", "gui-"+targetOS+"-"+arch)
	if err := os.RemoveAll(output); err != nil {
		return err
	}
	if err := copyTree(source, output); err != nil {
		return err
	}
	for _, asset := range []struct{ source, target string }{
		{"icon.png", "nimotehost.png"}, {"icon.ico", "nimotehost.ico"},
	} {
		if err := copyFile(filepath.Join("cmd", "nimotehost-gui", "assets", asset.source), filepath.Join(output, asset.target)); err != nil {
			return err
		}
	}
	binaryDir := output
	if targetOS == "darwin" {
		binaryDir = filepath.Join(output, "NimoteHost.app", "Contents", "MacOS")
		if err := os.MkdirAll(binaryDir, 0755); err != nil {
			return err
		}
		if err := copyFile(filepath.Join(output, "nimotehost"), filepath.Join(binaryDir, "nimotehost")); err != nil {
			return err
		}
		if err := os.Chmod(filepath.Join(binaryDir, "nimotehost"), 0755); err != nil {
			return err
		}
		resources := filepath.Join(output, "NimoteHost.app", "Contents", "Resources")
		if err := copyFile(filepath.Join("cmd", "nimotehost-gui", "assets", "icon.icns"), filepath.Join(resources, "nimotehost.icns")); err != nil {
			return err
		}
		plist := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>nimotehost-gui</string><key>CFBundleIdentifier</key><string>com.nimotehost.desktop</string><key>CFBundleName</key><string>NimoteHost</string><key>CFBundleIconFile</key><string>nimotehost.icns</string><key>CFBundlePackageType</key><string>APPL</string><key>CFBundleShortVersionString</key><string>` + version + `</string><key>NSHighResolutionCapable</key><true/></dict></plist>`
		if err := os.WriteFile(filepath.Join(output, "NimoteHost.app", "Contents", "Info.plist"), []byte(plist), 0644); err != nil {
			return err
		}
	}
	absolute, err := filepath.Abs(binaryDir)
	if err != nil {
		return err
	}
	name := "nimotehost-gui"
	if targetOS == "windows" {
		name += ".exe"
	}
	flags := "-X " + modulePath + "/internal/host.Version=" + version
	if targetOS == "windows" {
		flags += " -H=windowsgui"
	}
	cmd := guiCommand("build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-ldflags", flags, "-o", filepath.Join(absolute, name), ".")
	cmd.Env = append(cmd.Env, "GOOS="+targetOS, "GOARCH="+arch)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("build Fyne GUI (native C SDK required): %w", err)
	}
	// Check the produced artifact rather than relying on the runner's label.
	if err = verifyGUITarget(filepath.Join(absolute, name), targetOS, arch); err != nil {
		return err
	}
	// Carry the complete resolved GUI dependency manifest and upstream notices.
	cmd = guiCommand("mod", "download", "all")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err = cmd.Run(); err != nil {
		return err
	}
	cmd = guiCommand("list", "-m", "-json", "all")
	raw, err := cmd.Output()
	if err != nil {
		return err
	}
	type guiModule struct {
		Path, Version, Dir string
		Main               bool
		Replace            *guiModule
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	var manifest []map[string]string
	for {
		var mod guiModule
		if err = decoder.Decode(&mod); err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		if mod.Main {
			continue
		}
		if mod.Replace != nil {
			if mod.Path != modulePath {
				return fmt.Errorf("unexpected GUI replacement %s", mod.Path)
			}
			continue
		}
		manifest = append(manifest, map[string]string{"module": mod.Path, "version": mod.Version})
		entries, err := os.ReadDir(mod.Dir)
		if err != nil {
			return fmt.Errorf("read license for %s: %w", mod.Path, err)
		}
		found := false
		for _, entry := range entries {
			upper := strings.ToUpper(entry.Name())
			if !entry.IsDir() && (strings.HasPrefix(upper, "LICENSE") || strings.HasPrefix(upper, "COPYING") || strings.HasPrefix(upper, "NOTICE") || upper == "PATENTS") {
				found = true
				dest := filepath.Join(output, "licenses-gui", filepath.FromSlash(mod.Path+"@"+mod.Version), entry.Name())
				if err = copyFile(filepath.Join(mod.Dir, entry.Name()), dest); err != nil {
					return err
				}
			}
		}
		if !found {
			return fmt.Errorf("GUI dependency %s has no root license; inspect upstream before release", mod.Path)
		}
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(output, "GUI-MODULES.json"), data, 0644); err != nil {
		return err
	}
	if targetOS == "linux" {
		entry := "[Desktop Entry]\nType=Application\nName=NimoteHost\nExec=nimotehost-gui\nIcon=nimotehost\nTerminal=false\nCategories=Development;Utility;\n"
		if err = os.WriteFile(filepath.Join(output, "nimotehost.desktop"), []byte(entry), 0644); err != nil {
			return err
		}
	}
	// The CLI installer verifies this manifest; include every bundled GUI file too.
	if err = writeGUISums(output); err != nil {
		return err
	}
	archive, err := packageNamedDistribution(output, targetOS, arch, version, "nimotehost-gui")
	if err != nil {
		return err
	}
	fmt.Println(archive)
	return nil
}
func guiCommand(args ...string) *exec.Cmd {
	cmd := exec.Command("go", args...)
	cmd.Dir = filepath.Join("cmd", "nimotehost-gui")
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if key != "GOWORK" && key != "GOOS" && key != "GOARCH" && key != "CGO_ENABLED" {
			cmd.Env = append(cmd.Env, item)
		}
	}
	cmd.Env = append(cmd.Env, "GOWORK=off", "CGO_ENABLED=1")
	return cmd
}
func writeGUISums(dir string) error {
	var paths []string
	if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() != "SHA256SUMS" {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		return err
	}
	sort.Strings(paths)
	var sums strings.Builder
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256(b), filepath.ToSlash(rel))
	}
	return os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(sums.String()), 0644)
}

func verifyGUITarget(path, targetOS, arch string) error {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return err
	}
	settings := make(map[string]string)
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	if settings["GOOS"] != targetOS || settings["GOARCH"] != arch || settings["CGO_ENABLED"] != "1" {
		return fmt.Errorf("GUI target mismatch: got %s/%s CGO=%s, want %s/%s CGO=1", settings["GOOS"], settings["GOARCH"], settings["CGO_ENABLED"], targetOS, arch)
	}
	return nil
}
