//go:build linux || darwin

// SPDX-License-Identifier: Apache-2.0
package desktop

import (
	"os/exec"
	"syscall"
)

const connectionRefused = syscall.ECONNREFUSED

func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
