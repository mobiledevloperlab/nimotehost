// SPDX-License-Identifier: Apache-2.0
//go:build linux || darwin

package host

import "golang.org/x/sys/unix"

func availableDiskBytes(path string) (uint64, error) {
	var s unix.Statfs_t
	if err := unix.Statfs(path, &s); err != nil {
		return 0, err
	}
	return uint64(s.Bavail) * uint64(s.Bsize), nil
}
