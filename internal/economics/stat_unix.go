//go:build linux || darwin

package economics

import (
	"os"
	"syscall"
)

// devIno are the device and inode numbers Node's fs.Stats gives.
func devIno(fi os.FileInfo) (dev, ino float64) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return float64(st.Dev), float64(st.Ino)
	}
	return 0, 0
}
