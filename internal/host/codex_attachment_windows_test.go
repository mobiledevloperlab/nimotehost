// SPDX-License-Identifier: Apache-2.0
//go:build windows

package host

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/sys/windows"
)

func TestCodexWindowsReadOnlyACL(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	for _, tc := range []struct {
		name, owner, acl string
		directory, want  bool
	}{
		{"user-only", sid, "D:P(A;OICI;FA;;;" + sid + ")", true, true},
		{"system-admin", sid, "D:P(A;;FA;;;" + sid + ")(A;;FA;;;SY)(A;;FA;;;BA)", false, true},
		{"readable-dir", sid, "D:P(A;;FA;;;" + sid + ")(A;;FR;;;WD)", true, true},
		{"readable-socket", sid, "D:P(A;;FA;;;" + sid + ")(A;;FR;;;WD)", false, false},
		{"writable-dir", sid, "D:P(A;;FA;;;" + sid + ")(A;;FW;;;WD)", true, false},
		{"delete-child", sid, "D:P(A;;FA;;;" + sid + ")(A;;0x40;;;WD)", true, false},
		{"inherit-only", sid, "D:P(A;;FA;;;" + sid + ")(A;OICIIO;FA;;;WD)", true, true},
		{"null-dacl", sid, "D:NO_ACCESS_CONTROL", true, false},
		{"foreign-owner", "S-1-1-0", "D:P(A;;FA;;;" + sid + ")", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sd, err := windows.SecurityDescriptorFromString("O:" + tc.owner + tc.acl)
			if err != nil {
				t.Fatal(err)
			}
			before := sd.String()
			if got := privateCodexSecurityDescriptor(sd, user.User.Sid, tc.directory); got != tc.want {
				t.Fatalf("ACL accepted=%v, want %v", got, tc.want)
			}
			if sd.String() != before {
				t.Fatal("security validation changed the descriptor")
			}
		})
	}
}

// Runs on native Windows CI. Cross-compilation alone does not execute this.
func TestCodexWindowsControlSocket(t *testing.T) {
	directory, err := os.MkdirTemp("", "nc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	info, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	// Only the test-owned directory is prepared. Production never sets ACLs
	// on third-party sockets/directories.
	if err := secureDirectory(directory, info); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	// Elevated test runners can default new objects' owner to Administrators.
	// Prepare the fixture's explicit current-user ownership, as Codex does.
	setOwner := func(path string) {
		t.Helper()
		name, err := windows.UTF16PtrFromString(path)
		if err != nil {
			t.Fatal(err)
		}
		handle, err := windows.CreateFile(name, windows.WRITE_OWNER, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
			windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer windows.CloseHandle(handle)
		if err := windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION, user.User.Sid, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	setOwner(directory)
	endpoint := filepath.Join(directory, "control.sock")
	listener, err := net.Listen("unix", endpoint)
	if err != nil {
		t.Fatal("Windows AF_UNIX is required for control attachment:", err)
	}
	defer listener.Close()
	setOwner(endpoint)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		typ, data, err := c.Read(ctx)
		if err == nil {
			_ = c.Write(ctx, typ, data)
		}
	})}
	defer server.Close()
	go server.Serve(listener)
	before, err := windows.GetNamedSecurityInfo(directory, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	connection, err := dialCodexControl(ctx, ExternalAttachment{ExternalConnection: ExternalConnection{Transport: "codex-control", Endpoint: endpoint}})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Write([]byte(`{"id":1}`)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 64)
	n, err := connection.Read(buffer)
	if err != nil || string(buffer[:n]) != "{\"id\":1}\n" {
		t.Fatal(string(buffer[:n]), err)
	}
	after, err := windows.GetNamedSecurityInfo(directory, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil || before.String() != after.String() || !privateCodexSecurityDescriptor(after, user.User.Sid, true) {
		t.Fatal("socket attachment changed the provider ACL", err)
	}
}
