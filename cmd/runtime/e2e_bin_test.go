package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// The end-to-end tests run the real binary. It is built once for the package (each test used to link its own, five
// links of the same runtime) and copied to the path the test names: the runtime looks for its neighbours next
// to its own executable, so each test keeps a binary of its own in its own dir.
var e2eBin struct {
	once sync.Once
	dir  string
	path string
	out  []byte
	err  error
}

// buildRuntime puts the runtime built from this package at bin; goBin is the go command the test found.
func buildRuntime(t *testing.T, goBin, bin string) {
	t.Helper()
	e2eBin.once.Do(func() {
		e2eBin.dir, e2eBin.err = os.MkdirTemp("", "runtime-e2e-")
		if e2eBin.err != nil {
			return
		}
		e2eBin.path = filepath.Join(e2eBin.dir, "runtime")
		e2eBin.out, e2eBin.err = exec.Command(goBin, "build", "-o", e2eBin.path, ".").CombinedOutput()
	})
	if e2eBin.err != nil {
		t.Fatalf("build: %v\n%s", e2eBin.err, e2eBin.out)
	}
	if err := copyExecutable(e2eBin.path, bin); err != nil {
		t.Fatalf("build: %v", err)
	}
}

func copyExecutable(from, to string) error {
	if err := os.Link(from, to); err == nil {
		return nil
	}
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

// removeE2EBin drops the shared build when the package's tests are done.
func removeE2EBin() {
	if e2eBin.dir != "" {
		_ = os.RemoveAll(e2eBin.dir)
	}
}
