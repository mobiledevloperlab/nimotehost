// SPDX-License-Identifier: Apache-2.0
package host

import (
	"os"
	"path/filepath"
)

// publishExecutable records the running Windows CLI independently of its download
// directory and sshd's cached PATH. Keep it on shutdown so stopped hosts remain
// discoverable; the next successful start refreshes it after a move or upgrade.
// The caller holds host.lock, so a second process cannot replace the live path.
func publishExecutable(dir string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	return saveJSON(filepath.Join(dir, "discovery.json"), struct {
		Executable string `json:"executable"`
	}{executable})
}
