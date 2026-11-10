package design

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func portLibrary(t *testing.T, data string, port int) *Library {
	t.Helper()
	t.Setenv("RUNTIME_LOCALE", "")
	l := New(Config{LibraryDir: t.TempDir(), DataDir: data, Port: port, Texts: Texts{Tag: "test-design", CLI: "acme"}})
	l.osUser = func() string { return "agent" }
	t.Cleanup(l.StopWatching)
	t.Cleanup(l.StopServer)
	return l
}

// A restarted runtime serves the library on the port it had: a page that outlived the restart still has that port.
func TestEnsureServerKeepsPortAcrossRestart(t *testing.T) {
	data := t.TempDir()
	// 18470 is the default preferred port; here it may be taken by the person's own runtime, which is the case.
	first := portLibrary(t, data, 0)
	port, err := first.EnsureServer()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(data, ServerPortFile))
	if err != nil || string(raw) != strconv.Itoa(first.cfg.Port)+" "+strconv.Itoa(port)+"\n" {
		t.Fatalf("port file = %q, %v; want %d", raw, err, port)
	}
	first.StopServer()
	for i := 0; i < 3; i++ {
		again := portLibrary(t, data, 0)
		got, err := again.EnsureServer()
		if err != nil || got != port {
			t.Fatalf("restart %d: port %d, %v; want %d", i, got, err, port)
		}
		again.StopServer()
	}
}

// The preferred port taken by another runtime of the machine: the first run takes a free one, and the next start has
// the same one (not another free port each time).
func TestEnsureServerPreferredPortTakenStaysPut(t *testing.T) {
	data := t.TempDir()
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	preferred := busy.Addr().(*net.TCPAddr).Port
	first := portLibrary(t, data, preferred)
	port, err := first.EnsureServer()
	if err != nil || port == preferred {
		t.Fatalf("port %d, %v; want a free one other than %d", port, err, preferred)
	}
	first.StopServer()
	again := portLibrary(t, data, preferred)
	if got, err := again.EnsureServer(); err != nil || got != port {
		t.Fatalf("after a restart: %d, %v; want %d", got, err, port)
	}
	// The preferred port changed by the administrator: the remembered one is not the answer to it.
	if other := portLibrary(t, data, preferred+1); other.rememberedPort() != 0 {
		t.Fatalf("remembered %d for another preferred port", other.rememberedPort())
	}
}

// A remembered port that something else took is not an error: the server takes a free one and remembers that.
func TestEnsureServerRememberedPortTaken(t *testing.T) {
	data := t.TempDir()
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	taken := busy.Addr().(*net.TCPAddr).Port
	if err := os.WriteFile(filepath.Join(data, ServerPortFile), []byte(strconv.Itoa(18470)+" "+strconv.Itoa(taken)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l := portLibrary(t, data, 0)
	port, err := l.EnsureServer()
	if err != nil || port == taken || port == 0 {
		t.Fatalf("port %d, %v; want a free one other than %d", port, err, taken)
	}
	if got := l.rememberedPort(); got != port {
		t.Fatalf("remembered %d, want %d", got, port)
	}
}

// The port asked for wins over the remembered one; no data directory, no file.
func TestEnsureServerAskedPortAndNoDataDir(t *testing.T) {
	data := t.TempDir()
	free, _ := net.Listen("tcp", "127.0.0.1:0")
	want := free.Addr().(*net.TCPAddr).Port
	free.Close()
	l := portLibrary(t, data, want)
	if port, err := l.EnsureServer(); err != nil || port != want {
		t.Fatalf("port %d, %v; want %d", port, err, want)
	}
	none := portLibrary(t, "", 0)
	if port, err := none.EnsureServer(); err != nil || port == 0 {
		t.Fatalf("without a data dir: %d, %v", port, err)
	}
}
