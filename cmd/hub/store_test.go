package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/filesync"
	"github.com/ly-solutions/harnessly/internal/storelink"
)

// A store that ran inside a runtime is copied into the hub's as a space's room, backed up and collected; check-config
// says where the store lives.
func TestStoreMigrateBackupGC(t *testing.T) {
	ctx := context.Background()
	data, runtimeData := t.TempDir(), t.TempDir()
	l, err := storelink.Open(storelink.Options{DataDir: runtimeData, Name: "box", Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	e, err := l.Client(filestore.KindUploads).Put(ctx, "chat/shot.png", []byte("pixels"), "box")
	l.Close()
	if err != nil {
		t.Fatal(err)
	}
	hub := env(map[string]string{"HUB_PUBLIC_URL": "http://127.0.0.1:8080", "HUB_DATA_DIR": data})
	do := func(args ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := run(ctx, args, hub, &out, &errOut)
		return code, out.String() + errOut.String()
	}

	if code, out := do("check-config"); code != 0 || !strings.Contains(out, "store          SQLite in "+filepath.Join(data, "store")) {
		t.Fatalf("check-config: %d %s", code, out)
	}
	if code, out := do("store", "migrate", "--from", filepath.Join(runtimeData, "store"), "--rooms", "uploads", "--into-room", "uploads-sp1"); code != 0 || !strings.Contains(out, "verified true") {
		t.Fatalf("migrate: %d %s", code, out)
	}
	// The hub reads it as the uploads of space sp1.
	srv, err := filestore.OpenFolder(filepath.Join(data, "store"), func(_ *http.Request, coll string) (filestore.Access, error) {
		return filestore.Access{Member: "test", Room: coll + "-sp1"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	c := filesync.NewClient("http://store.invalid", filestore.KindUploads, nil, &http.Client{Transport: storelink.Handler(srv.Handler())})
	if err := c.GetBlob(ctx, e.Sha, &got); err != nil || got.String() != "pixels" {
		t.Fatalf("the hub's copy: %q %v", got.String(), err)
	}
	srv.Close()

	backup := t.TempDir()
	if code, out := do("store", "backup", "--dir", backup); code != 0 || !strings.Contains(out, "1 blobs copied") {
		t.Fatalf("backup: %d %s", code, out)
	}
	if code, out := do("store", "gc", "--dry-run"); code != 0 || !strings.Contains(out, "dry run") {
		t.Fatalf("gc: %d %s", code, out)
	}
	for _, bad := range [][]string{{"store"}, {"store", "nope"}, {"store", "backup"}, {"store", "migrate"},
		{"store", "migrate", "--from", "x", "--into-room", "a"}} {
		if code, _ := do(bad...); code != 2 {
			t.Errorf("%v: exit %d, want 2", bad, code)
		}
	}
	hub = env(map[string]string{"HUB_PUBLIC_URL": "http://127.0.0.1:8080", "HUB_DATA_DIR": data, "HUB_STORE": "off"})
	if code, out := do("check-config"); code != 0 || !strings.Contains(out, "store          off") {
		t.Fatalf("check-config, off: %d %s", code, out)
	}
}
