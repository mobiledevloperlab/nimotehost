// SPDX-License-Identifier: Apache-2.0
//go:build linux || darwin

package host

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// Official control directories may be 0755. The trust boundary is ownership
// and absence of group/other write access, not hiding the socket filename.
func ownedSocketDirectory(info os.FileInfo) bool {
	owner, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.IsDir() && info.Mode().Perm()&0022 == 0 && owner.Uid == uint32(os.Getuid())
}

func validateCodexSocket(endpoint string) (string, func(), error) {
	info, err := os.Lstat(endpoint)
	if err != nil {
		return "", nil, errors.New("Codex control socket is missing")
	}
	socketPath := endpoint
	if info.Mode()&os.ModeSymlink != 0 {
		// Codex uses a rendezvous symlink for long Unix socket paths. Follow
		// it only from an owner-controlled directory, then validate the target
		// without changing any provider permissions or state.
		parent := filepath.Dir(endpoint)
		parentInfo, parentErr := os.Stat(parent)
		if parentErr != nil || !ownedSocketDirectory(parentInfo) {
			return "", nil, errors.New("Codex socket alias directory is not private")
		}
		socketPath, err = filepath.EvalSymlinks(endpoint)
		if err != nil {
			return "", nil, errors.New("Codex socket alias is unavailable")
		}
		info, err = os.Lstat(socketPath)
	}
	if err != nil || info.Mode()&os.ModeSocket == 0 || secureReadFile(socketPath, info) != nil {
		return "", nil, errors.New("Codex control socket is missing or not private")
	}
	parentInfo, parentErr := os.Stat(filepath.Dir(socketPath))
	if parentErr != nil || !ownedSocketDirectory(parentInfo) {
		return "", nil, errors.New("Codex socket directory is not owned or is writable by another principal")
	}
	return socketPath, func() {}, nil
}
