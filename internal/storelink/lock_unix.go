//go:build unix

package storelink

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// lockFolder takes a sync lock in the folder, `<folder>/<name>`: one process holds it at a time (one runtime syncs a
// folder several runtimes share, or a runtime does not sync its share of a team's library twice). errBusy when another
// holds it; a folder where the lock file cannot be made is synced without one.
func lockFolder(dir, name string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		return func() {}, nil
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errBusy
		}
		return func() {}, nil
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
