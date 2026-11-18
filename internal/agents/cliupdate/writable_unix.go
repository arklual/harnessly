//go:build unix

package cliupdate

import "golang.org/x/sys/unix"

func writable(dir string) bool { return unix.Access(dir, unix.W_OK) == nil }
