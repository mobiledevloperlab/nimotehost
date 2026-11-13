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
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows 10 1809+ provides ConPTY. The runtime probe fails closed on older OSes.
var terminalSupported = windows.NewLazySystemDLL("kernel32.dll").NewProc("CreatePseudoConsole").Find() == nil

func secureDirectory(path string, _ os.FileInfo) error {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
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

func startProcess(cmd *exec.Cmd, interactive bool, output io.Writer) (processHandle, io.ReadWriteCloser, error) {
	return startWindowsProcess(cmd, interactive, output, nil)
}
func startWindowsProcess(cmd *exec.Cmd, interactive bool, output io.Writer, protocol *windowsProtocolIO) (processHandle, io.ReadWriteCloser, error) {
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
	directory, err := windows.UTF16PtrFromString(cmd.Dir)
	if err != nil {
		return nil, nil, err
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
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return nil, nil, err
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
	for _, entry := range cmd.Env {
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
