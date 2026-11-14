// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package host

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func DefaultDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "nimotehost"), nil
}

// CheckStateName prevents a status probe or a new GUI from creating a second
// identity before setup has migrated a legacy installation.
func CheckStateName(dir string) error {
	legacy := filepath.Join(filepath.Dir(dir), "nimote-host")
	if _, err := os.Lstat(legacy); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("legacy Host state is present; close the old GUI and run nimotehost setup to migrate before using the new command")
}

func randomID() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func privateDir(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("Host directory must be absolute")
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Host directory must be a real directory")
	}
	return secureDirectory(path, info)
}

func saveJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}
func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".host-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := replacePrivateFile(f.Name(), path); err != nil {
		return fmt.Errorf("publish private snapshot %s: %w", path, err)
	}
	return syncDirectory(filepath.Dir(path))
}

// Secret creation is exclusive so concurrent CLI invocations never rotate a
// running daemon's credential. A partial or unsafe file fails closed.
func loadSecret(dir, name string) (string, error) {
	path := filepath.Join(dir, name)
	value, err := randomID()
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		_, writeErr := f.WriteString(value)
		syncErr := f.Sync()
		closeErr := f.Close()
		if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
			return "", err
		}
		return value, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("invalid Host credential file")
	}
	if err := secureFile(path, info); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(string(data))
	if len(value) != 64 {
		return "", fmt.Errorf("invalid Host %s", name)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", err
	}
	return value, nil
}
