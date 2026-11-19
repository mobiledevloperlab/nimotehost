// SPDX-License-Identifier: Apache-2.0
package main

import (
	"runtime"

	"golang.org/x/sys/unix"
)

func platformArch() string {
	value, err := unix.SysctlUint32("hw.optional.arm64")
	if err == nil && value == 1 {
		return "arm64"
	}
	return runtime.GOARCH
}
