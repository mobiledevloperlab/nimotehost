//go:build windows

// SPDX-License-Identifier: Apache-2.0
package host

import "testing"

func sshAcceptanceLimits(t *testing.T) ResourceLimits { return DefaultResourceLimits() }
