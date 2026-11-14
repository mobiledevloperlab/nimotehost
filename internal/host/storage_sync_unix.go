// SPDX-License-Identifier: Apache-2.0
//go:build linux || darwin

package host

import "os"

// Persist the rename as well as the contents of an atomic state replacement.
func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
