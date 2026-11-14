// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Nimote contributors

//go:build windows

package host

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows 10 1809+ provides ConPTY. The runtime probe fails closed on older OSes.
var terminalSupported = windows.NewLazySystemDLL("kernel32.dll").NewProc("CreatePseudoConsole").Find() == nil

func currentOwnerDACL() (*windows.ACL, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return nil, err
	}
	dacl, _, err := sd.DACL()
	return dacl, err
}
func secureDirectory(path string, _ os.FileInfo) error {
	dacl, err := currentOwnerDACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}
func secureFile(path string, info os.FileInfo) error { return secureDirectory(path, info) }
func lockState(file *os.File) error {
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
}

type windowsTerminal struct {
	mu            sync.RWMutex
	closed        bool
	input, output *os.File
	console       windows.Handle
	once          sync.Once
}

func (p *windowsTerminal) Read(data []byte) (int, error)  { return p.output.Read(data) }
func (p *windowsTerminal) Write(data []byte) (int, error) { return p.input.Write(data) }
func (p *windowsTerminal) Close() error {
	p.once.Do(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.closed = true
		// Output is drained concurrently by Manager.wait while ConPTY emits its
		// final frame. Closing the output channel first can deadlock ConPTY teardown.
		windows.ClosePseudoConsole(p.console)
		p.input.Close()
		p.output.Close()
	})
	return nil
}
func resizeTerminal(terminal io.ReadWriteCloser, rows, cols uint16) error {
	p := terminal.(*windowsTerminal)
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return os.ErrClosed
	}
	return windows.ResizePseudoConsole(p.console, windows.Coord{X: int16(cols), Y: int16(rows)})
}

type windowsProcess struct {
	mu          sync.Mutex
	handle, job windows.Handle
	copied      chan struct{}
	pipe        *os.File
	closed      bool
}

func (p *windowsProcess) Wait() (int, error) {
	status, err := windows.WaitForSingleObject(p.handle, windows.INFINITE)
	if err != nil {
		return -1, err
	}
	if status != windows.WAIT_OBJECT_0 {
		return -1, errors.New("unexpected process wait result")
	}
	var code uint32
	if err := windows.GetExitCodeProcess(p.handle, &code); err != nil {
		return -1, err
	}
	// Terminate descendants before waiting for output EOF. A child must not keep
	// inherited pipe handles alive after its supervised root has exited.
	_ = p.Kill()
	if p.copied != nil {
		<-p.copied
	}
	if code != 0 {
		return int(code), fmt.Errorf("exit status %d", code)
	}
	return 0, nil
}
func (p *windowsProcess) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	return windows.TerminateJobObject(p.job, 1)
}
func (p *windowsProcess) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	err := errors.Join(windows.CloseHandle(p.handle), windows.CloseHandle(p.job))
	if p.pipe != nil {
		err = errors.Join(err, p.pipe.Close())
	}
	return err
}

// startProcess uses the Win32 process boundary only on Windows. Job-list startup
// attributes assign the entire process tree atomically, with kill-on-close.
// See docs/completed.md for the Microsoft ConPTY and Job Object contracts.
type windowsProtocolIO struct{ input, stderr *os.File }

