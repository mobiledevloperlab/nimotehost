// SPDX-License-Identifier: Apache-2.0
//go:build linux

package host

import (
	"errors"
	"os"
	"path/filepath"
)

func resourceEnforcement(l ResourceLimits) ResourceEnforcement {
	result := ResourceEnforcement{Backend: "process-group", CrashCleanup: "process-group"}
	if l.CgroupRoot == "" {
		return result
	}
	controls, err := cgroupControllers(l.CgroupRoot)
	if err != nil {
		return result
	}
	result.Backend = "cgroup-v2"
	result.CrashCleanup = "cgroup"
	result.HardCPU = controls["cpu"]
	result.HardMemory = controls["memory"]
	result.HardTasks = controls["pids"]
	result.IndependentIdentity = validateIdentityBackend(l) == nil
	return result
}
func recoverExecutionScope(scope *sessionScope, l ResourceLimits) error {
	if scope == nil || l.CgroupRoot == "" || scope.CgroupRoot != l.CgroupRoot {
		return nil
	}
	if _, err := cgroupControllers(l.CgroupRoot); err != nil {
		return err
	}
	p := filepath.Join(l.CgroupRoot, scope.Name)
	info, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("unsafe recovered execution scope")
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	job := &unixScope{file: f, path: p}
	return job.Close()
}
