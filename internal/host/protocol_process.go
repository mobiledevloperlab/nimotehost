// SPDX-License-Identifier: Apache-2.0
package host

import (
	"context"
	"io"
	"os/exec"
	"time"
)

func startStructuredAgent(cmd *exec.Cmd, a *structuredAgent, options ...ResourceLimits) (processHandle, error) {
	if err := configureStructuredCommand(cmd, a); err != nil {
		return nil, err
	}
	return startConfiguredStructuredAgent(cmd, a, options...)
}

func startConfiguredStructuredAgent(cmd *exec.Cmd, a *structuredAgent, options ...ResourceLimits) (processHandle, error) {
	a.prepareRPCTransport()
	process, input, err := startProtocolProcess(cmd, &agentLines{writer: a.rpc.writer, onError: a.fail, ready: a.rpc.ready}, &a.diagnostics, options...)
	if err != nil {
		a.close()
		return nil, err
	}
	a.input = input
	a.stopProcess = process.Kill
	a.startProtocolIO()
	if err := a.initialize(); err != nil {
		_ = process.Kill()
		a.close()
		_, _ = process.Wait()
		_ = process.Close()
		return nil, err
	}
	return process, nil
}

// Native history helpers share the same tree ownership as live sessions. Every
// pipe, context callback and wait worker is joined on success, cancellation and
// failed handshakes; query lifetime never owns a managed Agent session.
func startAgentQuery(ctx context.Context, cmd *exec.Cmd) (io.WriteCloser, io.ReadCloser, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	reader, writer := io.Pipe()
	var diagnostics agentDiagnostics
	cmd.WaitDelay = 2 * time.Second
	process, input, err := startProtocolProcess(cmd, &agentLines{writer: writer, onError: func(err error) { _ = writer.CloseWithError(err) }}, &diagnostics)
	if err != nil {
		reader.Close()
		writer.Close()
		return nil, nil, nil, err
	}
	done := make(chan struct{})
	go func() { _, err := process.Wait(); _ = writer.CloseWithError(err); close(done) }()
	stop := context.AfterFunc(ctx, func() { _ = input.Close(); _ = reader.CloseWithError(ctx.Err()); _ = process.Kill() })
	cleanup := func() {
		stop()
		_ = process.Kill()
		_ = input.Close()
		_ = reader.Close()
		_ = writer.Close()
		<-done
		_ = process.Close()
	}
	return input, reader, cleanup, nil
}
