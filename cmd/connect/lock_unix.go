//go:build !windows

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// lockFile holds an exclusive lock on f for as long as the process lives; false when another process holds it.
func lockFile(f *os.File) bool {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) == nil
}
