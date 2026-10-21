package design

import (
	"errors"
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// Renaming a board file inside its project folder, which other users and agents can write: the folder is opened once,
// without following a link, and both names are used relative to it, so nothing on the way is resolved again and the
// new name can only land next to the old one. The old name must be a plain file (a link is refused, never followed),
// and a file that is at the new name is never replaced.

var errNotPlain = errors.New("not a plain file")

func plainName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\x00")
}

// renameInDir moves dir/from to dir/to; an existing dir/to fails with os.ErrExist.
func renameInDir(dir, from, to string) error {
	if !plainName(from) || !plainName(to) {
		return errors.New("a name with a path")
	}
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err := unix.Fstatat(fd, from, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return errNotPlain
	}
	return renameAt(fd, from, to, st)
}

func isExist(err error) bool { return errors.Is(err, syscall.EEXIST) || errors.Is(err, os.ErrExist) }