func startProcess(cmd *exec.Cmd, interactive bool, output io.Writer, options ...ResourceLimits) (processHandle, io.ReadWriteCloser, error) {
	return startWindowsProcess(cmd, interactive, output, nil, options...)
}
func startWindowsProcess(cmd *exec.Cmd, interactive bool, output io.Writer, protocol *windowsProtocolIO, options ...ResourceLimits) (processHandle, io.ReadWriteCloser, error) {
	quota := nativeQuotaOptions(options)
	if err := validateNativeQuotas(quota); err != nil {
		return nil, nil, err
	}
	if cmd.Err != nil {
		return nil, nil, cmd.Err
	}
	executablePath, commandText, err := windowsCommand(cmd)
	if err != nil {
		return nil, nil, err
	}
	executable, err := windows.UTF16PtrFromString(executablePath)
	if err != nil {
		return nil, nil, err
	}
	commandLine, err := windows.UTF16PtrFromString(commandText)
	if err != nil {
		return nil, nil, err
	}
	var directory *uint16
	if cmd.Dir != "" {
		directory, err = windows.UTF16PtrFromString(cmd.Dir)
		if err != nil {
			return nil, nil, err
		}
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, nil, err
	}
	success := false
	defer func() {
		if !success {
			windows.CloseHandle(job)
		}
	}()
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if quota.MemoryBytes > 0 {
		limits.BasicLimitInformation.LimitFlags |= windows.JOB_OBJECT_LIMIT_JOB_MEMORY
		limits.JobMemoryLimit = uintptr(quota.MemoryBytes)
	}
	if quota.MaxProcesses > 0 {
		limits.BasicLimitInformation.LimitFlags |= windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS
		limits.BasicLimitInformation.ActiveProcessLimit = uint32(quota.MaxProcesses)
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return nil, nil, err
	}
	if quota.CPUPercent > 0 {
		// Win32 JOB_OBJECT_CPU_RATE_CONTROL_ENABLE | HARD_CAP; units 1/100 percent.
		rate := struct{ Flags, Rate uint32 }{0x1 | 0x4, uint32(quota.CPUPercent * 100)}
		if _, err := windows.SetInformationJobObject(job, windows.JobObjectCpuRateControlInformation, uintptr(unsafe.Pointer(&rate)), uint32(unsafe.Sizeof(rate))); err != nil {
			return nil, nil, err
		}
	}
	attrs, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		return nil, nil, err
	}
	defer attrs.Delete()
	// PROC_THREAD_ATTRIBUTE_JOB_LIST is supported on Windows 10+.
	if err := attrs.Update(0x0002000D, unsafe.Pointer(&job), unsafe.Sizeof(job)); err != nil {
		return nil, nil, err
	}
	si := windows.StartupInfoEx{StartupInfo: windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{}))}, ProcThreadAttributeList: attrs.List()}
	var terminal *windowsTerminal
	var pipeR, pipeW, stdin *os.File
	inherit := false
	if interactive {
		// Explicit null standard handles let ConPTY initialize console I/O.
		// Otherwise children can retain the SSH parent's redirected stdin and
		// exit immediately instead of reading from the pseudoconsole.
		si.Flags = windows.STARTF_USESTDHANDLES
		inputR, inputW, err := os.Pipe()
		if err != nil {
			return nil, nil, err
		}
		defer inputR.Close()
		outR, outW, err := os.Pipe()
		if err != nil {
			inputW.Close()
			return nil, nil, err
		}
		defer outW.Close()
		var console windows.Handle
		if err := windows.CreatePseudoConsole(windows.Coord{X: 80, Y: 24}, windows.Handle(inputR.Fd()), windows.Handle(outW.Fd()), 0, &console); err != nil {
			inputW.Close()
			outR.Close()
			return nil, nil, err
		}
		terminal = &windowsTerminal{input: inputW, output: outR, console: console}
		defer func() {
			if !success {
				go io.Copy(io.Discard, terminal.output)
				terminal.Close()
			}
		}()
		// HPCON itself is the attribute value, rather than a pointer to the handle.
		// Unlike other attributes, this API takes the opaque HPCON value itself.
		update := windows.NewLazySystemDLL("kernel32.dll").NewProc("UpdateProcThreadAttribute")
		result, _, attributeErr := update.Call(uintptr(unsafe.Pointer(attrs.List())), 0, windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, uintptr(console), unsafe.Sizeof(console), 0, 0)
		if result == 0 {
			return nil, nil, attributeErr
		}
	} else {
		pipeR, pipeW, err = os.Pipe()
		if err != nil {
			return nil, nil, err
		}
		defer pipeW.Close()
		defer func() {
			if !success {
				pipeR.Close()
			}
		}()
		if protocol != nil {
			stdin = protocol.input
		} else {
			stdin, err = os.Open(os.DevNull)
			if err != nil {
				return nil, nil, err
			}
			defer stdin.Close()
		}
		handles := []windows.Handle{windows.Handle(pipeW.Fd()), windows.Handle(stdin.Fd())}
		if protocol != nil {
			handles = append(handles, windows.Handle(protocol.stderr.Fd()))
		}
		for _, h := range handles {
			if err := windows.SetHandleInformation(h, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
				return nil, nil, err
			}
		}
		if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), uintptr(len(handles))*unsafe.Sizeof(handles[0])); err != nil {
			return nil, nil, err
		}
		si.Flags = windows.STARTF_USESTDHANDLES
		si.StdOutput = handles[0]
		si.StdErr = handles[0]
		if protocol != nil {
			si.StdErr = handles[2]
		}
		si.StdInput = handles[1]
		inherit = true
	}
	var environment *uint16
	// Win32 environment blocks have two final NULs. Embedded NULs are rejected.
	var block []uint16
	for _, entry := range cmd.Environ() {
		if strings.ContainsRune(entry, 0) {
			return nil, nil, errors.New("invalid environment")
		}
		value, err := windows.UTF16FromString(entry)
		if err != nil {
			return nil, nil, err
		}
		block = append(block, value...)
	}
	block = append(block, 0)
	if len(block) == 1 {
		block = append(block, 0)
	}
	environment = &block[0]
	pi := windows.ProcessInformation{}
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT)
	if !interactive {
		flags |= windows.CREATE_NO_WINDOW
	}
	if err := windows.CreateProcess(executable, commandLine, nil, nil, inherit, flags, environment, directory, &si.StartupInfo, &pi); err != nil {
		return nil, nil, err
	}
	windows.CloseHandle(pi.Thread)
	process := &windowsProcess{handle: pi.Process, job: job, pipe: pipeR}
	if !interactive {
		process.copied = make(chan struct{})
		go func() { _, _ = io.Copy(output, pipeR); close(process.copied) }()
	}
	success = true
	if interactive {
		return process, terminal, nil
	}
	return process, nil, nil
}

