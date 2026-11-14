// SPDX-License-Identifier: Apache-2.0
//go:build linux || darwin

package host

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSupervisorOwnerProcess(t *testing.T) {
	if os.Getenv("NIMOTE_SUPERVISOR_OWNER_TEST") != "1" {
		return
	}
	dir := os.Getenv("NIMOTE_SUPERVISOR_OWNER_DIR")
	m, err := NewManager(filepath.Join(dir, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	kind := os.Getenv("NIMOTE_SUPERVISOR_KIND")
	record, err := m.Start(StartRequest{Kind: kind, Command: "/bin/sh", Args: []string{"-c", `printf '%s' $$ > task-pid; (while :; do printf x >> heartbeat; sleep 0.02; done) & while :; do sleep 1; done`}, Directory: dir})
	if err != nil {
		t.Fatal(err)
	}
	_ = json.NewEncoder(os.Stdout).Encode(record)
	select {}
}
func TestHostSIGKILLReapsOwnedUnixProcessGroups(t *testing.T) {
	for _, kind := range []string{"task", "terminal"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			owner := exec.Command(exe, "-test.run=^TestSupervisorOwnerProcess$")
			owner.Env = append(os.Environ(), "NIMOTE_SUPERVISOR_OWNER_TEST=1", "NIMOTE_SUPERVISOR_OWNER_DIR="+dir, "NIMOTE_SUPERVISOR_KIND="+kind)
			output, err := owner.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			owner.Stderr = io.Discard
			if err := owner.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = owner.Process.Kill(); _ = owner.Wait() }()
			ready := make(chan Session, 1)
			go func() {
				var s Session
				if json.NewDecoder(bufio.NewReader(output)).Decode(&s) == nil {
					ready <- s
				}
			}()
			var record Session
			select {
			case record = <-ready:
			case <-time.After(5 * time.Second):
				t.Fatal("owner fixture never started")
			}
			var taskPID int
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				raw, _ := os.ReadFile(filepath.Join(dir, "task-pid"))
				taskPID, _ = strconv.Atoi(string(raw))
				if taskPID > 0 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if taskPID <= 0 {
				t.Fatal("target never started")
			}
			if err := owner.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = owner.Wait()
			deadline = time.Now().Add(5 * time.Second)
			for !errors.Is(syscall.Kill(taskPID, 0), syscall.ESRCH) {
				if time.Now().After(deadline) {
					t.Fatal("owned target survived Host SIGKILL")
				}
				time.Sleep(20 * time.Millisecond)
			}
			before, err := os.ReadFile(filepath.Join(dir, "heartbeat"))
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(150 * time.Millisecond)
			after, err := os.ReadFile(filepath.Join(dir, "heartbeat"))
			if err != nil || len(before) != len(after) {
				t.Fatal("descendant survived Host SIGKILL", err)
			}
			m, err := NewManager(filepath.Join(dir, "sessions"))
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			recovered, err := m.Get(record.ID)
			if err != nil || recovered.State != "interrupted" {
				t.Fatal("crash recovery did not preserve interrupted state", recovered, err)
			}
		})
	}
}
func TestSupervisorPreservesExitAndRejectsInvalidPrivateEntry(t *testing.T) {
	for _, code := range []int{0, 7} {
		cmd := exec.Command("/bin/sh", "-c", "exit "+strconv.Itoa(code))
		process, _, err := startProcess(cmd, false, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		got, err := process.Wait()
		if got != code || (err == nil) != (code == 0) {
			t.Fatal("target exit changed", got, err)
		}
		if err := process.Kill(); err != nil {
			t.Fatal("late cancel failed", err)
		}
		_ = process.Close()
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, supervisorArg, "pipe", "/bin/sh", "sh", "-c", "exit 0")
	if err := cmd.Run(); err == nil {
		t.Fatal("private supervisor accepted missing inherited pipes")
	}
}
func TestInteractiveShellJobControlStillReceivesInput(t *testing.T) {
	m := newTestManager(t)
	s, err := m.Start(StartRequest{Kind: "terminal", Command: "/bin/bash", Args: []string{"--noprofile", "--norc", "-i"}, Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Input(s.ID, []byte("printf 'SUPERVISOR_TTY_OK\\n'\nexit\n")); err != nil {
		t.Fatal(err)
	}
	result := awaitSession(t, m, s.ID)
	data, _, err := m.Output(s.ID, 0)
	if err != nil || result.State != "succeeded" || !strings.Contains(string(data), "SUPERVISOR_TTY_OK") {
		t.Fatal("interactive job control broke", result, err, string(data))
	}
}
