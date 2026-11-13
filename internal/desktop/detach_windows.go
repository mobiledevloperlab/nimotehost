// SPDX-License-Identifier: Apache-2.0
package desktop

import (
	"golang.org/x/sys/windows"
	"os/exec"
	"syscall"
)

// Winsock uses a different error domain from syscall.ECONNREFUSED.
const connectionRefused = windows.WSAECONNREFUSED

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200, HideWindow: true}
}
