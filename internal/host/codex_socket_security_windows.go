// SPDX-License-Identifier: Apache-2.0
//go:build windows

package host

import (
	"errors"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Never use secureFile/secureDirectory here: those helpers set Host-owned
// files' ACLs. An external Agent's security descriptor must remain unchanged.
func validateCodexSocket(endpoint string) (string, func(), error) {
	parent := filepath.Dir(endpoint)
	ancestor, err := filepath.EvalSymlinks(filepath.Dir(parent))
	if err != nil {
		return "", nil, errors.New("Codex socket directory is unavailable")
	}
	parent = filepath.Join(ancestor, filepath.Base(parent))
	endpoint = filepath.Join(parent, filepath.Base(endpoint))
	name, err := windows.UTF16PtrFromString(parent)
	if err != nil {
		return "", nil, errors.New("invalid Codex socket directory")
	}
	// FILE_LIST_DIRECTORY and omission of FILE_SHARE_DELETE pin this directory
	// against rename/replacement until the WebSocket handshake has finished.
	handle, err := windows.CreateFile(name, windows.READ_CONTROL|windows.FILE_LIST_DIRECTORY|windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return "", nil, errors.New("cannot pin Codex socket directory")
	}
	release := func() { _ = windows.CloseHandle(handle) }
	failed := true
	defer func() {
		if failed {
			release()
		}
	}()
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(handle, &info) != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return "", nil, errors.New("Codex socket directory is not a plain directory")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", nil, errors.New("cannot determine local socket principal")
	}
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	socketInfo, statErr := os.Lstat(endpoint)
	if err != nil || !privateCodexSecurityDescriptor(sd, user.User.Sid, true) || statErr != nil || socketInfo.Mode()&os.ModeSocket == 0 || socketInfo.Mode()&os.ModeSymlink != 0 || !privateCodexSocketACL(endpoint, user.User.Sid, false) {
		return "", nil, errors.New("Codex socket or directory ownership/ACL does not permit safe attachment")
	}
	failed = false
	return endpoint, release, nil
}

func privateCodexSocketACL(path string, user *windows.SID, directory bool) bool {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	// AF_UNIX uses a custom NTFS reparse point. Query that object itself;
	// following it as a normal file can fail or query the wrong object.
	handle, err := windows.CreateFile(name, windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	return privateCodexSecurityDescriptor(sd, user, directory)
}

func privateCodexSecurityDescriptor(sd *windows.SECURITY_DESCRIPTOR, user *windows.SID, directory bool) bool {
	if sd == nil || user == nil {
		return false
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !owner.Equals(user) {
		return false
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		return false
	} // A null DACL allows everyone.
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(acl, i, &ace) != nil || ace == nil {
			return false
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 || ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		// Unknown object/callback ACEs fail closed.
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceSize < 20 {
			return false
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.IsValid() {
			return false
		}
		if sid.Equals(user) || sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
			continue
		}
		// FILE_DELETE_CHILD (0x40) is not exported by x/sys/windows. It can
		// remove/replace a socket even without DELETE on the socket itself.
		const fileDeleteChild windows.ACCESS_MASK = 0x40
		mask := windows.ACCESS_MASK(windows.GENERIC_ALL|windows.GENERIC_WRITE|windows.FILE_WRITE_DATA|windows.FILE_APPEND_DATA|windows.FILE_WRITE_EA|windows.FILE_WRITE_ATTRIBUTES|windows.DELETE|windows.WRITE_DAC|windows.WRITE_OWNER) | fileDeleteChild
		if !directory {
			mask |= windows.ACCESS_MASK(windows.GENERIC_READ | windows.GENERIC_EXECUTE | windows.FILE_READ_DATA | windows.FILE_EXECUTE)
		}
		if ace.Mask&mask != 0 {
			return false
		}
	}
	return true
}
