// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

// Package hostservice owns operating-system service integration for Nimote Host.
// It deliberately contains no control-plane or transport code.
package hostservice

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const label = "com.nimote.host"

type commandRunner func(context.Context, string, ...string) error

// Config supplies the small platform boundary. Tests can provide temporary
// directories and a recording runner without interacting with a real service
// manager.
type Config struct {
	OS          string
	Executable  string
	ConfigDir   string
	HomeDir     string
	UID         int
	Environment map[string]string
	Run         commandRunner
}

// Service installs and controls one per-user host service.
type Service struct{ config Config }

// NewHostDefault returns the production per-user service integration.
func NewHostDefault() (*Service, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate nimote-host executable: %w", err)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("locate user configuration directory: %w", err)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("locate user home directory: %w", err)
	}
	uid := -1
	if runtime.GOOS != "windows" {
		uid = os.Getuid()
	}
	environment := map[string]string{}
	if runtime.GOOS != "windows" {
		// Capture only the search path, not credentials or the entire shell environment.
		var paths []string
		seen := map[string]bool{}
		for _, path := range filepath.SplitList(os.Getenv("PATH")) {
			if filepath.IsAbs(path) && !seen[path] {
				paths = append(paths, path)
				seen[path] = true
			}
		}
		if len(paths) > 0 {
			environment["PATH"] = strings.Join(paths, string(os.PathListSeparator))
		}
		// The service and SSH CLI must resolve the same state directory.
		if value := os.Getenv("XDG_CONFIG_HOME"); value != "" {
			environment["XDG_CONFIG_HOME"] = value
		}
	}
	return New(Config{OS: runtime.GOOS, Executable: executable, ConfigDir: configDir, HomeDir: homeDir, UID: uid, Environment: environment})
}

// New validates a platform service configuration.
func New(config Config) (*Service, error) {
	if config.OS == "" {
		config.OS = runtime.GOOS
	}
	if config.OS != "linux" && config.OS != "darwin" && config.OS != "windows" {
		return nil, fmt.Errorf("Nimote Host services are not supported on %s", config.OS)
	}
	if !filepath.IsAbs(config.Executable) || strings.ContainsAny(config.Executable, "\x00\r\n") {
		return nil, errors.New("invalid nimote-host executable path")
	}
	if !filepath.IsAbs(config.ConfigDir) || !filepath.IsAbs(config.HomeDir) || strings.ContainsAny(config.ConfigDir+config.HomeDir, "\x00\r\n") {
		return nil, errors.New("user service directories are required")
	}
	if config.UID < 0 && config.OS != "windows" {
		return nil, errors.New("user ID is required")
	}
	for key, value := range config.Environment {
		if (key != "PATH" && key != "XDG_CONFIG_HOME") || strings.ContainsAny(value, "\x00\r\n") {
			return nil, errors.New("invalid service environment")
		}
	}
	if config.Run == nil {
		config.Run = runCommand
	}
	return &Service{config: config}, nil
}

// Installed reports whether this user has a service definition, without changing it.
func (s *Service) Installed() (bool, error) {
	path := s.definitionPath()
	if s.config.OS == "windows" {
		path = filepath.Join(s.config.ConfigDir, "nimote-host", "service.xml")
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("Host service definition is not a regular file")
	}
	return true, nil
}

// Diagnostics describes the native service manager's troubleshooting command.
func (s *Service) Diagnostics() string {
	switch s.config.OS {
	case "linux":
		return "Check systemctl --user status nimote-host.service and journalctl --user -u nimote-host.service -n 50 --no-pager. Without a user service manager, use nimote-host start."
	case "darwin":
		return fmt.Sprintf("Check launchctl print gui/%d/com.nimote.host in the logged-in user's GUI session; use nimote-host start for foreground operation.", s.config.UID)
	default:
		return "Check schtasks.exe /Query /TN NimoteHost /V /FO LIST. Run as the intended logged-in user."
	}
}

// CheckAvailable probes the native manager before setup changes installation files.
func (s *Service) CheckAvailable(ctx context.Context) error {
	var err error
	switch s.config.OS {
	case "linux":
		err = s.run(ctx, "systemctl", "--user", "show-environment")
	case "darwin":
		err = s.run(ctx, "launchctl", "print", fmt.Sprintf("gui/%d", s.config.UID))
	}
	if err != nil {
		return s.managerError("contact the user service manager", err)
	}
	return nil
}

