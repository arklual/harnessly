package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The app guard runs the CLI shipped beside the runtime when the deployment names none: the runtime's PATH need not
// hold harnessly (on a machine it does not), and a guard that is not found keeps every chat with apps from starting.
func TestBesideRuntime(t *testing.T) {
	dir := t.TempDir()
	runtime := filepath.Join(dir, "runtime")
	noExe := func() (string, error) { return "", errors.New("none") }
	if got := besideRuntime(runtime, noExe); got != "" {
		t.Fatalf("no cli: %q", got)
	}
	cli := filepath.Join(dir, "harnessly")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := besideRuntime(runtime, noExe); got != "" {
		t.Fatalf("not executable: %q", got)
	}
	if err := os.Chmod(cli, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := besideRuntime(runtime, noExe); got != cli {
		t.Fatalf("beside arg0: %q", got)
	}
	exe := func() (string, error) { return runtime, nil }
	if got := besideRuntime("runtime", exe); got != cli {
		t.Fatalf("beside the executable: %q", got)
	}
}
