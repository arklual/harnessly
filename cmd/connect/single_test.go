package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A second Connect on the same computer refuses to link instead of fighting the first one for the server.
func TestSecondInstanceRefuses(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "device.json")
	if os.Getenv("CONNECT_LOCK_CHILD") == "1" {
		return
	}
	t.Setenv("HARNESSLY_CONNECT_CONFIG", cfg)
	if err := single(); err != nil {
		t.Fatal(err)
	}
	// Another process with the same config folder.
	cmd := exec.Command(os.Args[0], "-test.run=TestSecondHelper")
	cmd.Env = append(os.Environ(), "CONNECT_LOCK_CHILD=1", "HARNESSLY_CONNECT_CONFIG="+cfg)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "REFUSED") {
		t.Fatalf("%v %s", err, out)
	}
}

func TestSecondHelper(t *testing.T) {
	if os.Getenv("CONNECT_LOCK_CHILD") != "1" {
		t.Skip()
	}
	if single() == errRunning {
		println("REFUSED")
	}
}
