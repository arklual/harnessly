package apps

import (
	"os"
	"syscall"
)

// changedAt is the inode's change time: a write or a touch moves it, and only root can set it back, so a stamp with it
// cannot be kept by restoring the size and the modification time of an edited file.
func changedAt(info os.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Ctim.Nano()
	}
	return 0
}
