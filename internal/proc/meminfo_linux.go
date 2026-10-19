package proc

import "syscall"

func sysMemRatio() (float64, bool) {
	var info syscall.Sysinfo_t
	if syscall.Sysinfo(&info) != nil || info.Totalram == 0 {
		return 0, false
	}
	return float64(info.Freeram) / float64(info.Totalram), true
}
