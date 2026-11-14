// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package hostinstall

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Transaction holds an exclusive setup lock. Old distributions are retained
// beside the destination after a successful update; runtime state is never moved.
type Transaction struct {
	Target string
	Backup string
	stage  string
	lock   string
	active bool
}

// Prepare validates and copies into a sibling staging directory before touching
// the installed version. Close must be called on the returned transaction.
func Prepare(ctx context.Context, source, target string) (*Transaction, error) {
	if !filepath.IsAbs(source) || !filepath.IsAbs(target) || filepath.Clean(target) == filepath.VolumeName(target)+string(os.PathSeparator) {
		return nil, errors.New("absolute non-root setup paths are required")
	}
	if info, err := os.Lstat(target); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("installation destination must be a real directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return nil, err
	}
	tx := &Transaction{Target: target, lock: target + ".setup-lock"}
	if err := os.Mkdir(tx.lock, 0700); err != nil {
		return nil, fmt.Errorf("cannot acquire setup lock %s (another setup may be running): %w", tx.lock, err)
	}
	stage, err := os.MkdirTemp(filepath.Dir(target), ".nimotehost-stage-")
	if err != nil {
		_ = tx.Close()
		return nil, err
	}
	tx.stage = stage
	if err := copyVerified(ctx, source, stage); err != nil {
		_ = tx.Close()
		return nil, err
	}
	return tx, nil
}

// Unchanged verifies the installed tree and compares its manifest with the staged
// bundle. A corrupted installation is repaired instead of treated as up to date.
func (tx *Transaction) Unchanged(ctx context.Context) bool {
	if Verify(ctx, tx.Target) != nil {
		return false
	}
	a, err := os.ReadFile(filepath.Join(tx.Target, "SHA256SUMS"))
	if err != nil {
		return false
	}
	b, err := os.ReadFile(filepath.Join(tx.stage, "SHA256SUMS"))
	return err == nil && string(a) == string(b)
}

// Activate replaces only the installation directory. The caller must first stop
// the native service and must call Rollback if subsequent service startup fails.
func (tx *Transaction) Activate(ctx context.Context) error {
	if tx.active {
		return errors.New("setup transaction already activated")
	}
	if _, err := os.Stat(tx.Target); err == nil {
		backup, err := os.MkdirTemp(filepath.Dir(tx.Target), ".nimotehost-previous-")
		if err != nil {
			return err
		}
		if err := os.Remove(backup); err != nil {
			return err
		}
		if err := rename(ctx, tx.Target, backup); err != nil {
			return err
		}
		tx.Backup = backup
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := rename(ctx, tx.stage, tx.Target); err != nil {
		if tx.Backup != "" {
			return errors.Join(err, rename(context.Background(), tx.Backup, tx.Target))
		}
		return err
	}
	tx.stage = ""
	tx.active = true
	return nil
}

// Rollback preserves the failed bundle for inspection and restores the old tree.
// The caller must stop the new service before calling it (especially on Windows).
func (tx *Transaction) Rollback(ctx context.Context) error {
	if !tx.active {
		return nil
	}
	failed, err := os.MkdirTemp(filepath.Dir(tx.Target), ".nimotehost-failed-")
	if err != nil {
		return err
	}
	if err := os.Remove(failed); err != nil {
		return err
	}
	if err := rename(ctx, tx.Target, failed); err != nil {
		return err
	}
	if tx.Backup != "" {
		if err := rename(ctx, tx.Backup, tx.Target); err != nil {
			return fmt.Errorf("restore previous bundle from %s: %w", tx.Backup, err)
		}
	}
	tx.active = false
	return nil
}

// Close releases temporary files and the setup lock, retaining version backups.
func (tx *Transaction) Close() error {
	var err error
	if tx.stage != "" {
		err = os.RemoveAll(tx.stage)
	}
	return errors.Join(err, os.Remove(tx.lock))
}

func rename(ctx context.Context, old, new string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := os.Rename(old, new)
		if err == nil || runtime.GOOS != "windows" || errors.Is(err, os.ErrNotExist) {
			return err
		}
		// Windows may briefly retain an executable handle after its service exits.
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(err, ctx.Err())
		case <-timer.C:
		}
	}
}
