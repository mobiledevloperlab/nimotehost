// SPDX-License-Identifier: Apache-2.0
package hostservice

import (
	"os/exec"
	"syscall"
)

// Service manager commands invoked by the GUI must not create a console.
func hideManagerWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
