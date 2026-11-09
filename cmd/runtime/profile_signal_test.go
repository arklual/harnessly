package main

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// The profiles are gzip'd pprof, one set per signal, and the folder keeps the newest few.
func TestWriteProfilesKeepsTheNewestSets(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 11, 8, 22, 0, 0, 0, time.UTC)
	for i := range profileSets + 2 {
		if _, err := writeProfiles(dir, at.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != profileSets*len(profileNames) {
		t.Fatalf("%d files, want %d", len(entries), profileSets*len(profileNames))
	}
	if _, err := os.Stat(filepath.Join(dir, "heap-20261108-220000.pprof")); !os.IsNotExist(err) {
		t.Fatal("the oldest set was kept")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "heap-20261108-220004.pprof"))
	if err != nil || !bytes.HasPrefix(raw, []byte{0x1f, 0x8b}) {
		t.Fatalf("not a gzip'd profile: %v", err)
	}
}

// SIGUSR1 writes a set.
func TestProfileOnSignal(t *testing.T) {
	dir := t.TempDir()
	profileOnSignal(dir)
	if err := syscall.Kill(os.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		if m, _ := filepath.Glob(filepath.Join(dir, "goroutine-*.pprof")); len(m) == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no profiles after SIGUSR1")
}
