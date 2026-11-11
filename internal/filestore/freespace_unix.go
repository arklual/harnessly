//go:build linux || darwin

package filestore

import "golang.org/x/sys/unix"

// freeSpace is how many bytes the disk of dir still takes for an unprivileged writer.
func freeSpace(dir string) (int64, bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return int64(st.Bavail) * int64(st.Bsize), true
}
