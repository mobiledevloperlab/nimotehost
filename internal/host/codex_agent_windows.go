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

func startProtocolProcess(cmd *exec.Cmd, output, diagnostics io.Writer, options ...ResourceLimits) (processHandle, io.WriteCloser, error) {
	inputR, inputW, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	defer inputR.Close()
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		inputW.Close()
		return nil, nil, err
	}
	defer stderrW.Close()
	process, _, err := startWindowsProcess(cmd, false, output, &windowsProtocolIO{input: inputR, stderr: stderrW}, options...)
	if err != nil {
		inputW.Close()
		stderrR.Close()
		return nil, nil, err
	}
	_ = inputR.Close()
	_ = stderrW.Close()
	handle := &windowsStructuredProcess{process: process, input: inputW, stderr: stderrR, stderrDone: make(chan struct{})}
	go func() { _, _ = io.Copy(diagnostics, stderrR); close(handle.stderrDone) }()
	return handle, inputW, nil
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
