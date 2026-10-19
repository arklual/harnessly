//go:build !linux

package proc

func sysMemRatio() (float64, bool) { return 0, false }
