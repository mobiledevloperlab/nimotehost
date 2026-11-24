// SPDX-License-Identifier: Apache-2.0
package hostcli

import (
	"runtime"

	"golang.org/x/sys/unix"
)

func platformArch() string {
	var info unix.Utsname
	if err := unix.Uname(&info); err != nil {
		return runtime.GOARCH
	}
	switch unix.ByteSliceToString(info.Machine[:]) {
	case "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	case "i386", "i486", "i586", "i686":
		return "386"
	default:
		return "unknown"
	}
}
