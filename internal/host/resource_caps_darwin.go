// SPDX-License-Identifier: Apache-2.0
//go:build darwin

package host

func resourceEnforcement(l ResourceLimits) ResourceEnforcement {
	return ResourceEnforcement{Backend: "process-group", CrashCleanup: "process-group"}
}
func recoverExecutionScope(scope *sessionScope, l ResourceLimits) error { return nil }
