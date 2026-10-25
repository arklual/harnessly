//go:build !linux

package design

// lowerPriority: only Linux lowers one thread's priority; elsewhere the reading is only slowed by its pauses.
func lowerPriority() {}
