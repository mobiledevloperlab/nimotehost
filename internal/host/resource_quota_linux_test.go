// SPDX-License-Identifier: Apache-2.0
//go:build linux

package host

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRootLinuxCPUQuotaEnforcesThrottling(t *testing.T) {
	if os.Getenv("NIMOTE_TEST_ROOT_CPU") != "1" {
		t.Skip("explicit root-only CPU controller acceptance")
	}
	if os.Geteuid() != 0 {
		t.Fatal("CPU acceptance requires root")
	}
	root := rootScopeFixture(t)
	// Only this newly created test subtree changes; parent/system delegation is
	// never enabled automatically. Missing delegation is a real acceptance failure.
	if err := os.WriteFile(filepath.Join(root, "cgroup.subtree_control"), []byte("+cpu"), 0600); err != nil {
		t.Fatal("CPU controller not delegated to test subtree", err)
	}
	l := DefaultResourceLimits()
	l.CgroupRoot = root
	l.CPUPercent = 1
	l.MemoryBytes = 64 << 20
	l.MaxProcesses = 64
	if !resourceEnforcement(l).HardCPU {
		t.Fatal("delegated CPU backend not detected")
	}
	script := strings.Repeat("(while :; do :; done) &\n", runtime.NumCPU()/100+1) + "wait"
	cmd := exec.Command("/bin/sh", "-c", script)
	p, _, err := startProcess(cmd, false, io.Discard, l)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := p.Kill(); err != nil {
			t.Error(err)
		}
		_, _ = p.Wait()
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	}()
	path := p.(*unixProcess).scope.path
	raw, err := os.ReadFile(filepath.Join(path, "cpu.max"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != fmt.Sprintf("%d 100000", 1000*runtime.NumCPU()) {
		t.Fatal("kernel CPU quota differs from requested rate", string(raw))
	}
	deadline := time.Now().Add(4 * time.Second)
	for {
		data, err := os.ReadFile(filepath.Join(path, "cpu.stat"))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "nr_throttled" {
				n, err := strconv.ParseUint(fields[1], 10, 64)
				if err != nil {
					t.Fatal(err)
				}
				if n > 0 {
					t.Log("kernel CPU throttling observed", n)
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("CPU workload was not throttled", string(data))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestLinuxMemoryAllocationChild(t *testing.T) {
	if os.Getenv("NIMOTE_TEST_MEMORY_ALLOCATION_CHILD") != "1" {
		return
	}
	data := make([]byte, 128<<20)
	for i := 0; i < len(data); i += 4096 {
		data[i] = 1
	}
	fmt.Fprintln(os.Stdout, "allocation completed")
	runtime.KeepAlive(data)
	os.Exit(0)
}
func TestRootLinuxMemoryQuotaKillsExcessAllocation(t *testing.T) {
	if os.Getenv("NIMOTE_TEST_ROOT_CPU") != "1" {
		t.Skip("explicit root-only native quota acceptance")
	}
	if os.Geteuid() != 0 {
		t.Fatal("root-only acceptance")
	}
	root := rootScopeFixture(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, budget := range []uint64{512 << 20, 64 << 20} {
		t.Run(strconv.FormatUint(budget>>20, 10)+"MiB", func(t *testing.T) {
			l := DefaultResourceLimits()
			l.CgroupRoot = root
			l.MemoryBytes = budget
			l.MaxProcesses = 128
			cmd := exec.Command(executable, "-test.run=^TestLinuxMemoryAllocationChild$")
			cmd.Env = append(os.Environ(), "NIMOTE_TEST_MEMORY_ALLOCATION_CHILD=1")
			output := &agentDiagnostics{}
			p, _, err := startProcess(cmd, false, output, l)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			defer p.Kill()
			code, err := p.Wait()
			if budget > 128<<20 {
				if code != 0 || err != nil || !strings.Contains(output.text(), "allocation completed") {
					t.Fatal("control allocation failed", code, err, output.text())
				}
			} else if code != -1 || err == nil || !strings.Contains(err.Error(), "killed") || strings.Contains(output.text(), "allocation completed") {
				t.Fatal("excess physical allocation was not killed by the kernel", code, err, output.text())
			}
		})
	}
}
