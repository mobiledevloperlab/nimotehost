// SPDX-License-Identifier: Apache-2.0
package main

import (
	"debug/pe"
	"runtime"

	"golang.org/x/sys/windows"
)

func platformArch() string {
	var process, native uint16
	if err := windows.IsWow64Process2(windows.CurrentProcess(), &process, &native); err == nil {
		switch native {
		case pe.IMAGE_FILE_MACHINE_AMD64:
			return "amd64"
		case pe.IMAGE_FILE_MACHINE_ARM64:
			return "arm64"
		case pe.IMAGE_FILE_MACHINE_I386:
			return "386"
		default:
			return "unknown"
		}
	}
	return runtime.GOARCH
}
