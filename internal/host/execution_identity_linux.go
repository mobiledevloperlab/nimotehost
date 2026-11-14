// SPDX-License-Identifier: Apache-2.0
//go:build linux

package host

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const identityExecArg = "__nimotehost_identity_exec_v1"

func init() {
	if len(os.Args) > 1 && os.Args[1] == identityExecArg {
		os.Exit(runIdentityExec(os.Args[2:]))
	}
}
func runIdentityExec(args []string) int {
	if len(args) < 3 {
		return 125
	}
	uid, err := strconv.ParseUint(args[0], 10, 32)
	if err != nil || uid < 1000000 || uid > 2147483646 || uint32(os.Getuid()) != uint32(uid) || os.Geteuid() != os.Getuid() {
		return 125
	}
	runtime.LockOSThread()
	if unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != nil {
		return 125
	}
	// Prevent escaping the job through privileged Unix-socket activation brokers.
	// HTTP/TLS over IP and the existing stdio Agent protocols remain available.
	arch := uint32(unix.AUDIT_ARCH_X86_64)
	if runtime.GOARCH == "arm64" {
		arch = unix.AUDIT_ARCH_AARCH64
	}
	filter := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 4},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: arch, Jt: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_KILL_PROCESS},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		// io_uring can create sockets without invoking SYS_SOCKET. No ring FD
		// is inherited; deny setup so AF_UNIX cannot bypass the broker filter.
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: uint32(unix.SYS_IO_URING_SETUP), Jt: 0, Jf: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{Code: unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K, K: 0x40000000, Jt: 0, Jf: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_KILL_PROCESS},
		// Connected anonymous stream pairs support runtime signal wakeups but
		// cannot connect/listen as a named broker socket. Datagram/seqpacket
		// pairs remain denied because they can reconnect to external peers.
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: uint32(unix.SYS_SOCKETPAIR), Jt: 6, Jf: 0},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: uint32(unix.SYS_SOCKET), Jt: 0, Jf: 4},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 16},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.AF_INET, Jt: 2, Jf: 0},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.AF_INET6, Jt: 1, Jf: 0},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 16},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.AF_UNIX, Jt: 0, Jf: 3},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 24},
		{Code: unix.BPF_ALU | unix.BPF_AND | unix.BPF_K, K: 0xf},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SOCK_STREAM, Jt: 1, Jf: 0},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	}
	if installIdentityFilter(filter) != nil {
		return 125
	}
	_ = unix.Setpriority(unix.PRIO_PROCESS, 0, 19)
	if err := syscall.Exec(args[1], args[2:], os.Environ()); err != nil {
		return 126
	}
	return 126
}
func validateExecutionIdentity(identity *ExecutionIdentity, dir string, l ResourceLimits) error {
	if identity == nil {
		return nil
	}
	if os.Geteuid() != 0 || identity.UID < 1000000 || identity.UID > 2147483646 || identity.GID != identity.UID {
		return errors.New("isolated execution requires root and a dedicated UID/GID >= 1000000")
	}
	_, uidErr := user.LookupId(strconv.FormatUint(uint64(identity.UID), 10))
	var unknownUID user.UnknownUserIdError
	if uidErr == nil || !errors.As(uidErr, &unknownUID) {
		return errors.New("execution UID must be a verified unused numeric identity, not a login or service account")
	}
	_, gidErr := user.LookupGroupId(strconv.FormatUint(uint64(identity.GID), 10))
	var unknownGID user.UnknownGroupIdError
	if gidErr == nil || !errors.As(gidErr, &unknownGID) {
		return errors.New("execution GID must not be an existing group")
	}

	if l.CgroupRoot == "" {
		return errors.New("isolated execution requires cgroup containment")
	}
	if err := platformQuotaValidate(l); err != nil {
		return err
	}
	controls, err := cgroupControllers(l.CgroupRoot)
	if err != nil {
		return err
	}
	if !controls["memory"] || !controls["pids"] {
		return errors.New("isolated execution requires delegated memory and pids controllers")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || stat.Uid != identity.UID || info.Mode().Perm()&0077 != 0 {
		return errors.New("isolated workspace must be private and owned by its dedicated UID")
	}
	return validateIdentityBackend(l)
}
func identityEnvironment(identity *ExecutionIdentity, dir string) []string {
	return []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + dir, "USER=nimote-isolated", "LOGNAME=nimote-isolated", "TMPDIR=" + dir, "LANG=C.UTF-8", "TERM=xterm-256color"}
}
func configureIdentityChild(cmd *exec.Cmd, identity *ExecutionIdentity) error {
	if identity == nil {
		return nil
	}
	if os.Geteuid() != 0 || identity.UID < 1000000 || identity.UID > 2147483646 || identity.GID != identity.UID {
		return errors.New("invalid execution identity")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	args := append([]string{executable, identityExecArg, strconv.FormatUint(uint64(identity.UID), 10), cmd.Path}, cmd.Args...)
	cmd.Path, cmd.Args, cmd.Err = executable, args, nil
	cmd.SysProcAttr.Credential = &syscall.Credential{Uid: identity.UID, Gid: identity.GID, Groups: []uint32{}}
	return nil
}

func verifyUnusedExecutionUID(uid uint32) error {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "status"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return errors.New("cannot verify execution UID is unused")
		}
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(line, "Uid:") {
				continue
			}
			fields := strings.Fields(line)
			for _, field := range fields[1:] {
				n, err := strconv.ParseUint(field, 10, 32)
				if err == nil && uint32(n) == uid {
					return errors.New("execution UID is already in use by a process")
				}
			}
		}
	}
	return nil
}

// Validate the administrative boundary independently of any particular workspace.
func validateIdentityBackend(l ResourceLimits) error {
	if os.Geteuid() != 0 {
		return errors.New("execution identity requires root")
	}
	controls, err := cgroupControllers(l.CgroupRoot)
	if err != nil {
		return err
	}
	if !controls["memory"] || !controls["pids"] {
		return errors.New("memory and pids delegation required")
	}
	rootInfo, err := os.Stat(l.CgroupRoot)
	if err != nil {
		return err
	}
	rootStat, ok := rootInfo.Sys().(*syscall.Stat_t)
	if !ok || rootStat.Uid != 0 || rootInfo.Mode().Perm()&0077 != 0 {
		return errors.New("isolated cgroup delegation must be private and root-owned")
	}
	for _, name := range []string{"memory.max", "pids.max"} {
		raw, err := os.ReadFile(filepath.Join(l.CgroupRoot, name))
		if err != nil {
			return err
		}
		value, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
		if err != nil || value == 0 {
			return errors.New("finite aggregate memory and task budgets required")
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	for p := executable; ; p = filepath.Dir(p) {
		info, err := os.Stat(p)
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		// Root-owned sticky directories (e.g. /tmp) prevent replacement of the
		// root-owned next component, despite their public write permission.
		stickyParent := p != executable && info.IsDir() && info.Mode()&os.ModeSticky != 0
		if !ok || st.Uid != 0 || (info.Mode().Perm()&0022 != 0 && !stickyParent) || info.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
			return errors.New("Host executable and ancestors must be protected and root-owned")
		}
		if (p == executable && info.Mode().Perm()&0005 != 0005) || (p != executable && info.Mode().Perm()&0001 == 0) {
			return errors.New("Host executable must be globally readable and executable")
		}
		if p == "/" {
			break
		}
	}
	return nil
}
