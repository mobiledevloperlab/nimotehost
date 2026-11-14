// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

//go:build linux || darwin

package host

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

const terminalSupported = true

func secureDirectory(_ string, info os.FileInfo) error { return secureFile("", info) }
func secureFile(_ string, info os.FileInfo) error {
	if info.Mode().Perm()&0077 != 0 {
		return errors.New("Host state must be private to its owner (chmod 700 directory / 600 files)")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Uid != uint32(os.Getuid()) {
		return errors.New("Host state must be owned by current user")
	}
	return nil
}
func prepareTask(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func resizeTerminal(terminal io.ReadWriteCloser, rows, cols uint16) error {
	return pty.Setsize(terminal.(*os.File), &pty.Winsize{Rows: rows, Cols: cols})
}

func startProcess(cmd *exec.Cmd, interactive bool, output io.Writer, options ...ResourceLimits) (processHandle, io.ReadWriteCloser, error) {
	if err := validateNativeQuotas(nativeQuotaOptions(options)); err != nil {
		return nil, nil, err
	}
	if !interactive {
		cmd.Stdout, cmd.Stderr = output, output
	}
	return startSupervisedProcess(cmd, interactive, options...)
}
func lockState(file *os.File) error { return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB) }

func openManagementLockFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
}

func openPrivateReader(path string) (*os.File, error) { return os.Open(path) }

func secureReadFile(path string, info os.FileInfo) error { return secureFile(path, info) }

func replacePrivateFile(from, to string) error { return os.Rename(from, to) }
