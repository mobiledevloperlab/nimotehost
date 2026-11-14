// SPDX-License-Identifier: Apache-2.0
//go:build windows

package host

import (
	"errors"
	"golang.org/x/sys/windows"
)

const nativeQuotasSupported = true

func availableDiskBytes(path string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var free uint64
	err = windows.GetDiskFreeSpaceEx(p, &free, nil, nil)
	return free, err
}

func platformQuotaValidate(l ResourceLimits) error {
	if l.CgroupRoot != "" {
		return errors.New("cgroup_root is only supported on Linux")
	}
	return nil
}