// npm-installed CLI agents commonly resolve to .cmd launchers. Invoke only
// simple argv through those wrappers, refusing cmd expansion characters rather
// than silently changing argv into shell syntax. Explicit cmd.exe tasks retain
// their intended shell semantics.
func windowsCommand(cmd *exec.Cmd) (string, string, error) {
	extension := strings.ToLower(filepath.Ext(cmd.Path))
	if extension == ".cmd" || extension == ".bat" {
		items := append([]string{cmd.Path}, cmd.Args[1:]...)
		for _, item := range items {
			if strings.ContainsAny(item, "\"\x00\r\n%!*&|<>()^") {
				return "", "", errors.New("batch launcher arguments contain shell expansion characters; use a native executable or an explicit cmd.exe task")
			}
		}
		shell, err := exec.LookPath("cmd.exe")
		if err != nil {
			return "", "", err
		}
		return shell, windows.ComposeCommandLine([]string{shell, "/d", "/s", "/c"}) + ` ""` + strings.Join(items, `" "`) + `""`, nil
	}
	if strings.EqualFold(filepath.Base(cmd.Path), "cmd.exe") && len(cmd.Args) > 2 {
		index := len(cmd.Args) - 2
		if strings.EqualFold(cmd.Args[index], "/c") || strings.EqualFold(cmd.Args[index], "/k") {
			return cmd.Path, windows.ComposeCommandLine(cmd.Args[:index+1]) + ` "` + cmd.Args[index+1] + `"`, nil
		}
	}
	return cmd.Path, windows.ComposeCommandLine(cmd.Args), nil
}

// Windows refuses directory rename while a descendant file handle is open,
// even with FILE_SHARE_DELETE. Keep this management-only lock beside state.
// Normalize the legacy name so migration and rollback retain the same lock.
func openManagementLockFile(path string) (*os.File, error) {
	state := filepath.Dir(path)
	base := filepath.Base(state)
	if strings.EqualFold(base, "nimote-host") {
		base = "nimotehost"
	}
	path = filepath.Join(filepath.Dir(state), "."+base+".control.lock")
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return nil, errors.New("management lock must be a regular file")
		}
		if err := secureFile(path, info); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err == nil {
		err = secureFile(path, info)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// Readers pin a complete snapshot without blocking atomic permission revocation.
func openPrivateReader(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.WRITE_DAC, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	dacl, err := currentOwnerDACL()
	if err == nil {
		err = windows.SetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
	}
	if err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}

// openPrivateReader protects the pinned object using a share-delete handle.
func secureReadFile(_ string, _ os.FileInfo) error { return nil }

// Transient sharing/ACL or antivirus handles may briefly veto a Win32 atomic
// replacement. Retry only that same metadata commit, never its user operation.
func replacePrivateFile(from, to string) error {
	deadline := time.Now().Add(time.Second)
	for {
		err := renamePrivateSnapshot(from, to)
		if err == nil || time.Now().After(deadline) || !(errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)) {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// FileRenameInfoEx POSIX replacement keeps share-delete readers on the old
// snapshot while publishing the new path atomically. Legacy kernels use the
// bounded MoveFileEx fallback and report a persistent sharing conflict honestly.
func renamePrivateSnapshot(from, to string) error {
	source, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(source, windows.DELETE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	defer func() {
		if h != 0 {
			windows.CloseHandle(h)
		}
	}()
	name, err := windows.UTF16FromString(to)
	if err != nil {
		return err
	}
	type renameInfo struct {
		Flags  uint32
		Root   windows.Handle
		Length uint32
		Name   [1]uint16
	}
	offset := int(unsafe.Offsetof(renameInfo{}.Name))
	buffer := make([]byte, offset+len(name)*2)
	header := (*renameInfo)(unsafe.Pointer(&buffer[0]))
	header.Flags = 3
	header.Length = uint32((len(name) - 1) * 2)
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(&buffer[offset])), len(name)), name)
	err = windows.SetFileInformationByHandle(h, windows.FileRenameInfoEx, &buffer[0], uint32(len(buffer)))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) || errors.Is(err, windows.ERROR_NOT_SUPPORTED) || errors.Is(err, windows.ERROR_INVALID_FUNCTION) {
		windows.CloseHandle(h)
		h = 0
		return os.Rename(from, to)
	}
	return err
}