// Stop waits for native service shutdown after an authenticated graceful stop.
func (s *Service) Stop(ctx context.Context) error {
	switch s.config.OS {
	case "linux":
		if err := s.run(ctx, "systemctl", "--user", "stop", s.unitName()); err != nil {
			return s.managerError("stop", err)
		}
	case "darwin":
		if err := s.run(ctx, "launchctl", "bootout", fmt.Sprintf("gui/%d", s.config.UID), s.definitionPath()); err != nil {
			return s.managerError("stop", err)
		}
	}
	// On Windows the authenticated shutdown ends the scheduled task. Setup retries
	// directory replacement until Windows releases the executable handle.
	return nil
}

// Install writes and enables a per-user background service, then starts it.
func (s *Service) Install(ctx context.Context) error {
	if s.config.OS == "windows" {
		return s.installWindows(ctx)
	}
	if err := s.CheckAvailable(ctx); err != nil {
		return err
	}

	path := s.definitionPath()
	_, wasInstalled := os.Stat(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create Nimote Host service directory: %w", err)
	}
	if err := writePrivateFile(path, s.definition()); err != nil {
		return fmt.Errorf("write Nimote Host service definition: %w", err)
	}
	if s.config.OS == "linux" {
		if err := s.run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
			return s.managerError("reload", err)
		}
		if err := s.run(ctx, "systemctl", "--user", "enable", filepath.Base(path)); err != nil {
			return s.managerError("enable", err)
		}
		if err := s.run(ctx, "systemctl", "--user", "restart", filepath.Base(path)); err != nil {
			return s.managerError("restart", err)
		}
		return nil
	}
	if wasInstalled == nil {
		// bootout reports an error when a stale file exists but is not currently
		// loaded. The following bootstrap is the authoritative operation in both
		// cases, so that benign condition is intentionally ignored.
		_ = s.run(ctx, "launchctl", "bootout", fmt.Sprintf("gui/%d", s.config.UID), path)
	}
	if err := s.run(ctx, "launchctl", "bootstrap", fmt.Sprintf("gui/%d", s.config.UID), path); err != nil {
		return s.managerError("start", err)
	}
	return nil
}

// Uninstall stops, disables, and removes only Nimote Host's own service file.
func (s *Service) Uninstall(ctx context.Context) error {
	if s.config.OS == "windows" {
		return s.uninstallWindows(ctx)
	}
	path := s.definitionPath()
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inspect Nimote Host service definition: %w", err)
	}
	if s.config.OS == "linux" {
		if err := s.run(ctx, "systemctl", "--user", "disable", "--now", filepath.Base(path)); err != nil {
			return s.managerError("stop and disable", err)
		}
		if err := s.run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
			return s.managerError("reload", err)
		}
	} else if err := s.run(ctx, "launchctl", "bootout", fmt.Sprintf("gui/%d", s.config.UID), path); err != nil {
		return s.managerError("stop", err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove Nimote Host service definition: %w", err)
	}
	return nil
}

// Restart asks the native manager to restart the existing Nimote Host service.
func (s *Service) Restart(ctx context.Context) error {
	installed, err := s.Installed()
	if err != nil {
		return err
	}
	if !installed {
		return errors.New("Nimote Host service is not installed; run nimote-host install first")
	}
	if s.config.OS == "windows" {
		return s.restartWindows(ctx)
	}
	path := s.definitionPath()
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("Nimote Host service is not installed; run `nimote-host install` first")
		}
		return fmt.Errorf("inspect Nimote Host service definition: %w", err)
	}
	if s.config.OS == "linux" {
		if err := s.run(ctx, "systemctl", "--user", "restart", filepath.Base(path)); err != nil {
			return s.managerError("restart", err)
		}
		return nil
	}
	// Reload the existing definition instead of kickstart: on current macOS,
	// kickstart after an authenticated clean exit can remain in "spawn scheduled"
	// beyond the readiness timeout. Bootstrap also works when the saved definition is unloaded.
	domain := fmt.Sprintf("gui/%d", s.config.UID)
	_ = s.run(ctx, "launchctl", "bootout", domain, path)
	if err := s.run(ctx, "launchctl", "bootstrap", domain, path); err != nil {
		return s.managerError("restart", err)
	}
	return nil
}

