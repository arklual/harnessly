package design

import (
	"runtime"

	"golang.org/x/sys/unix"
)

// lowerPriority puts the calling goroutine on a thread of its own at the lowest CPU and idle I/O priority (nice 19,
// ionice idle): the thread ends with the goroutine, so nothing else ever runs at it.
func lowerPriority() {
	runtime.LockOSThread()
	tid := unix.Gettid()
	_ = unix.Setpriority(unix.PRIO_PROCESS, tid, 19)
	const ioprioWhoProcess, ioprioClassIdle, ioprioClassShift = 1, 3, 13
	_, _, _ = unix.Syscall(unix.SYS_IOPRIO_SET, ioprioWhoProcess, uintptr(tid), ioprioClassIdle<<ioprioClassShift)
}
