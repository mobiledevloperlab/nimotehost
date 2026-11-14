// SPDX-License-Identifier: Apache-2.0
//go:build darwin

package host

import (
	"errors"
	"golang.org/x/sys/unix"
)

func observeChildExit(pid int) error {
	fd, err := unix.Kqueue()
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	unix.CloseOnExec(fd)
	change := unix.Kevent_t{Ident: uint64(pid), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_ONESHOT, Fflags: unix.NOTE_EXIT}
	changes := []unix.Kevent_t{change}
	events := make([]unix.Kevent_t, 1)
	for {
		n, err := unix.Kevent(fd, changes, events, nil)
		changes = nil
		if errors.Is(err, unix.EINTR) {
			continue
		}
		// An already-zombie child can reject registration. It remains unreaped and
		// owned by this supervisor, so cleanup before child.Wait is still safe.
		if errors.Is(err, unix.ESRCH) {
			return nil
		}
		if err != nil {
			return err
		}
		if n > 0 {
			if events[0].Flags&unix.EV_ERROR != 0 {
				e := unix.Errno(events[0].Data)
				if e == unix.ESRCH {
					return nil
				}
				return e
			}
			return nil
		}
	}
}
