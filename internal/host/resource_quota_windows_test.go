// SPDX-License-Identifier: Apache-2.0
//go:build windows

package host

import (
	"golang.org/x/sys/windows"
	"io"
	"os"
	"os/exec"
	"testing"
	"unsafe"
)

func TestWindowsNativeJobQuotaConfiguration(t *testing.T) {
	limits := DefaultResourceLimits()
	limits.CPUPercent = 10
	limits.MemoryBytes = 256 << 20
	limits.MaxProcesses = 4
	if err := limits.Validate(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("cmd.exe", "/d", "/c", "ping -n 60 127.0.0.1 >nul")
	process, _, err := startProcess(cmd, false, io.Discard, limits)
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	defer func() { _ = process.Kill(); _, _ = process.Wait() }()
	job := process.(*windowsProcess).job
	var extended windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if err := windows.QueryInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&extended)), uint32(unsafe.Sizeof(extended)), nil); err != nil {
		t.Fatal(err)
	}
	flags := uint32(windows.JOB_OBJECT_LIMIT_JOB_MEMORY | windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS | windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE)
	if extended.JobMemoryLimit != uintptr(limits.MemoryBytes) || extended.BasicLimitInformation.ActiveProcessLimit != 4 || extended.BasicLimitInformation.LimitFlags&flags != flags {
		t.Fatal("native job quotas were not applied", extended)
	}
	var rate struct{ Flags, Rate uint32 }
	if err := windows.QueryInformationJobObject(job, windows.JobObjectCpuRateControlInformation, uintptr(unsafe.Pointer(&rate)), uint32(unsafe.Sizeof(rate)), nil); err != nil {
		t.Fatal(err)
	}
	if rate.Flags != 5 || rate.Rate != 1000 {
		t.Fatal("CPU hard cap not active", rate)
	}
}
func TestWindowsQuotaAllocationChild(t *testing.T) {
	mode := ""
	for i, arg := range os.Args {
		if arg == "--quota-allocation-child" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
		}
	}
	if mode == "" {
		return
	}
	p, err := windows.VirtualAlloc(0, 512<<20, windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	allocated := err == nil && p != 0
	if allocated {
		_ = windows.VirtualFree(p, 0, windows.MEM_RELEASE)
	}
	if (mode == "allow" && allocated) || (mode == "deny" && !allocated) {
		os.Exit(0)
	}
	os.Exit(3)
}
func TestWindowsJobMemoryLimitActuallyDeniesAllocation(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"allow", "deny"} {
		t.Run(mode, func(t *testing.T) {
			limits := DefaultResourceLimits()
			if mode == "deny" {
				limits.MemoryBytes = 256 << 20
			}
			cmd := exec.Command(executable, "-test.run=^TestWindowsQuotaAllocationChild$", "--", "--quota-allocation-child", mode)
			process, _, err := startProcess(cmd, false, io.Discard, limits)
			if err != nil {
				t.Fatal(err)
			}
			defer process.Close()
			defer process.Kill()
			code, err := process.Wait()
			if err != nil || code != 0 {
				t.Fatal("allocation did not match job quota", mode, code, err)
			}
		})
	}
}
