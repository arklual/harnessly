//go:build linux

package wire

import "syscall"

// freeBytes is what an unprivileged writer can still put on the disk of dir; known is false where it cannot be told.
func freeBytes(dir string) (free int64, known bool) {
	var st syscall.Statfs_t
	if syscall.Statfs(dir, &st) != nil {
		return 0, false
	}
	return int64(st.Bavail) * int64(st.Bsize), true
}
