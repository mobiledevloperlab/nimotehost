// SPDX-License-Identifier: Apache-2.0
//go:build linux

package host

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const nativeQuotasSupported = false // Default backend has no delegated hierarchy.

type unixScope struct {
	file *os.File
	path string
}

func cgroupControllers(root string) (map[string]bool, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, errors.New("cgroup_root must be a canonical absolute directory")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("cgroup_root must be a real directory")
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(root, &stat); err != nil {
		return nil, err
	}
	if uint64(stat.Type) != uint64(unix.CGROUP2_SUPER_MAGIC) {
		return nil, errors.New("cgroup_root is not a cgroup v2 filesystem")
	}
	data, err := os.ReadFile(filepath.Join(root, "cgroup.subtree_control"))
	if err != nil {
		return nil, err
	}
	result := map[string]bool{}
	for _, name := range strings.Fields(string(data)) {
		result[name] = true
	}
	return result, nil
}
func platformQuotaValidate(l ResourceLimits) error {
	requested := l.CPUPercent != 0 || l.MemoryBytes != 0 || l.MaxProcesses != 0
	if l.CgroupRoot == "" {
		if requested {
			return errors.New("Linux native quotas require an administrator-delegated cgroup_root")
		}
		return nil
	}
	controllers, err := cgroupControllers(l.CgroupRoot)
	if err != nil {
		return err
	}
	for name, needed := range map[string]bool{"cpu": l.CPUPercent > 0, "memory": l.MemoryBytes > 0, "pids": l.MaxProcesses > 0} {
		if needed && !controllers[name] {
			return fmt.Errorf("cgroup %s controller is not delegated", name)
		}
	}
	return nil
}
func newUnixScope(l ResourceLimits) (*unixScope, error) {
	if err := validateNativeQuotas(l); err != nil {
		return nil, err
	}
	if l.CgroupRoot == "" {
		return nil, nil
	}
	name, err := randomID()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(l.CgroupRoot, "nimotehost-"+name)
	if l.scopeName != "" {
		path = filepath.Join(l.CgroupRoot, l.scopeName)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		return nil, err
	}
	scope := &unixScope{path: path}
	success := false
	defer func() {
		if !success {
			_ = scope.Close()
		}
	}()
	settings := map[string]string{}
	if l.CPUPercent > 0 {
		settings["cpu.max"] = fmt.Sprintf("%d 100000", int64(l.CPUPercent)*1000*int64(runtime.NumCPU()))
	}
	if l.MemoryBytes > 0 {
		settings["memory.max"] = strconv.FormatUint(l.MemoryBytes, 10)
		settings["memory.swap.max"] = "0"
		settings["memory.oom.group"] = "1"
	}
	if l.MaxProcesses > 0 {
		settings["pids.max"] = strconv.Itoa(l.MaxProcesses)
	}
	for key, value := range settings {
		if err := os.WriteFile(filepath.Join(path, key), []byte(value), 0600); err != nil {
			return nil, err
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	scope.file = f
	success = true
	return scope, nil
}
func (s *unixScope) Close() error {
	if s == nil {
		return nil
	}
	if s.file != nil {
		_ = killCgroupFD(int(s.file.Fd()))
		_ = s.file.Close()
		s.file = nil
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := os.Remove(s.path)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if !errors.Is(err, syscall.EBUSY) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func killCgroupFD(fd int) error {
	var stat unix.Statfs_t
	if err := unix.Fstatfs(fd, &stat); err != nil {
		return err
	}
	if uint64(stat.Type) != uint64(unix.CGROUP2_SUPER_MAGIC) {
		return errors.New("invalid process scope descriptor")
	}
	f, err := unix.Openat(fd, "cgroup.kill", unix.O_WRONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(f)
	_, err = unix.Write(f, []byte("1"))
	return err
}
func attachScope(cmd *exec.Cmd, fd int) {
	cmd.SysProcAttr.UseCgroupFD = true
	cmd.SysProcAttr.CgroupFD = fd
}

func cleanupSupervisorScope(fd int, path string) {
	_ = killCgroupFD(fd)
	// The descriptor is still pinned; only this empty owned scope may be removed.
	for i := 0; i < 100; i++ {
		err := os.Remove(path)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return
		}
		if !errors.Is(err, syscall.EBUSY) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
