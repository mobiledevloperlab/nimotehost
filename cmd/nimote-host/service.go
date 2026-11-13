// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/mobiledevloperlab/nimote-host/internal/host"
)

const helpText = `Nimote Host — run tasks and terminals over your existing SSH connection.

Setup (run as your normal user):
  nimote-host setup         Verify this bundle, install at a fixed path and start Host

Advanced setup (binary already at a fixed installation path):
  nimote-host install       Install and start the background service; wait until ready
  nimote-host status        Check the running Host (JSON)
  nimote-host capabilities  Show available features and installed Agent CLIs (JSON)

Manage:
  nimote-host stop          Stop Host and its active tasks; wait until stopped
  nimote-host restart       Restart the installed service; terminates active tasks
  nimote-host uninstall     Stop and remove the service; keep credentials and records
  nimote-host start         Run in the foreground (without a background service)

Integrate:
  nimote-host rpc-stream    Persistent multiplexed JSON RPC over SSH stdio
  nimote-host rpc           Read one JSON RPC request from stdin
  nimote-host rpc-encoded <base64>
                           Send one base64-encoded JSON RPC request
  nimote-host mcp           Serve MCP over stdio (read-only by default)
  nimote-host version       Show the binary version
  nimote-host help          Show this help

Connect using the same SSH account and the absolute path to this binary.
Host listens only on 127.0.0.1:47831. No additional account or public port is needed.
Linux requires a systemd user manager; macOS/Windows require a logged-in user.
`

type serviceManager interface {
	Installed() (bool, error)
	Install(context.Context) error
	Uninstall(context.Context) error
	Restart(context.Context) error
	Diagnostics() string
}
type hostCaller func(context.Context, string, host.RPCRequest) ([]byte, error)

func manageService(ctx context.Context, command, dir string, out io.Writer, service serviceManager, call hostCaller) error {
	installed, err := service.Installed()
	if err != nil {
		return err
	}
	// Check before requesting shutdown, including for Windows scheduled tasks.
	if command == "restart" && !installed {
		return errors.New("Host service is not installed; run nimote-host install first")
	}
	if command == "uninstall" && !installed {
		_, err := fmt.Fprintln(out, "Host service is not installed. Credentials and task records are retained.")
		return err
	}
	if command == "install" {
		if _, err := call(ctx, dir, host.RPCRequest{Method: "GET", Path: "/v1/status"}); err == nil {
			return errors.New("Host is already running; installation was not changed. To reinstall, allow tasks to finish, then run nimote-host stop followed by nimote-host install")
		} else if !hostUnavailable(err) {
			return fmt.Errorf("cannot check the existing Host: %w", err)
		}
		if err := service.Install(ctx); err != nil {
			return err
		}
	} else {
		// Stop only through the authenticated API. Authentication, timeout and permission
		// failures must not be mistaken for a stopped Host.
		if _, err := call(ctx, dir, host.RPCRequest{Method: "POST", Path: "/v1/shutdown"}); err == nil {
			if err := awaitState(ctx, dir, false, call); err != nil {
				return err
			}
		} else if !hostUnavailable(err) {
			return fmt.Errorf("cannot stop Host: %w", err)
		}
		if command == "uninstall" {
			if err := service.Uninstall(ctx); err != nil {
				return err
			}
			_, err := fmt.Fprintln(out, "Host service removed. Credentials and task records are retained.")
			return err
		}
		if err := service.Restart(ctx); err != nil {
			return err
		}
	}
	if err := awaitState(ctx, dir, true, call); err != nil {
		return fmt.Errorf("service command completed, but Host is not ready: %w. %s", err, service.Diagnostics())
	}
	address := host.Address
	if config, configErr := host.ReadConfig(dir); configErr == nil {
		address = config.Address()
	}
	_, err = fmt.Fprintln(out, "Host is running on "+address+".\nNext: nimote-host capabilities\nConnect using your existing SSH account and the absolute path to this binary.")
	return err
}

func hostUnavailable(err error) bool {
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial" && errors.Is(err, connectionRefused)
}

func awaitState(ctx context.Context, dir string, running bool, call hostCaller) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := call(ctx, dir, host.RPCRequest{Method: "GET", Path: "/v1/status"})
		if running && err == nil {
			return nil
		}
		if !running && hostUnavailable(err) {
			return nil
		}
		if err != nil && !retryableStatusError(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// A connection can close between shutdown acknowledgement and the next poll.
// Retry transport errors, but require an actual refused connection before
// reporting a stopped Host. RPC authentication and protocol errors fail closed.
func retryableStatusError(err error) bool {
	if hostUnavailable(err) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var op *net.OpError
	return errors.As(err, &op)
}
