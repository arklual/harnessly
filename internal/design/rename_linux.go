//go:build linux

package design

import "golang.org/x/sys/unix"

// renameAt is renameat2 with RENAME_NOREPLACE: atomic, never replaces, never follows a link.
func renameAt(fd int, from, to string, _ unix.Stat_t) error {
	return unix.Renameat2(fd, from, fd, to, unix.RENAME_NOREPLACE)
}