func (s *Service) definitionPath() string {
	if s.config.OS == "linux" {
		return filepath.Join(s.config.ConfigDir, "systemd", "user", s.unitName())
	}
	return filepath.Join(s.config.HomeDir, "Library", "LaunchAgents", s.label()+".plist")
}

func (s *Service) definition() []byte {
	if s.config.OS == "linux" {
		description := "Nimote Host"
		var environment strings.Builder
		for _, key := range s.environmentKeys() {
			environment.WriteString("Environment=" + systemdQuote(key+"="+s.config.Environment[key]) + "\n")
		}
		return []byte("[Unit]\nDescription=" + description + "\nAfter=network-online.target\nWants=network-online.target\nStartLimitIntervalSec=60\nStartLimitBurst=3\n\n[Service]\nType=simple\nExecStart=" + systemdQuote(s.config.Executable) + " start\nWorkingDirectory=%h\n" + environment.String() + "Restart=on-failure\nRestartSec=5\n\n[Install]\nWantedBy=default.target\n")
	}
	document := launchdPlistLabel(s.config.Executable, s.label())
	var environment strings.Builder
	environment.WriteString("  <key>WorkingDirectory</key><string>" + xmlEscape(s.config.HomeDir) + "</string>\n")
	if len(s.config.Environment) > 0 {
		environment.WriteString("  <key>EnvironmentVariables</key><dict>\n")
		for _, key := range s.environmentKeys() {
			environment.WriteString("    <key>" + xmlEscape(key) + "</key><string>" + xmlEscape(s.config.Environment[key]) + "</string>\n")
		}
		environment.WriteString("  </dict>\n")
	}
	return []byte(strings.Replace(document, "  <key>RunAtLoad</key>", environment.String()+"  <key>RunAtLoad</key>", 1))
}

func (s *Service) run(ctx context.Context, name string, args ...string) error {
	return s.config.Run(ctx, name, args...)
}

func runCommand(ctx context.Context, name string, args ...string) error {
	// Output captures a bounded stderr tail on ExitError while stdout is never
	// included in diagnostics (show-environment may contain unrelated secrets).
	_, err := exec.CommandContext(ctx, name, args...).Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && len(exit.Stderr) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exit.Stderr)))
	}
	return err
}

func writePrivateFile(path string, value []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".nimote-host-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(value); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func (s *Service) managerError(action string, err error) error {
	return fmt.Errorf("Nimote Host service could not %s: %w. %s", action, err, s.Diagnostics())
}

func (s *Service) environmentKeys() []string {
	keys := make([]string, 0, len(s.config.Environment))
	for key := range s.config.Environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func systemdQuote(value string) string {
	value = strings.ReplaceAll(value, "%", "%%")
	return "\"" + strings.ReplaceAll(strings.ReplaceAll(value, "\\", "\\\\"), "\"", "\\\"") + "\""
}

func launchdPlist(executable string) string { return launchdPlistLabel(executable, label) }
func launchdPlistLabel(executable, serviceLabel string) string {
	// Build the small plist directly so the executable path is XML-escaped by
	// encoding/xml rather than interpolated into an XML document.
	return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\">\n<dict>\n  <key>Label</key>\n  <string>" + xmlEscape(serviceLabel) + "</string>\n  <key>ProgramArguments</key>\n  <array>\n    <string>" + xmlEscape(executable) + "</string>\n    <string>start</string>\n  </array>\n  <key>RunAtLoad</key>\n  <true/>\n  <key>KeepAlive</key>\n  <dict><key>SuccessfulExit</key><false/></dict>\n  <key>ThrottleInterval</key>\n  <integer>30</integer>\n</dict>\n</plist>\n"
}

func xmlEscape(value string) string {
	var builder strings.Builder
	_ = xml.EscapeText(&builder, []byte(value))
	return builder.String()
}

func (s *Service) label() string {
	return label
}
func (s *Service) unitName() string {
	return "nimote-host.service"
}
