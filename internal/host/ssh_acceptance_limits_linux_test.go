//go:build linux

// SPDX-License-Identifier: Apache-2.0
package host

import "testing"

func sshAcceptanceLimits(t *testing.T) ResourceLimits {
	l := DefaultResourceLimits()
	l.CgroupRoot = rootScopeFixture(t)
	l.MemoryBytes = 384 << 20
	l.MaxProcesses = 128
	return l
}
