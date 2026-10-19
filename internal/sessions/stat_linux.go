//go:build linux

package sessions

import (
	"time"

	"golang.org/x/sys/unix"
)

// fileTimes is a file's size, modification and birth time (statx); ok false when it cannot be read.
func fileTimes(path string) (size int64, mtime, birth time.Time, hasBirth, ok bool) {
	var st unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, path, 0, unix.STATX_BASIC_STATS|unix.STATX_BTIME, &st); err != nil {
		return 0, time.Time{}, time.Time{}, false, false
	}
	return int64(st.Size), time.Unix(st.Mtime.Sec, int64(st.Mtime.Nsec)), time.Unix(st.Btime.Sec, int64(st.Btime.Nsec)),
		st.Mask&unix.STATX_BTIME != 0, true
}
