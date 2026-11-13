// SPDX-License-Identifier: Apache-2.0
//go:build windows

package host

import (
	"errors"
	"io"
	"os"
	"os/exec"
)

// Redirected pipes do not depend on ConPTY. Process trees still use the native
// atomic Job Object assignment shared with tasks and interactive terminals.
const structuredAgentSupported = true

func startStructuredAgent(cmd *exec.Cmd, a *structuredAgent) (processHandle, error) {
	if err := configureStructuredCommand(cmd, a); err != nil {
		return nil, err
	}
	inputR, inputW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer inputR.Close()
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		inputW.Close()
		return nil, err
	}
	defer stderrW.Close()
	a.prepareRPCTransport()
	process, _, err := startWindowsProcess(cmd, false, &agentLines{writer: a.rpc.writer, onError: a.fail, ready: a.rpc.ready}, &windowsProtocolIO{input: inputR, stderr: stderrW})
	if err != nil {
		inputW.Close()
		stderrR.Close()
		a.close()
		return nil, err
	}
	_ = inputR.Close()
	_ = stderrW.Close()
	handle := &windowsStructuredProcess{process: process, input: inputW, stderr: stderrR, stderrDone: make(chan struct{})}
	a.input = inputW
	a.stopProcess = process.Kill
	go func() { _, _ = io.Copy(&a.diagnostics, stderrR); close(handle.stderrDone) }()
	a.startProtocolIO()
	if err = a.initialize(); err != nil {
		_ = process.Kill()
		_, _ = handle.Wait()
		a.close()
		_ = handle.Close()
		return nil, err
	}
	return handle, nil
}

type windowsStructuredProcess struct {
	process       processHandle
	input, stderr *os.File
	stderrDone    chan struct{}
}

func (p *windowsStructuredProcess) Wait() (int, error) {
	code, err := p.process.Wait()
	// Kill also covers abnormal wait failures before joining the stderr reader.
	_ = p.process.Kill()
	<-p.stderrDone
	return code, err
}
func (p *windowsStructuredProcess) Kill() error { return p.process.Kill() }
func (p *windowsStructuredProcess) Close() error {
	// Agent.close may already have closed stdin to wake its writer.
	_ = p.input.Close()
	return errors.Join(p.stderr.Close(), p.process.Close())
}
