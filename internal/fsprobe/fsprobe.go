// Package fsprobe asks the file system about a path without letting a dead mount eat the process: a stat on a hung NFS
// share blocks in the kernel for good, and the Go runtime starts a new OS thread for every blocked call (HRN-1735:
// 299 threads, 283 of them stuck, on a machine whose NFS server was gone). Here a probe has a deadline and the
// number of probes that may be stuck at once is capped, so the extra callers get an error at once instead of a thread.
package fsprobe

import (
	"errors"
	"os"
	"time"
)

// Timeout is how long a probe may take before the caller gives up on it.
const Timeout = 3 * time.Second

// MaxStuck is how many probes may be in flight at once; one more is refused without touching the disk.
const MaxStuck = 16

// ErrUnavailable is returned when the path did not answer in time or too many probes are already waiting.
var ErrUnavailable = errors.New("fsprobe: path is not answering")

var slots = make(chan struct{}, MaxStuck)

type result struct {
	info os.FileInfo
	err  error
}

// Stat is os.Stat with a deadline and a cap on probes in flight.
func Stat(path string) (os.FileInfo, error) { return stat(path, Timeout, os.Stat) }

func stat(path string, timeout time.Duration, call func(string) (os.FileInfo, error)) (os.FileInfo, error) {
	select {
	case slots <- struct{}{}:
	default:
		return nil, ErrUnavailable
	}
	done := make(chan result, 1)
	go func() {
		info, err := call(path)
		<-slots // freed only when the kernel answers, so a stuck call keeps its slot
		done <- result{info, err}
	}()
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case r := <-done:
		return r.info, r.err
	case <-t.C:
		return nil, ErrUnavailable
	}
}
