// SPDX-License-Identifier: Apache-2.0
//go:build linux || darwin

package host

import (
	"io"
	"os"
	"os/exec"
)

const structuredAgentSupported = true

func startProtocolProcess(cmd *exec.Cmd, output, diagnostics io.Writer, options ...ResourceLimits) (processHandle, io.WriteCloser, error) {
	if err := validateNativeQuotas(nativeQuotaOptions(options)); err != nil {
		return nil, nil, err
	}
	inputR, input, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	defer inputR.Close()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inputR, output, diagnostics
	process, _, err := startSupervisedProcess(cmd, false, options...)
	if err != nil {
		input.Close()
		return nil, nil, err
	}
	return process, input, nil
}
