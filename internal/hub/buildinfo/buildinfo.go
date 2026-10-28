// Package buildinfo holds what the linker puts into the hub binary.
package buildinfo

// Version is set at link time: -ldflags "-X github.com/ly-solutions/harnessly/internal/hub/buildinfo.Version=1.2.3".
var Version = "dev"
