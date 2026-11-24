// SPDX-License-Identifier: Apache-2.0
package hostcli

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestDeviceBridgeRejectsNonRPCCommands(t *testing.T) {
	for _, command := range []string{"", "sh", "sftp", "nimotehost start", "nimotehost install", "nimotehost devices list", "nimotehost mcp", "nimotehost rpc\nsh"} {
		t.Run(command, func(t *testing.T) {
			t.Setenv("SSH_ORIGINAL_COMMAND", command)
			if err := deviceBridge(context.Background(), strings.Repeat("a", 64), strings.NewReader(""), io.Discard); err == nil {
				t.Fatal("accepted non-RPC command")
			}
		})
	}
}

func TestScopedDeviceCLI(t *testing.T) {
	dir := t.TempDir()
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "APPDATA", "USERPROFILE"} {
		t.Setenv(key, dir)
	}
	var out strings.Builder
	id := strings.Repeat("a", 64)
	if err := devicesCommand([]string{"add-scoped", "phone", "session_operator", id}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"role":"session_operator"`) || !strings.Contains(out.String(), id) {
		t.Fatal(out.String())
	}
	out.Reset()
	if err := devicesCommand([]string{"list"}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "credential") || !strings.Contains(out.String(), `"sessions"`) {
		t.Fatal(out.String())
	}
	if err := devicesCommand([]string{"add-scoped", "bad", "owner", id}, io.Discard); err == nil {
		t.Fatal("privileged CLI role accepted")
	}
}
