// SPDX-License-Identifier: Apache-2.0
//go:build windows

package host

func resourceEnforcement(l ResourceLimits) ResourceEnforcement {
	return ResourceEnforcement{Backend: "job-object", HardCPU: true, HardMemory: true, HardTasks: true, CrashCleanup: "job-object"}
}
func recoverExecutionScope(scope *sessionScope, l ResourceLimits) error { return nil }
