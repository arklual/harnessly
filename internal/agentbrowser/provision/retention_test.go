package provision

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestOldVersionsRetainedWhileInUse(t *testing.T) {
	root := filepath.Join(t.TempDir(), "agent-browser", "chrome")
	old := filepath.Join(root, "123.0.0.1")
	chrome := filepath.Join(old, "chrome-linux64", "chrome")
	if err := os.MkdirAll(filepath.Dir(chrome), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chrome, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	lease, err := Retain(chrome)
	if err != nil {
		t.Fatal(err)
	}
	prune(root)
	if _, err = os.Stat(chrome); err != nil {
		t.Fatal("removed leased Chrome", err)
	}
	if err = lease.Close(); err != nil {
		t.Fatal(err)
	}
	// Legacy browser-host processes protect their executable even without the new lock.
	host := exec.Command("sh", "-c", "read line", "browser-host", "--chrome", chrome)
	in, err := host.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = host.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close(); _ = host.Wait() })
	prune(root)
	if _, err = os.Stat(chrome); err != nil {
		t.Fatal("removed Chrome used by a live host", err)
	}
	_ = in.Close()
	_ = host.Wait()
	prune(root)
	if _, err = os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("unused old version remains: %v", err)
	}
	// The same lock file remains valid across deletion, preventing an inode replacement race.
	if _, err = Retain(chrome); err == nil {
		t.Fatal("started a removed version")
	}
}
