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
func killProcess(cmd *exec.Cmd) error {
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
func startTerminal(cmd *exec.Cmd) (io.ReadWriteCloser, error) { return pty.Start(cmd) }
func resizeTerminal(terminal io.ReadWriteCloser, rows, cols uint16) error {
	return pty.Setsize(terminal.(*os.File), &pty.Winsize{Rows: rows, Cols: cols})
}

type unixProcess struct{ cmd *exec.Cmd }

func (p *unixProcess) Wait() (int, error) {
	err := p.cmd.Wait()
	// Root completion ends ownership of its process group. Do not leave
	// descendants running after the session reports completion.
	err = errors.Join(err, killProcess(p.cmd))
	if p.cmd.ProcessState == nil {
		return -1, err
	}
	return p.cmd.ProcessState.ExitCode(), err
}
func (p *unixProcess) Kill() error  { return killProcess(p.cmd) }
func (p *unixProcess) Close() error { return nil }
func startProcess(cmd *exec.Cmd, interactive bool, output io.Writer) (processHandle, io.ReadWriteCloser, error) {
	var terminal io.ReadWriteCloser
	var err error
	if interactive {
		terminal, err = startTerminal(cmd)
	} else {
		prepareTask(cmd)
		cmd.Stdout = output
		cmd.Stderr = output
		err = cmd.Start()
	}
	if err != nil {
		return nil, nil, err
	}
	return &unixProcess{cmd}, terminal, nil
}
func lockState(file *os.File) error { return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
