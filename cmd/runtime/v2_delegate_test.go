package main

import (
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/tmux"
)

// The caller of a request is the chat whose pane holds the client's end of its connection: a request made by a
// process inside one session's pane is that session's conversation, one made from elsewhere is nobody's.
func TestDelegationCallerIsFoundByTheConnection(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl")
	}
	socket := fmt.Sprintf("hs-test-deleg-%d", rand.IntN(1_000_000_000))
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", socket, "kill-server").Run()
		dir := os.Getenv("TMUX_TMPDIR")
		if dir == "" {
			dir = "/tmp"
		}
		_ = os.Remove(filepath.Join(dir, "tmux-"+strconv.Itoa(os.Getuid()), socket))
	})
	found := make(chan [2]string, 4)
	cache := &callerCache{panes: tmux.New(tmux.Names{Socket: socket, Prefix: "hs-"}), convOf: func(session string) string {
		if session == "hs-deleg-a" {
			return "tab-a"
		}
		return ""
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The server ends each connection, so the client's port is free again at once.
		w.Header().Set("Connection", "close")
		id, why := cache.of(r)
		found <- [2]string{id, why}
	}))
	defer srv.Close()

	// From inside the session's pane, two levels below its shell.
	port := 20000 + rand.IntN(20000)
	cmd := fmt.Sprintf("sh -c 'curl -s -m 5 --local-port %d %s >/dev/null; sleep 30'", port, srv.URL)
	if out, err := exec.Command("tmux", "-f", "/dev/null", "-L", socket, "new-session", "-d", "-s", "hs-deleg-a", "-x", "80", "-y", "24", cmd).CombinedOutput(); err != nil {
		t.Fatalf("new-session: %v %s", err, out)
	}
	select {
	case got := <-found:
		if got[0] != "tab-a" {
			t.Fatalf("from the pane: %q (%s)", got[0], got[1])
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the pane's request never came")
	}

	// From this test process, over the very addresses the pane's connection had: a socket of its own, no chat's.
	client := &http.Client{Transport: &http.Transport{DialContext: (&net.Dialer{
		LocalAddr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port},
		Control: func(_, _ string, raw syscall.RawConn) error {
			return raw.Control(func(fd uintptr) { _ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1) })
		},
	}).DialContext}}
	var res *http.Response
	var err error
	for try := 0; try < 50; try++ {
		if res, err = client.Get(srv.URL); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if got := <-found; got[0] != "" || got[1] == "" {
		t.Fatalf("from outside on the pane's port: %q (%s)", got[0], got[1])
	}
}

// A conversation kept for a socket holds only while the process it was found in still holds the socket.
func TestDelegationCallerIsRecheckedOnEveryCall(t *testing.T) {
	cache := &callerCache{panes: tmux.New(tmux.Names{Socket: fmt.Sprintf("hs-test-none-%d", rand.IntN(1_000_000_000)), Prefix: "hs-"}),
		convOf: func(string) string { return "" }}
	got := make(chan [2]string, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inode, why := socketOf(r)
		if inode == "" {
			got <- [2]string{"", why}
			return
		}
		var ids [2]string
		for i, pid := range []int{os.Getpid(), 1} {
			cache.mu.Lock()
			cache.seen = map[string]cachedCaller{inode: {conv: "tab-kept", pid: pid, at: time.Now()}}
			cache.mu.Unlock()
			ids[i], _ = cache.of(r)
		}
		got <- ids
	}))
	defer srv.Close()
	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if ids := <-got; ids != [2]string{"tab-kept", ""} {
		t.Fatalf("kept while held, looked up anew when not: %q", ids)
	}
}
