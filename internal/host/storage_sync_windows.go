// SPDX-License-Identifier: Apache-2.0
//go:build windows

package host

// Windows does not support os.File.Sync on directory handles. The file itself
// is flushed before replacement; do not report a committed replacement as failed
// merely because POSIX directory syncing is unavailable.
func syncDirectory(string) error { return nil }
