//go:build unix

package hookspool

import (
	"os"
	"syscall"
	"time"
)

// lock takes the file's lock, waiting no longer than wait: a hook must not hang on it.
func lock(f *os.File, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EINTR {
			return err
		}
		if time.Now().After(deadline) {
			return ErrBusy
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func unlock(f *os.File) { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
