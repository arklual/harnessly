//go:build !linux

package wire

func freeBytes(string) (int64, bool) { return 0, false }
