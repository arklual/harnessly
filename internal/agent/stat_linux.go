//go:build linux

package agent

import "syscall"

func statMtime(st *syscall.Stat_t) (int64, int64) { return int64(st.Mtim.Sec), int64(st.Mtim.Nsec) }
