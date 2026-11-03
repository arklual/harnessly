//go:build linux || darwin

package agent

import (
	"errors"
	"syscall"
)

// statfsDisk is the total and used bytes of the filesystem that holds path, the way df counts them: used against used
// plus what a user can still take (the root's reserve is not "free").
func statfsDisk(path string) (total, used uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bs := uint64(st.Bsize)
	blocks, free, avail := uint64(st.Blocks), uint64(st.Bfree), uint64(st.Bavail)
	if blocks == 0 || bs == 0 || free > blocks {
		return 0, 0, errors.New("filesystem reports no size")
	}
	u := blocks - free
	return (u + avail) * bs, u * bs, nil
}
