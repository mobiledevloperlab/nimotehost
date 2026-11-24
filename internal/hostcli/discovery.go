// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package hostcli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ensureSSHCommand makes the fixed installation discoverable by SSH exec
// clients which invoke nimotehost by name. zsh reads .zshenv for noninteractive
// SSH commands; bash reads .bashrc when invoked by sshd.
func ensureSSHCommand(home, target, name string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(bin)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("SSH command directory must be a real directory")
	}
	link := filepath.Join(bin, name)
	installed := filepath.Join(target, name)
	if info, err := os.Lstat(link); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("SSH command path %s is already occupied", link)
		}
		current, err := os.Readlink(link)
		if err != nil || current != installed {
			return fmt.Errorf("SSH command path %s points to another installation", link)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.Symlink(installed, link); err != nil {
			return err
		}
	} else {
		return err
	}
	startup := ""
	shell := filepath.Base(os.Getenv("SHELL"))
	switch shell {
	case "zsh":
		startup = filepath.Join(home, ".zshenv")
	case "bash":
		startup = filepath.Join(home, ".bashrc")
	default:
		return nil
	}
	const entry = `export PATH="$HOME/.local/bin:$PATH"`
	var data []byte
	mode := os.FileMode(0600)
	if info, err := os.Lstat(startup); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("shell startup path %s must be a regular file", startup)
		}
		mode = info.Mode().Perm()
		data, err = os.ReadFile(startup)
		if err != nil {
			return err
		}
		if shell == "zsh" && strings.Contains(string(data), entry) || shell == "bash" && strings.HasPrefix(string(data), "# NimoteHost SSH command\n"+entry+"\n") {
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if shell == "bash" {
		// SSH bash sessions can stop reading .bashrc at an early noninteractive
		// guard. Put PATH first while retaining the rest of the file verbatim.
		temporary, err := os.CreateTemp(home, ".nimotehost-bashrc-*")
		if err != nil {
			return err
		}
		defer os.Remove(temporary.Name())
		if err := temporary.Chmod(mode); err != nil {
			temporary.Close()
			return err
		}
		_, writeErr := fmt.Fprint(temporary, "# NimoteHost SSH command\n", entry, "\n")
		if writeErr == nil {
			_, writeErr = temporary.Write(data)
		}
		closeErr := errors.Join(temporary.Sync(), temporary.Close())
		if err := errors.Join(writeErr, closeErr); err != nil {
			return err
		}
		return os.Rename(temporary.Name(), startup)
	}
	f, err := os.OpenFile(startup, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprint(f, "\n# NimoteHost SSH command\n", entry, "\n")
	return errors.Join(writeErr, f.Close())
}
