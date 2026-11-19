package main

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// a project on a network disk that hangs refuses the new chat as soon as the check gives up.
func TestFolderCheck(t *testing.T) {
	dir := t.TempDir()
	if code := folderCheck(dir, time.Second, os.Stat); code != "" {
		t.Fatalf("a folder that is there: %q", code)
	}
	if code := folderCheck(dir+"/nope", time.Second, os.Stat); code != wire.RefuseNotFound {
		t.Fatalf("a folder that is gone: %q", code)
	}
	release := make(chan struct{})
	defer close(release)
	hung := func(string) (os.FileInfo, error) { <-release; return nil, errors.New("late") }
	start := time.Now()
	if code := folderCheck(dir, 50*time.Millisecond, hung); code != wire.RefuseTimeout {
		t.Fatalf("a folder that does not answer: %q", code)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("the check waited %v", d)
	}
}
