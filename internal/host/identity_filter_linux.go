// SPDX-License-Identifier: Apache-2.0
//go:build linux

package host

import (
	"golang.org/x/sys/unix"
	"runtime"
	"unsafe"
)

func installIdentityFilter(filter []unix.SockFilter) error {
	program := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	err := unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, uintptr(unsafe.Pointer(&program)), 0, 0)
	runtime.KeepAlive(filter)
	runtime.KeepAlive(&program)
	return err
}
