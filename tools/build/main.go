// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

// Command build builds a standalone Host distribution from the module root.
package main

import (
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

const modulePath = "github.com/mobiledevloperlab/nimotehost"

type dependency struct {
	Module    string `json:"module"`
	Version   string `json:"version"`
	Directory string `json:"directory"`
}
type module struct {
	Path      string  `json:"path"`
	Version   string  `json:"version,omitempty"`
	Main      bool    `json:"main,omitempty"`
	Sum       string  `json:"sum,omitempty"`
	GoModSum  string  `json:"goModSum,omitempty"`
	GoVersion string  `json:"goVersion,omitempty"`
	Replace   *module `json:"replace,omitempty"`
}

func main() {
	targetOS := flag.String("os", runtime.GOOS, "linux, darwin or windows")
	arch := flag.String("arch", runtime.GOARCH, "amd64, arm64, or 386 (Linux/Windows CLI only)")
	version := flag.String("version", "2.4.0", "release version")
	gui := flag.Bool("gui", false, "also build the native Fyne desktop bundle (requires CGO and a native SDK)")
	flag.Parse()
	if *gui {
		if err := validateGUITarget(*targetOS, *arch); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if err := build(*targetOS, *arch, *version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *gui {
		if err := buildGUI(*targetOS, *arch, *version); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
func validate(targetOS, arch, version string) error {
	if targetOS != "linux" && targetOS != "darwin" && targetOS != "windows" {
		return errors.New("unsupported OS")
	}
	if arch != "amd64" && arch != "arm64" && !((targetOS == "windows" || targetOS == "linux") && arch == "386") {
		return errors.New("unsupported architecture")
	}
	if !regexp.MustCompile(`^[0-9][A-Za-z0-9._-]*$`).MatchString(version) {
		return errors.New("invalid version")
	}
	return nil
}
func command(args ...string) *exec.Cmd {
	cmd := exec.Command("go", args...)
	// This module must never resolve dependencies through a parent workspace.
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if key != "GOWORK" && key != "GOOS" && key != "GOARCH" && key != "CGO_ENABLED" {
			cmd.Env = append(cmd.Env, item)
		}
	}
	cmd.Env = append(cmd.Env, "GOWORK=off", "CGO_ENABLED=0")
	return cmd
}

// Obfuscation is opt-in so ordinary development and existing CI remain unchanged.
// Only project packages are targeted; the tool fails closed if garble is missing.
func obfuscateBuild(cmd *exec.Cmd) {
	if os.Getenv("NIMOTE_OBFUSCATE") != "1" {
		return
	}
	cmd.Path, cmd.Err = exec.LookPath("garble")
	cmd.Args = append([]string{"garble", "-literals"}, cmd.Args[1:]...)
	cmd.Env = append(cmd.Env, "GOGARBLE="+modulePath)
}

func inspectModules(data []byte, expected []dependency) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	pending := map[string]string{}
	for _, dep := range expected {
		pending[dep.Module] = dep.Version
	}
	foundMain := false
	for {
		var mod module
		if err := decoder.Decode(&mod); err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		if mod.Replace != nil {
			return errors.New("local/replaced dependencies cannot enter a release")
		}
		if mod.Main {
			if mod.Path != modulePath {
				return errors.New("run this tool from the Host module root")
			}
			foundMain = true
			continue
		}
		version, ok := pending[mod.Path]
		if !ok || version != mod.Version {
			return fmt.Errorf("dependency %s@%s needs an updated third_party license record", mod.Path, mod.Version)
		}
		delete(pending, mod.Path)
	}
	if !foundMain || len(pending) != 0 {
		return errors.New("dependency/license records do not match go.mod")
	}
	return nil
}
func copyFile(source, dest string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	mode := os.FileMode(0644)
	if info.Mode()&0111 != 0 {
		mode = 0755
	}
	if err := os.WriteFile(dest, data, mode); err != nil {
		return err
	}
	return os.Chmod(dest, mode)
}
func copyTree(source, dest string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink not allowed: %s", path)
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		return copyFile(path, filepath.Join(dest, relative))
	})
}
func build(targetOS, arch, version string) error {
	if err := validate(targetOS, arch, version); err != nil {
		return err
	}
	data, err := os.ReadFile("third_party/dependencies.json")
	if err != nil {
		return err
	}
	var deps []dependency
	if err := json.Unmarshal(data, &deps); err != nil {
		return err
	}
	list := command("list", "-mod=readonly", "-m", "-json", "all")
	modules, err := list.Output()
	if err != nil {
		return fmt.Errorf("list modules: %w", err)
	}
	if err := inspectModules(modules, deps); err != nil {
		return err
	}
	// Ensure every approved dependency has an actual license before building.
	for _, dep := range deps {
		if _, err := os.ReadFile(filepath.Join("third_party", dep.Directory, "LICENSE")); err != nil {
			return err
		}
	}
	output := filepath.Join("dist", targetOS+"-"+arch)
	// Each distribution must contain only files from this build.
	if err := os.RemoveAll(output); err != nil {
		return err
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		return err
	}
	name := "nimotehost"
	if targetOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(output, name)
	cmd := command("build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-ldflags", "-s -w -X "+modulePath+"/internal/host.Version="+version, "-o", binary, "./cmd/nimotehost")
	obfuscateBuild(cmd)
	cmd.Env = append(cmd.Env, "GOOS="+targetOS, "GOARCH="+arch)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	for _, name := range []string{"LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md", "THIRD_PARTY_NOTICES.zh-CN.md"} {
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		if strings.HasPrefix(name, "THIRD_PARTY_NOTICES") {
			data = []byte(strings.ReplaceAll(string(data), "(third_party/", "(licenses/"))
		}
		if err := os.WriteFile(filepath.Join(output, name), data, 0644); err != nil {
			return err
		}
	}
	for _, name := range []string{distributionInstaller(targetOS)} {
		dest := filepath.Join(output, "scripts", name)
		if err := copyFile(filepath.Join("scripts", name), dest); err != nil {
			return err
		}
		if name == "install.sh" {
			if err := os.Chmod(dest, 0755); err != nil {
				return err
			}
		}
	}
	if err := writeDistributionReadmes(output, targetOS, arch, version, false); err != nil {
		return err
	}
	// Replace distribution notices on each build so removed dependencies don't linger.
	licenses := filepath.Join(output, "licenses")
	if err := os.RemoveAll(licenses); err != nil {
		return err
	}
	if err := copyTree("third_party", licenses); err != nil {
		return err
	}
	for _, name := range []string{"LICENSE", "PATENTS"} {
		source := filepath.Join(runtime.GOROOT(), name)
		if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
			// Homebrew keeps top-level Go notices beside libexec (GOROOT).
			source = filepath.Join(filepath.Dir(runtime.GOROOT()), name)
		}
		if err := copyFile(source, filepath.Join(licenses, "go", name)); err != nil {
			return err
		}
	}
	publicModules, err := moduleManifest(modules)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(output, "MODULES.json"), publicModules, 0644); err != nil {
		return err
	}
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		return err
	}
	binaryData, err := os.ReadFile(binary)
	if err != nil {
		return err
	}
	manifest := struct {
		Version, OS, Arch, SHA256 string
		BuildInfo                 any
	}{version, targetOS, arch, fmt.Sprintf("%x", sha256.Sum256(binaryData)), info}
	data, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(output, "BUILD.json"), append(data, '\n'), 0644); err != nil {
		return err
	}
	if err := validateDistribution(output, targetOS, false); err != nil {
		return err
	}
	var paths []string
	err = filepath.WalkDir(output, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() != "SHA256SUMS" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(paths)
	var sums strings.Builder
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(output, path)
		fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256(data), filepath.ToSlash(relative))
	}
	if err := os.WriteFile(filepath.Join(output, "SHA256SUMS"), []byte(sums.String()), 0644); err != nil {
		return err
	}
	archive, err := packageDistribution(output, targetOS, arch, version)
	if err != nil {
		return err
	}
	fmt.Println(binary)
	fmt.Println(archive)
	return nil
}

func moduleManifest(data []byte) ([]byte, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	modules := []module{}
	for {
		var mod module
		if err := decoder.Decode(&mod); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		modules = append(modules, mod)
	}
	return json.MarshalIndent(modules, "", "  ")
}
