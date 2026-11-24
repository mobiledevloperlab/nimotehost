// SPDX-License-Identifier: Apache-2.0
package hostcli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/mobiledevloperlab/nimotehost/internal/host"
)

// Old names are confined to upgrade compatibility. New installations always
// use nimotehost; existing authenticated state is moved without regenerating it.
const legacyName = "nimote-host"

type nameMigration struct {
	oldDir, newDir, binary               string
	installed, running, moved, committed bool
}

func prepareNameMigration(ctx context.Context, newDir string) (*nameMigration, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	oldDir := filepath.Join(filepath.Dir(newDir), legacyName)
	binary := filepath.Join(home, ".local", "share", legacyName, legacyName)
	definition := filepath.Join(home, "Library", "LaunchAgents", "com.nimote.host.plist")
	if runtime.GOOS == "linux" {
		definition = filepath.Join(filepath.Dir(newDir), "systemd", "user", legacyName+".service")
	}
	if runtime.GOOS == "windows" {
		binary = filepath.Join(os.Getenv("LOCALAPPDATA"), "NimoteHost", legacyName+".exe")
		definition = filepath.Join(oldDir, "service.xml")
	}
	return migrateName(ctx, oldDir, newDir, binary, definition, host.Call, runSetupCommand)
}

type migrationCommand func(context.Context, string, string) error

func migrateName(ctx context.Context, oldDir, newDir, binary, definition string, call hostCaller, command migrationCommand) (_ *nameMigration, result error) {
	m := &nameMigration{oldDir: oldDir, newDir: newDir, binary: binary}
	info, err := os.Lstat(oldDir)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("legacy state must be a real directory")
	}
	if _, err := os.Lstat(newDir); err == nil {
		return nil, errors.New("both legacy and nimotehost state directories exist; refusing to merge or overwrite them")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	lock, err := host.ControlLock(oldDir)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if info, err := os.Lstat(definition); err == nil {
		if !info.Mode().IsRegular() {
			return nil, errors.New("legacy service definition must be a regular file")
		}
		m.installed = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if m.installed {
		info, err := os.Lstat(binary)
		if err != nil || !info.Mode().IsRegular() {
			return nil, errors.New("restore the complete legacy installation before migrating its service")
		}
	}
	_, err = call(ctx, oldDir, host.RPCRequest{Method: "GET", Path: "/v1/status"})
	m.running = err == nil
	if err != nil && !hostUnavailable(err) {
		return nil, fmt.Errorf("check legacy Host: %w", err)
	}
	if m.running && !m.installed {
		return nil, errors.New("stop the legacy foreground Host before running setup")
	}
	changed := false
	defer func() {
		if result != nil && changed {
			result = errors.Join(result, m.rollbackWith(ctx, command))
		}
	}()
	if m.running {
		if _, err := call(ctx, oldDir, host.RPCRequest{Method: "POST", Path: "/v1/shutdown-if-idle"}); err != nil {
			return nil, fmt.Errorf("migration left legacy Host unchanged: %w", err)
		}
		changed = true
		if err := awaitState(ctx, oldDir, false, call); err != nil {
			return nil, err
		}
	}
	// Unregister using the old executable, which knows its original native label.
	// Do not move state until the service and process locks have both released it.
	if m.installed {
		changed = true
		if err := command(ctx, binary, "uninstall"); err != nil {
			return nil, err
		}
	}
	idle, err := host.StateIdle(oldDir)
	if err != nil {
		return nil, err
	}
	if !idle {
		return nil, errors.New("legacy Host is still cleaning up; retry setup shortly")
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		return nil, err
	}
	m.moved = true
	return m, nil
}

func (m *nameMigration) restore(ctx context.Context, command migrationCommand) error {
	if !m.installed {
		return nil
	}
	if err := command(ctx, m.binary, "install"); err != nil {
		return err
	}
	if !m.running {
		return command(ctx, m.binary, "stop")
	}
	return nil
}

func (m *nameMigration) rollbackWith(ctx context.Context, command migrationCommand) error {
	if m.committed {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
	defer cancel()
	if m.moved {
		idle, err := host.StateIdle(m.newDir)
		if err != nil {
			return err
		}
		if !idle {
			return errors.New("new Host is still running; migration state was retained for recovery")
		}
		if err := os.Rename(m.newDir, m.oldDir); err != nil {
			return err
		}
		m.moved = false
	}
	return m.restore(ctx, command)
}

func (m *nameMigration) rollback(ctx context.Context) error {
	return m.rollbackWith(ctx, runSetupCommand)
}

// Keep the existing SSH command alias useful, while publishing the new name.
// Never replace a user-owned command or a link to another installation.
func (m *nameMigration) updateAlias(target string) error {
	if runtime.GOOS == "windows" || !m.moved {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	// Existing restricted SSH keys can contain the absolute old executable path.
	// Retain the old executable and turn that path into a new-CLI compatibility
	// link so those keys keep using the migrated device identities.

	if err := forwardLegacyPath(m.binary, target); err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		newApp := filepath.Join(filepath.Dir(target), "NimoteHost.app")
		if info, err := os.Stat(newApp); err == nil && info.IsDir() {
			if err := forwardLegacyPath(filepath.Join(filepath.Dir(m.binary), "Nimote Host.app"), newApp); err != nil {
				return err
			}
		}
	}
	alias := filepath.Join(home, ".local", "bin", legacyName)
	current, err := os.Readlink(alias)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return nil
	}
	if current != m.binary {
		return nil
	}
	temp := alias + ".migrating"
	if err := os.Symlink(target, temp); err != nil {
		return err
	}
	defer os.Remove(temp)
	return os.Rename(temp, alias)
}

// An old Dock entry or forced SSH command should launch the new application.
// Preserve the retired file/directory rather than overwriting its contents.
func forwardLegacyPath(old, target string) error {
	info, err := os.Lstat(old)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return fmt.Errorf("invalid legacy application path: %s", old)
	}
	backup := old + ".before-nimotehost"
	if _, err := os.Lstat(backup); err == nil {
		return fmt.Errorf("legacy application backup already exists: %s", backup)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(old, backup); err != nil {
		return err
	}
	if err := os.Symlink(target, old); err != nil {
		return errors.Join(err, os.Rename(backup, old))
	}
	return nil
}
