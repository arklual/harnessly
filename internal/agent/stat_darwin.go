//go:build darwin

package agent

import "syscall"

func statMtime(st *syscall.Stat_t) (int64, int64) {
	return int64(st.Mtimespec.Sec), int64(st.Mtimespec.Nsec)
}
