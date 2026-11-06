//go:build unix

package supervise

import (
	"strconv"
	"syscall"
)

func killPid(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	return syscall.Kill(n, syscall.SIGKILL)
}

func alive(s string) bool {
	n, err := strconv.Atoi(s)
	if err != nil {
		return false
	}
	return syscall.Kill(n, 0) == nil
}
