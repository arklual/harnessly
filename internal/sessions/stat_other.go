//go:build !linux

package sessions

import (
	"os"
	"time"
)

// fileTimes is a file's size and modification time; the birth time is not read on this system.
func fileTimes(path string) (size int64, mtime, birth time.Time, hasBirth, ok bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, time.Time{}, time.Time{}, false, false
	}
	return info.Size(), info.ModTime(), time.Unix(0, 0), false, true // Node reports a zero birth time then
}
