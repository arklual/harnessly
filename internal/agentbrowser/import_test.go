package agentbrowser

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportChromeCopiesPrivatelyAndStripsSingletons(t *testing.T) {
	root := t.TempDir()
	source, dest := filepath.Join(root, "source"), filepath.Join(root, "copied")
	must(t, os.MkdirAll(filepath.Join(source, "Default"), 0755))
	must(t, os.WriteFile(filepath.Join(source, "Default", "Cookies"), []byte("fixture"), 0644))
	must(t, os.Symlink("missing", filepath.Join(source, "SingletonSocket")))
	must(t, os.WriteFile(filepath.Join(source, "SingletonCookie"), []byte("stale"), 0600))
	hostname, err := os.Hostname()
	must(t, err)
	must(t, os.Symlink(hostname+"-2147483647", filepath.Join(source, "SingletonLock")))
	warning, err := ImportChrome(t.Context(), source, dest)
	must(t, err)
	if !strings.Contains(warning, "system keyring") {
		t.Fatal(warning)
	}
	for _, name := range []string{"SingletonSocket", "SingletonCookie", "SingletonLock"} {
		if _, err = os.Lstat(filepath.Join(dest, name)); !os.IsNotExist(err) {
			t.Fatalf("copied %s", name)
		}
		if _, err = os.Lstat(filepath.Join(source, name)); err != nil {
			t.Fatalf("source changed: %s", name)
		}
	}
	for path, mode := range map[string]os.FileMode{dest: 0700, filepath.Join(dest, "Default"): 0700, filepath.Join(dest, "Default", "Cookies"): 0600} {
		st, err := os.Stat(path)
		must(t, err)
		if st.Mode().Perm() != mode {
			t.Fatalf("%s: %o", path, st.Mode().Perm())
		}
	}
	raw, err := os.ReadFile(filepath.Join(dest, "Default", "Cookies"))
	must(t, err)
	if string(raw) != "fixture" {
		t.Fatal(string(raw))
	}
}

func TestImportChromeRejectsUnsafeSources(t *testing.T) {
	for _, name := range []string{"live-lock", "foreign-lock", "symlink-file", "symlink-directory", "source-symlink", "destination-symlink", "traversal", "overlap", "nonempty", "canceled", "special-file"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			source, dest := filepath.Join(root, "source"), filepath.Join(root, "copied")
			must(t, os.MkdirAll(source, 0700))
			must(t, os.WriteFile(filepath.Join(source, "fixture"), []byte("fixture"), 0600))
			ctx := t.Context()
			switch name {
			case "live-lock":
				hostname, err := os.Hostname()
				must(t, err)
				must(t, os.Symlink(fmt.Sprintf("%s-%d", hostname, os.Getpid()), filepath.Join(source, "SingletonLock")))
			case "foreign-lock":
				must(t, os.Symlink("foreign-host-123", filepath.Join(source, "SingletonLock")))
			case "symlink-file":
				must(t, os.Symlink("fixture", filepath.Join(source, "link")))
			case "symlink-directory":
				must(t, os.Symlink(root, filepath.Join(source, "link")))
			case "source-symlink":
				must(t, os.Symlink(source, filepath.Join(root, "link")))
				source = filepath.Join(root, "link")
			case "destination-symlink":
				must(t, os.Symlink(source, dest))
			case "traversal":
				source = source + "/../source"
			case "overlap":
				dest = filepath.Join(source, "child")
			case "nonempty":
				must(t, os.MkdirAll(dest, 0700))
				must(t, os.WriteFile(filepath.Join(dest, "keep"), []byte("keep"), 0600))
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "special-file":
				// A Unix socket is not a profile file and must not be imported.
				listener := importSocket(t, filepath.Join(source, "socket"))
				defer listener.Close()
			}
			if _, err := ImportChrome(ctx, source, dest); err == nil {
				t.Fatal("unsafe import accepted")
			}
			if _, err := os.Stat(filepath.Join(root, "source", "fixture")); err != nil {
				t.Fatal("source changed")
			}
			staging, err := filepath.Glob(filepath.Join(root, ".chrome-import-*"))
			must(t, err)
			if len(staging) != 0 {
				t.Fatal("partial copy left behind")
			}
		})
	}
}

func importSocket(t *testing.T, path string) net.Listener {
	t.Helper()
	listener, err := net.Listen("unix", path)
	must(t, err)
	return listener
}
