// SPDX-License-Identifier: Apache-2.0
package hostcli

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/mobiledevloperlab/nimotehost/internal/host"
	"io"
	"os"
	"strings"
)

func devicesCommand(args []string, out io.Writer) error {
	dir, err := host.DefaultDir()
	if err != nil {
		return err
	}
	if err := host.CheckStateName(dir); err != nil {
		return err
	}
	var result any
	switch {
	case len(args) == 1 && args[0] == "list":
		result, err = host.ListDevices(dir)
	case len(args) == 2 && args[0] == "add":
		result, err = host.CreateDevice(dir, args[1])
	case len(args) >= 3 && args[0] == "add-controller":
		result, err = host.CreateControllerDevice(dir, args[1], args[2:])
	case len(args) >= 4 && args[0] == "add-scoped":
		result, err = host.CreateScopedDevice(dir, args[1], args[2], args[3:])
	case len(args) == 2 && args[0] == "revoke":
		err = host.RevokeDevice(dir, args[1])
		result = map[string]bool{"revoked": err == nil}
	default:
		return errors.New("usage: nimotehost devices list | add <name> | add-scoped <name> <session_observer|session_operator> <session-id>... | add-controller <name> <session-id>... | revoke <id>")
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(result)
}
func deviceBridge(ctx context.Context, id string, in io.Reader, out io.Writer) error {
	// No shell evaluation: only these RPC bridges are accepted, regardless of the
	// command originally requested by the SSH peer. Device ACL is enforced by Host.
	original := os.Getenv("SSH_ORIGINAL_COMMAND")
	fields := strings.Fields(original)
	if len(fields) < 1 || strings.ContainsAny(original, "\x00\r\n") {
		return errors.New("device requires an SSH RPC command")
	}
	args := []string{fields[len(fields)-1]}
	if len(fields) > 1 && fields[len(fields)-2] == "rpc-encoded" {
		args = append([]string(nil), fields[len(fields)-2:]...)
		args[1] = strings.Trim(args[1], "\"'")
	}
	switch args[0] {
	case "rpc", "rpc-stream", "status", "capabilities":
		if len(args) != 1 {
			return errors.New("invalid bridge command")
		}
	case "rpc-encoded":
		if len(args) != 2 {
			return errors.New("invalid encoded command")
		}
	default:
		return errors.New("device key permits Host observation only; shell, SFTP and command execution are disabled")
	}
	// Validate the ID/credential even for rpc-stream's initial greeting.
	dir, err := host.DefaultDir()
	if err != nil {
		return err
	}
	if err := host.CheckStateName(dir); err != nil {
		return err
	}
	_, err = host.Call(host.DeviceContext(ctx, id), dir, host.RPCRequest{Method: "GET", Path: "/v1/status"})
	if err != nil {
		return err
	}
	return runWithExtension(host.DeviceContext(ctx, id), args, in, out, host.Extension{})
}
