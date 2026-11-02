//go:build !linux

package apps

// landlockABI: only Linux has it.
func landlockABI() int { return 0 }
