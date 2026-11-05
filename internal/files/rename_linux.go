//go:build linux

package files

import (
	"errors"

	"golang.org/x/sys/unix"
)

// renameNoReplace moves from to to unless to exists, in one step: nothing that appears there meanwhile is lost.
func renameNoReplace(from, to string) error {
	err := unix.Renameat2(unix.AT_FDCWD, from, unix.AT_FDCWD, to, unix.RENAME_NOREPLACE)
	switch {
	case errors.Is(err, unix.EEXIST):
		return ErrEntryExists
	case errors.Is(err, unix.ENOSYS), errors.Is(err, unix.EINVAL):
		// A file system that cannot: checked, then moved.
		return renameChecked(from, to)
	}
	return err
}
