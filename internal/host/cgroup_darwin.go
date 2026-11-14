// SPDX-License-Identifier: Apache-2.0
//go:build darwin

package host

import (
	"errors"
	"os"
	"os/exec"
)

const nativeQuotasSupported = false

type unixScope struct {
	file *os.File
	path string
}

func platformQuotaValidate(l ResourceLimits) error {
	if l.CgroupRoot != "" || l.CPUPercent != 0 || l.MemoryBytes != 0 || l.MaxProcesses != 0 {
		return errors.New("hard CPU, memory and process quotas are unsupported on macOS")
	}
	return nil
}
func newUnixScope(l ResourceLimits) (*unixScope, error) { return nil, platformQuotaValidate(l) }
func (s *unixScope) Close() error                       { return nil }
func killCgroupFD(fd int) error                         { return errors.New("cgroup containment is unsupported on macOS") }
func attachScope(cmd *exec.Cmd, fd int)                 {}

func cleanupSupervisorScope(fd int, path string) {}
