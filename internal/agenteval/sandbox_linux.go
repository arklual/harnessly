//go:build linux

package agenteval

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// SandboxAvailable says whether Landlock confines a stand here (its ABI version; 0 for none).
func SandboxAvailable() int {
	v, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 {
		return 0
	}
	return int(v)
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

// SandboxExec confines this process with Landlock (rw: read and write beneath; ro: read and run beneath; everything
// else refused, for every process it starts too) and becomes argv. It does not return when it succeeds.
func SandboxExec(rw, ro, argv []string) error {
	if len(argv) == 0 {
		return errors.New("sandbox: what to run")
	}
	v := SandboxAvailable()
	if v == 0 {
		return errors.New("sandbox: Landlock is not available on this kernel")
	}
	handled := handledFS(v)
	attr := landlockRuleset{handledFS: handled}
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
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("sandbox: no_new_privs: %v", err)
	}
	if _, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0); errno != 0 {
		return fmt.Errorf("sandbox: restrict: %v", errno)
	}
	_ = unix.Close(int(fd))
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	return syscall.Exec(bin, argv, os.Environ())
}
