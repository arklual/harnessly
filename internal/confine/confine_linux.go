//go:build linux

// Package confine puts the process that calls it under Landlock (files, TCP ports) and seccomp (no sockets), then
// becomes the program it was asked to run. What a stand of agent evals and an untrusted kit build run under.
package confine

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Available says whether Landlock confines a process here (its ABI version; 0 for none).
func Available() int {
	v, _ := Probe()
	return v
}

// Probe returns the ABI or the reason this execution environment cannot provide it.
func Probe() (int, error) {
	v, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	switch errno {
	case 0:
		return int(v), nil
	case unix.ENOSYS:
		return 0, fmt.Errorf("this execution environment does not expose the Landlock system call: %w", errno)
	case unix.EOPNOTSUPP:
		return 0, fmt.Errorf("the kernel does not enable the Landlock security module: %w", errno)
	case unix.EPERM, unix.EACCES:
		return 0, fmt.Errorf("the Landlock ABI query was denied: %w", errno)
	default:
		return 0, fmt.Errorf("Landlock ABI query failed: %w", errno)
	}
}

// handledFS are the file rights a ruleset of ABI version v handles: every right the kernel knows of, so what a rule
// does not grant is refused.
func handledFS(v int) uint64 {
	m := uint64(0x1fff) // EXECUTE … MAKE_SYM (ABI 1)
	if v >= 2 {
		m |= unix.LANDLOCK_ACCESS_FS_REFER
	}
	if v >= 3 {
		m |= unix.LANDLOCK_ACCESS_FS_TRUNCATE
	}
	if v >= 5 {
		m |= unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
	}
	return m
}

const readRights = unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR

// landlockRuleset is the ABI 1–4 shape of the kernel's struct; the kernel takes a struct of the size it is given.
type landlockRuleset struct {
	handledFS  uint64
	handledNet uint64
}

// Net says what the process may do with the network.
type Net int

const (
	// NetOpen leaves the network as it is.
	NetOpen Net = iota
	// NetPorts lets it connect over TCP only to the ports in Rules.Ports (and bind none): Landlock ABI 4 (Linux 6.7).
	NetPorts
	// NetNone makes socket() fail, for every family: no TCP, no UDP, no unix socket to a daemon of the machine
	// (socketpair stays). A program that never needs the network is cut off from it whatever it tries.
	NetNone
)

// Rules are what the confined process may do: read and write beneath RW; read and run beneath RO; everything else
// is refused, for every process it starts too.
type Rules struct {
	RW, RO []string
	Net    Net
	Ports  []uint16
}

// SandboxExec is Exec with the network left open.
func SandboxExec(rw, ro, argv []string) error { return Exec(Rules{RW: rw, RO: ro}, argv) }

const (
	ruleNetPort = 2 // LANDLOCK_RULE_NET_PORT
	netBindTCP  = 1 // LANDLOCK_ACCESS_NET_BIND_TCP
	netConnect  = 2 // LANDLOCK_ACCESS_NET_CONNECT_TCP
)

type landlockNetPort struct {
	allowed uint64
	port    uint64
}

