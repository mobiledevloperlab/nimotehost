// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestDeviceBridgeRejectsNonRPCCommands(t *testing.T) {
	for _, command := range []string{"", "sh", "sftp", "nimote-host start", "nimote-host install", "nimote-host devices list", "nimote-host mcp", "nimote-host rpc\nsh"} {
		t.Run(command, func(t *testing.T) {
			t.Setenv("SSH_ORIGINAL_COMMAND", command)
			if err := deviceBridge(context.Background(), strings.Repeat("a", 64), strings.NewReader(""), io.Discard); err == nil {
				t.Fatal("accepted non-RPC command")
			}
		})
	}
}
