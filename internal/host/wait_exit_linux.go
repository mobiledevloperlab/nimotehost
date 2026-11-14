// SPDX-License-Identifier: Apache-2.0
//go:build linux

package host

import (
	"errors"
	"golang.org/x/sys/unix"
)

func observeChildExit(pid int) error {
	for {
		var info unix.Siginfo
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return err
	}
}
