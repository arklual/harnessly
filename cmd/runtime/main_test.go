package main

import (
	"os"
	"syscall"
	"testing"
)

// The goldens of the files the previous runtime left were taken under the fleet's umask 077 (its units run with
// UMask=0077, as the gate does); a runner with 022 would see other modes on the same writes.
func TestMain(m *testing.M) {
	// The wall's probe starts this binary as its helper (appBoundary): here it is the test binary, which does what the
	// runtime's main does with the subcommand instead of running the tests again.
	if ran, err := subcommand(os.Args[1:]); ran {
		if err != nil {
			os.Stderr.WriteString(err.Error() + "\n")
			os.Exit(1)
		}
		os.Exit(0)
	}
	syscall.Umask(0o077)
	// The goldens are the previous runtime's answers, which were in Russian.
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}
