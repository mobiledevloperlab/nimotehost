// SPDX-License-Identifier: Apache-2.0
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Copy the old app preferences once, before Fyne creates the new application.
// The original stays available for rollback; a new preference file is never replaced.
func migrateDesktopPreferences() error {
	base, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		if runtime.GOOS == "darwin" {
			base = filepath.Join(home, "Library", "Preferences")
		} else {
			base = filepath.Join(home, "AppData", "Roaming")
		}
	}
	return copyLegacyPreferences(filepath.Join(base, "fyne"))
}

func copyLegacyPreferences(root string) error {
	target := filepath.Join(root, "com.nimotehost.desktop", "preferences.json")
	if _, err := os.Lstat(target); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	old := filepath.Join(root, "com.nimote.host.desktop", "preferences.json")
	info, err := os.Lstat(old)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return fmt.Errorf("invalid legacy desktop preferences: %s", old)
	}
	data, err := os.ReadFile(old)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, writeErr := out.Write(data)
	closeErr := out.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return errors.Join(err, os.Remove(target))
	}
	return nil
}
