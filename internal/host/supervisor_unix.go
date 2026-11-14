// SPDX-License-Identifier: Apache-2.0
//go:build linux || darwin

package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

const supervisorArg = "__nimotehost_process_supervisor_v1"

type supervisorPolicy struct {
	Interactive bool               `json:"interactive"`
	Scoped      bool               `json:"scoped"`
	ScopePath   string             `json:"scope_path,omitempty"`
	Identity    *ExecutionIdentity `json:"identity,omitempty"`
}

type supervisorReport struct {
	Code  int    `json:"code"`
	Error string `json:"error,omitempty"`
}

// A private re-exec entrypoint, not an RPC command. Both descriptors must be
// inherited pipes. No token, state path or credential crosses this boundary.
func init() {
	if len(os.Args) > 1 && os.Args[1] == supervisorArg {
		os.Exit(runProcessSupervisor(os.Args[2:]))
	}
}
func runProcessSupervisor(args []string) int {
	if len(args) < 3 {
		return 125
	}
	var policy supervisorPolicy
	if len(args[0]) > 4096 || decodeJSON([]byte(args[0]), &policy) != nil {
		return 125
	}
	life, status := os.NewFile(3, "host-lifetime"), os.NewFile(4, "process-status")
	for _, file := range []*os.File{life, status} {
		info, err := file.Stat()
		if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
			return 125
		}
		unix.CloseOnExec(int(file.Fd()))
	}
	defer life.Close()
	defer status.Close()
	encoder := json.NewEncoder(status)
	child := exec.Command(args[1])
	child.Args = append([]string(nil), args[2:]...)
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := configureIdentityChild(child, policy.Identity); err != nil {
		_ = encoder.Encode(supervisorReport{Code: -1, Error: err.Error()})
		return 125
	}
	if policy.Scoped {
		unix.CloseOnExec(5)
		attachScope(child, 5)
	}
	killOwned := func() error {
		if policy.Scoped {
			return killCgroupFD(5)
		}
		return syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
	}
	var mu sync.Mutex
	alive, stopping := false, false
	// Closing the Host's writer is cancellation, including Host SIGKILL. The
	// supervisor itself never inherits that writer, nor do target descendants.
	go func() {
		var b [1]byte
		_, _ = life.Read(b[:])
		mu.Lock()
		defer mu.Unlock()
		stopping = true
		if alive {
			_ = killOwned()
		}
	}()
	mu.Lock()
	if stopping {
		mu.Unlock()
		return 125
	}
	err := child.Start()
	alive = err == nil
	if err == nil && policy.Interactive {
		err = unix.IoctlSetPointerInt(int(os.Stdin.Fd()), unix.TIOCSPGRP, child.Process.Pid)
		if err != nil {
			_ = killOwned()
		}
	}
	// Do not keep the target's stdin reader alive: native Agent tests and real
	// providers must observe EPIPE when the target closes its input.
	_ = os.Stdin.Close()
	if err != nil {
		if alive {
			alive = false
			_ = child.Wait()
		}
		mu.Unlock()
		_ = encoder.Encode(supervisorReport{Code: -1, Error: boundedAgentText(err.Error())})
		return 125
	}
	mu.Unlock()
	if err := encoder.Encode(supervisorReport{}); err != nil {
		mu.Lock()
		if alive {
			_ = killOwned()
		}
		mu.Unlock()
	}
	// Observe exit WITHOUT reaping. The unreaped child pins its PID/group ID;
	// signal its descendants before Wait makes that ID available for reuse.
	observeErr := observeChildExit(child.Process.Pid)
	mu.Lock()
	_ = killOwned()
	alive = false
	waitErr := child.Wait()
	mu.Unlock()
	report := supervisorReport{Code: -1}
	if child.ProcessState != nil {
		report.Code = child.ProcessState.ExitCode()
	}
	if err := errors.Join(observeErr, waitErr); err != nil {
		report.Error = err.Error()
		if len(report.Error) > 512 {
			report.Error = report.Error[:512]
		}
	}
	_ = encoder.Encode(report)
	if policy.Scoped {
		cleanupSupervisorScope(5, policy.ScopePath)
	}
	return 0
}

