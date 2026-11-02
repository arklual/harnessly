package apps

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// What sandbox-exec adds, inside the mount namespace, before it becomes the agent's CLI. Both act on this thread and
// what it execs, so the caller holds the thread until the exec.
//
// A Landlock domain: the kernel then refuses the agent's process tree any ptrace-level access to a process outside it
// (/proc/<pid>/root, /proc/<pid>/mem, process_vm_writev, pidfd_getfd), on any kernel since 5.13 and whatever other
// security module the machine has; since 6.12 also signals to a process outside and connections to an abstract unix socket
// made outside. The domain restricts no file access (the wall's binds do that), so a program of the agent's that makes
// a namespace of its own (a browser's sandbox, a nested bwrap) works as before where the kernel has scopes.
//
// A seccomp filter: the ioctls that type into the terminal (TIOCSTI, TIOCLINUX) fail, whatever the kernel's
// dev.tty.legacy_tiocsti says. A shell outside the wall reads that terminal once the agent is gone.

// landlockABI is the kernel's Landlock version; 0 where it has none or has it off.
func landlockABI() int {
	abi, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 {
		return 0
	}
	return int(abi)
}

func harden() error {
	runtime.LockOSThread() // Not undone: the thread execs, or the process exits.
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("no_new_privs: %w", err)
	}
	return errors.Join(landlockDomain(), denyTerminalInput())
}

func landlockDomain() error {
	abi := landlockABI()
	if abi < 1 {
		return errors.New("landlock: the kernel has it off")
	}
	var attr unix.LandlockRulesetAttr
	size := unsafe.Sizeof(attr.Handled_access_fs)
	if abi >= landlockScopesABI {
		attr.Scoped = unix.LANDLOCK_SCOPE_SIGNAL | unix.LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET
		size = unsafe.Offsetof(attr.Scoped) + unsafe.Sizeof(attr.Scoped)
	} else {
		// No scopes: a domain needs something handled. It handles execute and the rule below allows it everywhere.
		attr.Handled_access_fs = unix.LANDLOCK_ACCESS_FS_EXECUTE
	}
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), size, 0)
	if errno != 0 {
		return fmt.Errorf("landlock: %w", errno)
	}
	defer unix.Close(int(fd))
	if abi < landlockScopesABI {
		root, err := unix.Open("/", unix.O_PATH|unix.O_CLOEXEC, 0)
		if err != nil {
			return fmt.Errorf("landlock: %w", err)
		}
		defer unix.Close(root)
		rule := unix.LandlockPathBeneathAttr{Allowed_access: unix.LANDLOCK_ACCESS_FS_EXECUTE, Parent_fd: int32(root)}
		if _, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, fd, unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&rule)), 0, 0, 0); errno != 0 {
			return fmt.Errorf("landlock: %w", errno)
		}
	}
	if _, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0); errno != 0 {
		return fmt.Errorf("landlock: %w", errno)
	}
	return nil
}

func denyTerminalInput() error {
	arch, ok := map[string]uint32{"amd64": unix.AUDIT_ARCH_X86_64, "arm64": unix.AUDIT_ARCH_AARCH64}[runtime.GOARCH]
	if !ok {
		return fmt.Errorf("seccomp: no filter for %s", runtime.GOARCH)
	}
	const (
		ld  = unix.BPF_LD | unix.BPF_W | unix.BPF_ABS
		jeq = unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K
		jge = unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K
		ret = unix.BPF_RET | unix.BPF_K
		// offsetof(struct seccomp_data, nr / arch / args[1]); the ioctl's request is 32 bits, the low word on these arches.
		offNr, offArch, offRequest = 0, 4, 24
		x32Bit                     = 0x40000000
	)
	allow, deny := uint32(unix.SECCOMP_RET_ALLOW), uint32(unix.SECCOMP_RET_ERRNO)|uint32(unix.EPERM)
	// Another syscall table (32-bit programs, x32) has other numbers, so nothing below could read it: it is refused.
	foreign := uint32(unix.SECCOMP_RET_ERRNO) | uint32(unix.ENOSYS)
	filter := []unix.SockFilter{
		{Code: ld, K: offArch},
		{Code: jeq, Jt: 1, K: arch},
		{Code: ret, K: foreign},
		{Code: ld, K: offNr},
		{Code: jge, Jf: 1, K: x32Bit},
		{Code: ret, K: foreign},
		{Code: jeq, Jt: 1, K: unix.SYS_IOCTL},
		{Code: ret, K: allow},
		{Code: ld, K: offRequest},
		{Code: jeq, Jt: 1, K: unix.TIOCSTI},
		{Code: jeq, Jf: 1, K: unix.TIOCLINUX},
		{Code: ret, K: deny},
		{Code: ret, K: allow},
	}
	prog := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	if _, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, 0, uintptr(unsafe.Pointer(&prog))); errno != 0 {
		return fmt.Errorf("seccomp: %w", errno)
	}
	return nil
}
