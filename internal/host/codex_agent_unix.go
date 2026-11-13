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
	a.prepareRPCTransport()
	cmd.Stdout = &agentLines{writer: a.rpc.writer, onError: a.fail, ready: a.rpc.ready}
	cmd.Stderr = &a.diagnostics
	if err = cmd.Start(); err != nil {
		_ = input.Close()
		a.close()
		return nil, err
	}
	a.stopProcess = func() error { return killProcess(cmd) }
	a.startProtocolIO()
	if err = a.initialize(); err != nil {
		_ = killProcess(cmd)
		_ = cmd.Wait()
		a.close()
		return nil, err
	}
	return &unixProcess{cmd}, nil
}