type unixProcess struct {
	cmd          *exec.Cmd
	life, status *os.File
	decoder      *json.Decoder
	cancelOnce   sync.Once
	closeOnce    sync.Once
	closeErr     error
	scope        *unixScope
	waitDone     chan struct{}
	waitErr      error
}

func (p *unixProcess) Wait() (int, error) {
	var report supervisorReport
	if err := p.decoder.Decode(&report); err != nil {
		<-p.waitDone
		return -1, errors.Join(p.waitErr, fmt.Errorf("supervisor completion unavailable: %w", err))
	}
	if report.Error != "" {
		return report.Code, errors.New(report.Error)
	}
	return report.Code, nil
}
func (p *unixProcess) Kill() error {
	var err error
	p.cancelOnce.Do(func() { err = p.life.Close() })
	return err
}
func (p *unixProcess) Close() error {
	p.closeOnce.Do(func() {
		_ = p.Kill()
		p.closeErr = p.status.Close()
		if p.waitDone != nil {
			<-p.waitDone
			p.closeErr = errors.Join(p.closeErr, p.waitErr)
		}
		if p.scope != nil {
			p.closeErr = errors.Join(p.closeErr, p.scope.Close())
		}
	})
	return p.closeErr
}

func startSupervisedProcess(target *exec.Cmd, interactive bool, options ...ResourceLimits) (*unixProcess, io.ReadWriteCloser, error) {
	if target.Err != nil {
		return nil, nil, target.Err
	}
	scope, err := newUnixScope(nativeQuotaOptions(options))
	if err != nil {
		return nil, nil, err
	}
	owned := false
	defer func() {
		if !owned && scope != nil {
			_ = scope.Close()
		}
	}()
	executable, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	lifeR, lifeW, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	defer lifeR.Close()
	statusR, statusW, err := os.Pipe()
	if err != nil {
		lifeW.Close()
		return nil, nil, err
	}
	defer statusW.Close()
	policy := supervisorPolicy{Interactive: interactive, Scoped: scope != nil, Identity: nativeQuotaOptions(options).identity}
	if scope != nil {
		policy.ScopePath = scope.path
	}
	encoded, _ := json.Marshal(policy)
	argv := append([]string{supervisorArg, string(encoded), target.Path}, target.Args...)
	cmd := exec.Command(executable, argv...)
	cmd.Env, cmd.Dir, cmd.WaitDelay = target.Env, target.Dir, target.WaitDelay
	cmd.Stdin, cmd.Stdout, cmd.Stderr = target.Stdin, target.Stdout, target.Stderr
	cmd.ExtraFiles = []*os.File{lifeR, statusW}
	if scope != nil {
		cmd.ExtraFiles = append(cmd.ExtraFiles, scope.file)
	}
	var terminal io.ReadWriteCloser
	if interactive {
		terminal, err = pty.Start(cmd)
	} else {
		prepareTask(cmd)
		err = cmd.Start()
	}
	if err != nil {
		lifeW.Close()
		statusR.Close()
		return nil, nil, err
	}
	_ = lifeR.Close()
	_ = statusW.Close()
	p := &unixProcess{scope: scope, cmd: cmd, life: lifeW, status: statusR, decoder: json.NewDecoder(io.LimitReader(statusR, 8192))}
	if err := statusR.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		_ = p.Kill()
		_ = cmd.Wait()
		_ = p.Close()
		if terminal != nil {
			_ = terminal.Close()
		}
		return nil, nil, err
	}
	var started supervisorReport
	if err := p.decoder.Decode(&started); err != nil || started.Error != "" {
		_ = p.Kill()
		_ = cmd.Wait()
		_ = p.Close()
		if terminal != nil {
			_ = terminal.Close()
		}
		if err != nil {
			return nil, nil, fmt.Errorf("supervisor startup failed: %w", err)
		}
		return nil, nil, errors.New(started.Error)
	}
	if err := statusR.SetReadDeadline(time.Time{}); err != nil {
		_ = p.Kill()
		_ = cmd.Wait()
		_ = p.Close()
		if terminal != nil {
			_ = terminal.Close()
		}
		return nil, nil, err
	}
	p.waitDone = make(chan struct{})
	// Target completion has its own report; guardian disposal must not consume
	// the target's deadline. Close always joins this reaper and copied streams.
	go func() { p.waitErr = cmd.Wait(); close(p.waitDone) }()
	owned = true
	return p, terminal, nil
}
