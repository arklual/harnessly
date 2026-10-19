//go:build linux || darwin

package agent

import (
	"os"
	"syscall"
)

// fileID is the inode and the modification time in milliseconds, computed the way Node's fs.Stats does.
func fileID(info os.FileInfo) (inode uint64, mtimeMs float64) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		sec, nsec := statMtime(st)
		return uint64(st.Ino), float64(sec)*1000 + float64(nsec)/1e6
	}
	t := info.ModTime()
	return 0, float64(t.Unix())*1000 + float64(t.Nanosecond())/1e6
}
