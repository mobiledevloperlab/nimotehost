// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package hostservice

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func TestHostServiceHasIndependentIdentity(t *testing.T) {
	for _, platform := range []string{"linux", "darwin"} {
		t.Run(platform, func(t *testing.T) {
			service := newTestService(t, platform, func(context.Context, string, ...string) error { return nil })
			if err := service.Install(context.Background()); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(service.definitionPath())
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "com.nimote.connect") || !strings.Contains(service.definitionPath(), "host") {
				t.Fatal("service identity overlaps legacy daemon")
			}
		})
	}
}
func TestWindowsHostTaskIsPerUserAndUnprivileged(t *testing.T) {
	var commands []recordedCommand
	service, err := New(Config{OS: "windows", Executable: filepath.Join(t.TempDir(), "nimotehost.exe"), ConfigDir: t.TempDir(), HomeDir: t.TempDir(), Run: func(_ context.Context, name string, args ...string) error {
		commands = append(commands, recordedCommand{name, args})
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(service.config.ConfigDir, "nimotehost", "service.xml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"InteractiveToken", "LeastPrivilege", "<Arguments>start</Arguments>", "<ExecutionTimeLimit>PT0S"} {
		if !strings.Contains(decodeTaskXML(t, data), text) {
			t.Fatal(text)
		}
	}
	if err := service.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := service.Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 4 || commands[0].args[0] != "/Create" || commands[3].args[0] != "/Delete" {
		t.Fatalf("%+v", commands)
	}
}

type recordedCommand struct {
	name string
	args []string
}

func newTestService(t *testing.T, platform string, run commandRunner) *Service {
	t.Helper()
	service, err := New(Config{OS: platform, Executable: filepath.Join(t.TempDir(), "nimotehost"), ConfigDir: t.TempDir(), HomeDir: t.TempDir(), UID: 1000, Run: run})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestLinuxPreflightPreservesServiceOnFailure(t *testing.T) {
	failure := errors.New("user bus unavailable")
	s := newTestService(t, "linux", func(context.Context, string, ...string) error { return failure })
	if err := os.MkdirAll(filepath.Dir(s.definitionPath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.definitionPath(), []byte("existing service"), 0600); err != nil {
		t.Fatal(err)
	}
	err := s.Install(context.Background())
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), "nimotehost start") {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.definitionPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "existing service" {
		t.Fatal(string(data))
	}
}

func TestServiceEnvironmentAndEscaping(t *testing.T) {
	for _, platform := range []string{"linux", "darwin"} {
		t.Run(platform, func(t *testing.T) {
			s := newTestService(t, platform, func(context.Context, string, ...string) error { return nil })
			s.config.Executable = filepath.Join(s.config.HomeDir, `Host %n $AGENT & tools`, "nimotehost")
			s.config.Environment = map[string]string{"PATH": "/opt/Agent tools:/bin", "XDG_CONFIG_HOME": "/home/test/config %n & data"}
			if err := s.Install(context.Background()); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(s.definitionPath())
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			if platform == "linux" {
				for _, want := range []string{`Host %%n $AGENT & tools`, `Environment="PATH=/opt/Agent tools:/bin"`, `Environment="XDG_CONFIG_HOME=/home/test/config %%n & data"`, `WorkingDirectory=`} {
					if !strings.Contains(text, want) {
						t.Fatalf("missing %q in %s", want, text)
					}
				}
			} else {
				decoder := xml.NewDecoder(strings.NewReader(text))
				for {
					_, err := decoder.Token()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				for _, want := range []string{"EnvironmentVariables", "/opt/Agent tools:/bin", "WorkingDirectory", "&amp;"} {
					if !strings.Contains(text, want) {
						t.Fatalf("missing %q in %s", want, text)
					}
				}
			}
			installed, err := s.Installed()
			if err != nil || !installed {
				t.Fatalf("%v %v", installed, err)
			}
		})
	}
}

func TestRejectUnsafeServiceEnvironment(t *testing.T) {
	for _, env := range []map[string]string{{"PATH": "/bin\nExecStart=/bad"}, {"TOKEN": "secret"}, {"XDG_CONFIG_HOME": "/tmp\x00bad"}} {
		_, err := New(Config{OS: "linux", Executable: filepath.Join(t.TempDir(), "host"), ConfigDir: t.TempDir(), HomeDir: t.TempDir(), UID: 1000, Environment: env})
		if err == nil {
			t.Fatal("accepted invalid environment")
		}
	}
}

func TestDefaultServiceCapturesOnlyRequiredEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses the login environment")
	}
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("PATH", "/opt/agent:relative::/usr/bin:/opt/agent")
	t.Setenv("NIMOTE_TEST_SECRET", "must not be saved")
	s, err := NewHostDefault()
	if err != nil {
		t.Fatal(err)
	}
	if s.config.Environment["PATH"] != "/opt/agent:/usr/bin" {
		t.Fatal(s.config.Environment)
	}
	if s.config.Environment["XDG_CONFIG_HOME"] != config || len(s.config.Environment) != 2 {
		t.Fatal(s.config.Environment)
	}
}

func TestRestartWithoutInstallationDoesNotCallManager(t *testing.T) {
	for _, platform := range []string{"linux", "darwin", "windows"} {
		t.Run(platform, func(t *testing.T) {
			called := false
			s := newTestService(t, platform, func(context.Context, string, ...string) error { called = true; return nil })
			if err := s.Restart(context.Background()); err == nil || called {
				t.Fatalf("err=%v manager called=%v", err, called)
			}
		})
	}
}

// Validate using systemd's actual parser, which handles scalar paths differently
// from ExecStart/Environment argument quoting. This never installs or starts a unit.
func TestLinuxDefinitionWithSystemdParser(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux systemd tools")
	}
	analyzer, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("systemd-analyze is not installed")
	}
	s := newTestService(t, "linux", func(context.Context, string, ...string) error { return nil })
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s.config.Executable = filepath.Join(t.TempDir(), "Host %n $AGENT & tools")
	if err := os.Symlink(executable, s.config.Executable); err != nil {
		t.Fatal(err)
	}
	s.config.Environment = map[string]string{"PATH": "/usr/bin:/opt/Agent tools", "XDG_CONFIG_HOME": "/tmp/config %n"}
	if err := os.MkdirAll(filepath.Dir(s.definitionPath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.definitionPath(), s.definition(), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(analyzer, "verify", s.definitionPath()).CombinedOutput()
	if err != nil {
		t.Fatalf("systemd rejected generated unit: %v\n%s", err, output)
	}
}

func decodeTaskXML(t *testing.T, data []byte) string {
	t.Helper()
	if len(data) < 2 || len(data)%2 != 0 || data[0] != 0xff || data[1] != 0xfe {
		t.Fatal("task XML must have a UTF-16LE BOM")
	}
	units := make([]uint16, (len(data)-2)/2)
	for index := range units {
		units[index] = uint16(data[2+index*2]) | uint16(data[3+index*2])<<8
	}
	return string(utf16.Decode(units))
}

func TestWindowsTaskEncodingPreservesUnicode(t *testing.T) {
	document := `<?xml version="1.0" encoding="UTF-16"?><Command>C:\用户 空格\工具🚀\nimotehost.exe</Command>`
	if actual := decodeTaskXML(t, encodeTaskXML(document)); actual != document {
		t.Fatalf("task XML changed: %q", actual)
	}
}

func TestWindowsRegistrationFailureRestoresPreviousDefinition(t *testing.T) {
	failure := errors.New("native registration failed")
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			service := newTestService(t, "windows", func(context.Context, string, ...string) error { return failure })
			path := filepath.Join(service.config.ConfigDir, "nimotehost", "service.xml")
			original := []byte("previous definition")
			if existing {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, original, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := service.Install(context.Background()); !errors.Is(err, failure) {
				t.Fatal(err)
			}
			installed, err := service.Installed()
			if err != nil || installed != existing {
				t.Fatalf("marker=%v err=%v", installed, err)
			}
			if existing {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != string(original) {
					t.Fatalf("previous definition lost: %s %v", data, err)
				}
			}
		})
	}
}

func TestMacRestartReloadsWithoutRewritingEnvironment(t *testing.T) {
	var commands []recordedCommand
	service := newTestService(t, "darwin", func(_ context.Context, name string, args ...string) error {
		commands = append(commands, recordedCommand{name, args})
		return nil
	})
	service.config.Environment = map[string]string{"PATH": "/stable/bin:/usr/bin"}
	if err := service.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(service.definitionPath())
	if err != nil {
		t.Fatal(err)
	}
	commands = nil
	if err := service.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || commands[0].args[0] != "bootout" || commands[1].args[0] != "bootstrap" {
		t.Fatalf("restart commands: %+v", commands)
	}
	after, err := os.ReadFile(service.definitionPath())
	if err != nil || string(before) != string(after) {
		t.Fatal("restart rewrote saved service environment", err)
	}
}

func TestServiceCommandErrorIncludesStderrWithoutStdout(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = runCommand(ctx, executable, "-test.run=TestServiceCommandDiagnosticChild", "--", "--service-diagnostic-child")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("native exit status lost: %v", err)
	}
	if !strings.Contains(err.Error(), "native registration detail") || strings.Contains(err.Error(), "unrelated environment output") {
		t.Fatalf("incorrect diagnostics: %v", err)
	}
}

func TestServiceCommandDiagnosticChild(t *testing.T) {
	for _, arg := range os.Args {
		if arg == "--service-diagnostic-child" {
			fmt.Fprintln(os.Stdout, "unrelated environment output")
			fmt.Fprintln(os.Stderr, "native registration detail")
			os.Exit(7)
		}
	}
}
