package main

import (
	"os"
	"syscall"
	"testing"
)

// The goldens of the files the previous runtime left were taken under the fleet's umask 077 (its units run with
// UMask=0077, as the gate does); a runner with 022 would see other modes on the same writes.
func TestMain(m *testing.M) {
	syscall.Umask(0o077)
	os.Exit(m.Run())
}
