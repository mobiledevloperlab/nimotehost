// SPDX-License-Identifier: Apache-2.0
package host

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

// Config is shared by every local client. The SSH RPC wire protocol is unchanged.
type Config struct {
	Port     int    `json:"port"`
	LogLevel string `json:"log_level"`
}

func ReadConfig(dir string) (Config, error) {
	c := Config{47831, "info"}
	if err := privateDir(dir); err != nil {
		return c, err
	}
	p := filepath.Join(dir, "config.json")
	info, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if !info.Mode().IsRegular() {
		return c, errors.New("configuration must be a regular file")
	}
	if err = secureFile(p, info); err != nil {
		return c, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return c, err
	}
	if err = decodeJSON(b, &c); err != nil {
		return c, err
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	if c.Port < 1024 || c.Port > 65535 {
		return errors.New("port must be between 1024 and 65535")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
		return nil
	}
	return errors.New("invalid log level")
}
func (c Config) Address() string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(c.Port)) }

// SaveConfig only changes stopped Hosts. Callers serialize lifecycle operations.
func SaveConfig(dir string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := privateDir(dir); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "host.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = lockState(f); err != nil {
		return errors.New("stop Host before changing configuration")
	}
	return saveJSON(filepath.Join(dir, "config.json"), c)
}

// ControlLock serializes management across GUI processes without owning Host lifetime.
func ControlLock(dir string) (*os.File, error) {
	if err := privateDir(dir); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "control.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockState(f); err != nil {
		f.Close()
		return nil, errors.New("another manager is changing Host; retry shortly")
	}
	return f, nil
}

// StateIdle distinguishes a closed RPC listener from completion of process and
// session cleanup. A new detached daemon must wait until the previous owner
// releases this lock, even after the shutdown response has been delivered.
func StateIdle(dir string) (bool, error) {
	if err := privateDir(dir); err != nil {
		return false, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "host.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return false, err
	}
	defer f.Close()
	if err = lockState(f); err != nil {
		return false, nil
	}
	return true, nil
}
