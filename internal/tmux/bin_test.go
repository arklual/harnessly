package tmux

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIsProgramSkipsAGuardScript(t *testing.T) {
	script := filepath.Join(t.TempDir(), "tmux")
	if err := os.WriteFile(script, []byte("#!/bin/bash\nexec /usr/bin/tmux \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if isProgram(script) {
		t.Fatal("a guard script counted as the tmux program")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	real, err := filepath.EvalSymlinks(sh)
	if err != nil {
		t.Fatal(err)
	}
	if !isProgram(real) {
		t.Fatalf("%s is a program", real)
	}
}
