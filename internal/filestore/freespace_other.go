//go:build !linux && !darwin

package filestore

// freeSpace is unknown here: the store does not check its disk before taking an upload.
func freeSpace(string) (int64, bool) { return 0, false }
