//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFile holds an exclusive lock on f for as long as the process lives; false when another process holds it.
func lockFile(f *os.File) bool {
	ol := new(windows.Overlapped)
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol) == nil
}
