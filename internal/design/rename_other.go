//go:build !linux

package design

import (
	"errors"

	"golang.org/x/sys/unix"
)

// renameAt without renameat2: a hard link (which never replaces and, with no flags, never follows a link), checked to
// be the very file that was looked at, then the old name removed only while it is still that file.
func renameAt(fd int, from, to string, seen unix.Stat_t) error {
	if err := unix.Linkat(fd, from, fd, to, 0); err != nil {
		return err
	}
	same := func(name string) bool {
		var st unix.Stat_t
		return unix.Fstatat(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW) == nil && st.Mode&unix.S_IFMT == unix.S_IFREG &&
			st.Dev == seen.Dev && st.Ino == seen.Ino
	}
	if !same(to) || !same(from) {
		_ = unix.Unlinkat(fd, to, 0)
		return errors.New("the board file changed while it was renamed")
	}
	return unix.Unlinkat(fd, from, 0)
}
