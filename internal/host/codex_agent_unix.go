// SPDX-License-Identifier: Apache-2.0
//go:build linux || darwin

package host

import (
	"os/exec"
)

const structuredAgentSupported = true

func startStructuredAgent(cmd *exec.Cmd, a *structuredAgent) (processHandle, error) {
	if err := configureStructuredCommand(cmd, a); err != nil {
		return nil, err
	}
	prepareTask(cmd)
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	a.input = input
	cmd.Stdout = &agentLines{agent: a}
	cmd.Stderr = &a.diagnostics
	if err = cmd.Start(); err != nil {
		_ = input.Close()
		return nil, err
	}
	a.stopProcess = func() error { return killProcess(cmd) }
	go a.writeLoop()
	if err = a.initialize(); err != nil {
		_ = killProcess(cmd)
		_ = cmd.Wait()
		a.close()
		return nil, err
	}
	return &unixProcess{cmd}, nil
}
