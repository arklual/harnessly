//go:build !linux && !darwin

package apps

import "os"

// changedAt: no inode change time here; the stamp keeps the size and the modification time.
func changedAt(os.FileInfo) int64 { return 0 }
