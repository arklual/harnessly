package oomguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func withAdjustment(t *testing.T, value string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "oom_score_adj")
	if err := os.WriteFile(file, []byte(value+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old, oldProtected := selfFile, protected
	selfFile = file
	protected = sync.OnceValue(isProtected)
	t.Cleanup(func() { selfFile, protected = old, oldProtected })
}

func TestUnprotectedLeavesAnUnprotectedRuntimeAlone(t *testing.T) {
	withAdjustment(t, "0")
	program, args := Unprotected("tmux", "-L", "x", "new-session")
	if program != "tmux" || !reflect.DeepEqual(args, []string{"-L", "x", "new-session"}) {
		t.Fatalf("got %s %v", program, args)
	}
}

func TestUnprotectedResetsWhatAProtectedRuntimeStarts(t *testing.T) {
	withAdjustment(t, "-900")
	program, args := Unprotected("echo", "a b", "c")
	if program != "/bin/sh" {
		t.Fatalf("got %s %v", program, args)
	}
	// The wrapped command still runs the program with its arguments untouched.
	out, err := exec.Command(program, args...).Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "a b c" {
		t.Fatalf("got %q", out)
	}
}