// Exec confines this process by r and becomes argv. It does not return when it succeeds.
func Exec(r Rules, argv []string) error {
	rw, ro := r.RW, r.RO
	if len(argv) == 0 {
		return errors.New("sandbox: what to run")
	}
	v := Available()
	if v == 0 {
		return errors.New("sandbox: Landlock is not available on this kernel")
	}
	if r.Net == NetPorts && v < 4 {
		return errors.New("sandbox: this kernel's Landlock cannot limit TCP ports (it needs ABI 4, Linux 6.7)")
	}
	handled := handledFS(v)
	attr := landlockRuleset{handledFS: handled}
	// NetNone handles TCP too with no rule that allows it: a second layer under the seccomp filter (ABI 4 and up).
	if r.Net == NetPorts || (r.Net == NetNone && v >= 4) {
		attr.handledNet = netBindTCP | netConnect
	}
	size := unsafe.Sizeof(attr)
	if v < 4 {
		size = 8
	}
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), size, 0)
	if errno != 0 {
		return fmt.Errorf("sandbox: create ruleset: %v", errno)
	}
	add := func(path string, rights uint64) error {
		f, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
		if err != nil {
			if errors.Is(err, unix.ENOENT) {
				return nil
			}
			return fmt.Errorf("sandbox: %s: %v", path, err)
		}
		defer unix.Close(f)
		var st unix.Stat_t
		if err := unix.Fstat(f, &st); err == nil && st.Mode&unix.S_IFMT != unix.S_IFDIR {
			// A file takes only the rights of a file.
			rights &= unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_TRUNCATE | unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
		}
		rule := unix.LandlockPathBeneathAttr{Allowed_access: rights & handled, Parent_fd: int32(f)}
		if _, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, fd, unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&rule)), 0, 0, 0); errno != 0 {
			return fmt.Errorf("sandbox: rule %s: %v", path, errno)
		}
		return nil
	}
	for _, p := range ro {
		if err := add(p, readRights); err != nil {
			return err
		}
	}
	for _, p := range rw {
		if err := add(p, handled); err != nil {
			return err
		}
	}
	if r.Net == NetPorts {
		for _, port := range r.Ports {
			rule := landlockNetPort{allowed: netConnect, port: uint64(port)}
			if _, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, fd, ruleNetPort, uintptr(unsafe.Pointer(&rule)), 0, 0, 0); errno != 0 {
				return fmt.Errorf("sandbox: port rule %d: %v", port, errno)
			}
		}
	}
	// The restrictions apply to the calling OS thread. Keep the helper on that
	// thread through exec. If setup fails, its caller exits the helper.
	runtime.LockOSThread()
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("sandbox: no_new_privs: %v", err)
	}
	if _, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0); errno != 0 {
		return fmt.Errorf("sandbox: restrict: %v", errno)
	}
	_ = unix.Close(int(fd))
	if r.Net == NetNone {
		if err := denySockets(); err != nil {
			return err
		}
	}
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	return syscall.Exec(bin, argv, os.Environ())
}

// denySockets installs a seccomp filter under which socket() fails with EPERM, whatever the family. It is inherited by
// every process this one starts and cannot be lifted.
func denySockets() error {
	arch, ok := map[string]uint32{"amd64": unix.AUDIT_ARCH_X86_64, "arm64": unix.AUDIT_ARCH_AARCH64}[runtime.GOARCH]
	if !ok {
		return fmt.Errorf("sandbox: no seccomp filter for %s", runtime.GOARCH)
	}
	const (
		ld     = unix.BPF_LD | unix.BPF_W | unix.BPF_ABS
		jeq    = unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K
		jge    = unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K
		ret    = unix.BPF_RET | unix.BPF_K
		x32Bit = 0x40000000
	)
	foreign := uint32(unix.SECCOMP_RET_ERRNO) | uint32(unix.ENOSYS)
	filter := []unix.SockFilter{
		{Code: ld, K: 4}, // arch
		{Code: jeq, Jt: 1, K: arch},
		{Code: ret, K: foreign},
		{Code: ld, K: 0}, // nr
		{Code: jge, Jf: 1, K: x32Bit},
		{Code: ret, K: foreign},
		{Code: jeq, Jt: 2, K: unix.SYS_SOCKET},
		{Code: jeq, Jt: 1, K: unix.SYS_IO_URING_SETUP}, // io_uring has its own socket operations
		{Code: ret, K: unix.SECCOMP_RET_ALLOW},
		{Code: ret, K: uint32(unix.SECCOMP_RET_ERRNO) | uint32(unix.EPERM)},
	}
	prog := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	if _, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, 0, uintptr(unsafe.Pointer(&prog))); errno != 0 {
		return fmt.Errorf("sandbox: seccomp: %v", errno)
	}
	return nil
}
