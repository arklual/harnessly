package apps

import (
	"os"
	"syscall"
)

// changedAt is the inode's change time (ctime_linux.go).
func changedAt(info os.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Ctimespec.Nano()
	}
	return 0
}
