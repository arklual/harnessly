//go:build unix

package selfupdate

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockRoot takes the machine's rollout lock: two stages or activations never run at once on a machine.
func lockRoot(root string) (func(), error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(root, ".rollout.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("another rollout is still running on this machine")
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
