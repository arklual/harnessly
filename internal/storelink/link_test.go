package storelink

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestDecide(t *testing.T) {
	m := &Machine{ID: "m1", DialURL: "wss://hub.example.org/agent/v1"}
	for _, c := range []struct {
		name string
		o    Options
		want string
	}{
		{"on its own", Options{Mode: ModeAuto}, KindEmbedded},
		{"empty mode is auto", Options{}, KindEmbedded},
		{"a hub's machine", Options{Mode: ModeAuto, Machine: m}, KindHub},
		{"a hub's machine paired too", Options{Mode: ModeAuto, Machine: m, Paired: true}, KindHub},
		{"paired with a cluster's agent", Options{Mode: ModeAuto, Paired: true}, ""},
		{"off", Options{Mode: ModeOff, Machine: m}, ""},
		{"embedded forced", Options{Mode: ModeEmbedded, Machine: m, Paired: true}, KindEmbedded},
		{"hub without a machine", Options{Mode: ModeHub}, ""},
		{"hub", Options{Mode: ModeHub, Machine: m}, KindHub},
	} {
		if got, why := Decide(c.o); got != c.want {
			t.Errorf("%s: %q (%s), want %q", c.name, got, why, c.want)
		}
	}
}

func TestOrigin(t *testing.T) {
	for in, want := range map[string]string{
		"wss://hub.example.org/agent/v1":  "https://hub.example.org",
		"ws://127.0.0.1:8080/agent/v1":    "http://127.0.0.1:8080",
		"wss://example.org/hub/agent/v1/": "https://example.org/hub",
		"https://hub.example.org":         "https://hub.example.org",
	} {
		if got, err := origin(in); err != nil || got != want {
			t.Errorf("origin(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "ftp://x/agent/v1", "not a url"} {
		if _, err := origin(bad); err == nil {
			t.Errorf("origin(%q) took a bad url", bad)
		}
	}
}

// A runtime with no hub keeps its store inside the process: the library is recorded as revisions in the data folder,
// a second copy of the library (a workspace that moved, a restart from scratch) gets it back from there, an upload is
// read by its hash, and it all outlives a restart of the runtime.
func TestEmbeddedStoreKeepsTheLibrary(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	open := func() *Link {
		l, err := Open(Options{DataDir: data, Name: "Big Box!", Log: quiet()})
		if err != nil {
			t.Fatal(err)
		}
		if l == nil || l.Kind() != KindEmbedded || l.Embedded() == nil {
			t.Fatalf("a runtime on its own must keep a store inside: %+v", l)
		}
		return l
	}
	l := open()
	lib := filepath.Join(dir, "library")
	write(t, lib, "p1/project/index.html", "<h1>v1</h1>")

	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); l.SyncFolder(ctx, filestore.KindDesign, func() string { return lib }) }()

	// The edit is recorded as a revision in the store.
	c := l.Client(filestore.KindDesign)
	waitFor(t, 10*time.Second, func() bool {
		e, err := c.Current(context.Background(), "p1/project/index.html")
		return err == nil && e != nil && e.Size == int64(len("<h1>v1</h1>"))
	})
	if _, err := os.Stat(filepath.Join(data, "store")); err != nil {
		t.Fatalf("the store is not in the data folder: %v", err)
	}

	// An upload is put and read back by its hash.
	up := l.Client(filestore.KindUploads)
	e, err := up.Put(context.Background(), "chat-1/shot.png", []byte("\x89PNG pixels"), "big-box")
	if err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	if err := up.GetBlob(context.Background(), e.Sha, &got); err != nil || got.String() != "\x89PNG pixels" {
		t.Fatalf("upload by hash: %q %v", got.String(), err)
	}
	stop()
	<-done
	l.Close()

	// The runtime starts again with an empty library elsewhere: the store gives it all back.
	l = open()
	defer l.Close()
	moved := filepath.Join(dir, "library-2")
	ctx, stop = context.WithCancel(context.Background())
	done = make(chan struct{})
	go func() { defer close(done); l.SyncFolder(ctx, filestore.KindDesign, func() string { return moved }) }()
	defer func() { stop(); <-done }()
	waitFor(t, 10*time.Second, func() bool {
		b, _ := os.ReadFile(filepath.Join(moved, "p1", "project", "index.html"))
		return string(b) == "<h1>v1</h1>"
	})
	got.Reset()
	if err := l.Client(filestore.KindUploads).GetBlob(context.Background(), e.Sha, &got); err != nil || got.String() != "\x89PNG pixels" {
		t.Fatalf("upload after a restart: %q %v", got.String(), err)
	}
}

func TestOpenOffAndPaired(t *testing.T) {
	for _, o := range []Options{{Mode: ModeOff}, {Paired: true}} {
		o.DataDir, o.Log = t.TempDir(), quiet()
		l, err := Open(o)
		if err != nil || l != nil {
			t.Fatalf("%+v: %v %v", o, l, err)
		}
		if _, err := os.Stat(filepath.Join(o.DataDir, "store")); !os.IsNotExist(err) {
			t.Fatalf("%+v: a store was made: %v", o, err)
		}
	}
}

func TestSanitize(t *testing.T) {
	for in, want := range map[string]string{"Big Box!": "big-box", "": "machine", "---": "machine", "vm_1": "vm_1"} {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, within time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("not within %s", within)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
