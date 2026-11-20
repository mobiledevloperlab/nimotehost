// SPDX-License-Identifier: Apache-2.0
//go:build windows

package host

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func registryToolString(root registry.Key, key, name string, base []string) string {
	return registryToolStringDepth(root, key, name, base, 0)
}

const windowsMachineEnvironment = `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`

func registryToolStringDepth(root registry.Key, key, name string, base []string, depth int) string {
	if depth > 8 {
		return ""
	}
	k, err := registry.OpenKey(root, key, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	value, kind, err := k.GetStringValue(name)
	if err != nil {
		return ""
	}
	if kind == registry.EXPAND_SZ {
		// Expand against the environment being built, not stale process PATH.
		var expanded strings.Builder
		for len(value) > 0 {
			before, rest, found := strings.Cut(value, "%")
			expanded.WriteString(before)
			if !found {
				break
			}
			key, after, closed := strings.Cut(rest, "%")
			if !closed {
				expanded.WriteString("%" + rest)
				break
			}
			v := ""
			// PATH may reference a variable installed after Host was launched,
			// such as PNPM_HOME or NVM_SYMLINK. Resolve only referenced values;
			// never import the registry's entire environment or credentials.
			if !strings.EqualFold(key, "PATH") {
				v = registryToolStringDepth(registry.CURRENT_USER, `Environment`, key, base, depth+1)
				if v == "" {
					v = registryToolStringDepth(registry.LOCAL_MACHINE, windowsMachineEnvironment, key, base, depth+1)
				}
			}
			if v == "" {
				v = environmentValue(base, key)
			}
			if v != "" {
				expanded.WriteString(v)
			} else {
				expanded.WriteString("%" + key + "%")
			}
			value = after
		}
		value = expanded.String()
	}
	return value
}

func probeToolEnvironment(_ context.Context, base []string, _ bool) ([]string, string) {
	// Windows tools use the user/system environment rather than POSIX profiles.
	// Read the registry again so installs after daemon startup become visible.
	systemPath := registryToolString(registry.LOCAL_MACHINE, windowsMachineEnvironment, "Path", base)
	userBase := replaceEnvironment(base, "PATH", systemPath)
	userPath := registryToolString(registry.CURRENT_USER, `Environment`, "Path", userBase)
	if userPath == "" && systemPath == "" {
		return nil, "windows_inherited"
	}
	env := []string{"PATH=" + systemPath + ";" + userPath}
	for _, name := range toolRuntimeVariables {
		value := registryToolString(registry.CURRENT_USER, `Environment`, name, base)
		if value == "" {
			value = registryToolString(registry.LOCAL_MACHINE, windowsMachineEnvironment, name, base)
		}
		if value != "" {
			env = append(env, name+"="+value)
		}
	}
	return env, "windows_registry"
}

func platformToolDirectories(base []string) []string {
	var paths []string
	for _, entry := range []struct{ key, suffix string }{
		{"SystemRoot", "System32"}, {"SystemRoot", ""}, {"APPDATA", "npm"},
		{"LOCALAPPDATA", `Microsoft\WindowsApps`}, {"LOCALAPPDATA", `Volta\bin`},
		{"LOCALAPPDATA", `Programs\Git\cmd`}, {"ProgramFiles", `Git\cmd`},
		{"ProgramFiles", "nodejs"}, {"ProgramFiles", `PowerShell\7`},
		{"USERPROFILE", `scoop\shims`}, {"USERPROFILE", `.cargo\bin`},
		{"USERPROFILE", `.local\bin`}, {"VOLTA_HOME", "bin"}, {"PNPM_HOME", ""},
	} {
		if root := environmentValue(base, entry.key); filepath.IsAbs(root) {
			paths = append(paths, filepath.Join(root, entry.suffix))
		}
	}
	return paths
}

func platformRegisteredTool(name string) string {
	if filepath.Ext(name) == "" {
		name += ".exe"
	}
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		path := registryToolString(root, `Software\Microsoft\Windows\CurrentVersion\App Paths\`+name, "", os.Environ())
		if path != "" {
			return strings.Trim(path, `"`)
		}
	}
	return ""
}
