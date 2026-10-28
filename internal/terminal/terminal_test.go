package terminal

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/tmux"
)

type fakeBridge struct {
	mu        sync.Mutex
	claimed   map[string]bool
	enters    []string
	reconcile []string
	output    []string
}

func (b *fakeBridge) ClaimPrompt(id, session, text string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.claimed[id] {
		return false
	}
	b.claimed[id] = true
	return true
}

func (b *fakeBridge) PromptEnter(session string) {
	b.mu.Lock()
	b.enters = append(b.enters, session)
	b.mu.Unlock()
}

func (b *fakeBridge) ReconcileCwd(session string) {
	b.mu.Lock()
	b.reconcile = append(b.reconcile, session)
	b.mu.Unlock()
}

func (b *fakeBridge) TerminalOutput(session string, data []byte) {
	b.mu.Lock()
	b.output = append(b.output, session+":"+string(data))
	b.mu.Unlock()
}

type rig struct {
	t      *testing.T
	socket string
	tm     *tmux.Tmux
	bridge *fakeBridge
	srv    *Server
	http   *httptest.Server
}

func newRig(t *testing.T) *rig {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	socket := fmt.Sprintf("hs-test-%d", rand.IntN(1_000_000))
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", socket, "kill-server").Run()
		// tmux leaves the socket file of a private server behind.
		dir := os.Getenv("TMUX_TMPDIR")
		if dir == "" {
			dir = "/tmp"
		}
		_ = os.Remove(filepath.Join(dir, "tmux-"+strconv.Itoa(os.Getuid()), socket))
	})
	tm := tmux.New(tmux.Names{Socket: socket, Prefix: "hs-"})
	bridge := &fakeBridge{claimed: map[string]bool{}}
	env := []string{"TERM=xterm-256color", "HOME=" + os.Getenv("HOME"), "PATH=" + os.Getenv("PATH")}
	srv := New(Config{Tmux: tm, Bridge: bridge, Home: t.TempDir(), Env: env, MaxConnections: 3})
	h := httptest.NewServer(httpHandler(srv))
	t.Cleanup(h.Close)
	t.Cleanup(srv.Shutdown)
	return &rig{t: t, socket: socket, tm: tm, bridge: bridge, srv: srv, http: h}
}

func (r *rig) session(name string, cmd string) {
	r.t.Helper()
	out, err := exec.Command("tmux", "-f", "/dev/null", "-L", r.socket, "new-session", "-d", "-s", name, "-x", "80", "-y", "24", cmd).CombinedOutput()
	if err != nil {
		r.t.Fatalf("new-session: %v %s", err, out)
	}
}

func (r *rig) dial(query string) *websocket.Conn {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(r.http.URL, "http")+"/api/terminal?"+query, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	c.SetReadLimit(readLimit)
	r.t.Cleanup(func() { c.CloseNow() })
	return c
}

// until reads frames until one of type typ whose payload, accumulated, contains want.
func until(t *testing.T, c *websocket.Conn, typ byte, want string, d time.Duration) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	var seen strings.Builder
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %q in type %d: %v (seen %q)", want, typ, err, seen.String())
		}
		if len(data) > 0 && data[0] == typ {
			seen.Write(data[1:])
			if strings.Contains(seen.String(), want) {
				return seen.String()
			}
		}
	}
}

func frame(typ byte, payload string) []byte { return append([]byte{typ}, payload...) }

func send(t *testing.T, c *websocket.Conn, b []byte) {
	t.Helper()
	if err := c.Write(context.Background(), websocket.MessageBinary, b); err != nil {
		t.Fatal(err)
	}
}

func TestTypesAndShowsOutput(t *testing.T) {
	r := newRig(t)
	r.session("hs-a", "cat")
	c := r.dial("session=hs-a&cols=100&rows=30&clientId=p1")
	send(t, c, frame(msgStdin, "hello-from-page\r"))
	until(t, c, msgStdout, "hello-from-page", 5*time.Second)
	r.bridge.mu.Lock()
	observed := strings.Join(r.bridge.output, "")
	r.bridge.mu.Unlock()
	if !strings.Contains(observed, "hs-a:") || !strings.Contains(observed, "hello-from-page") {
		t.Fatalf("PTY output was not observed: %q", observed)
	}

	send(t, c, []byte{msgHeartbeat})
	until(t, c, msgHeartbeat, "", 3*time.Second)

	if cols, rows, ok := r.srv.Size("hs-a"); !ok || cols != 100 || rows != 30 {
		t.Fatalf("size %d×%d %v", cols, rows, ok)
	}
}

func TestComposerTextIsTypedOncePerMessage(t *testing.T) {
	r := newRig(t)
	r.session("hs-b", "cat")
	c := r.dial("session=hs-b")
	send(t, c, frame(msgPromptMark, `{"id":"m1"}`))
	send(t, c, frame(msgWebStdin, "first-copy\r"))
	until(t, c, msgStdout, "first-copy", 5*time.Second)
	// The same message again (a retry by another path): refused by the claim, never typed.
	send(t, c, frame(msgPromptMark, `{"id":"m1"}`))
	send(t, c, frame(msgWebStdin, "second-copy\r"))
	send(t, c, frame(msgWebStdin, "unmarked\r"))
	out := until(t, c, msgStdout, "unmarked", 5*time.Second)
	if strings.Contains(out, "second-copy") {
		t.Fatalf("a refused message was typed: %q", out)
	}
	r.bridge.mu.Lock()
	enters := len(r.bridge.enters)
	r.bridge.mu.Unlock()
	if enters != 2 {
		t.Fatalf("Enter noted %d times, want 2", enters)
	}
}

func TestResizeReachesTmux(t *testing.T) {
	r := newRig(t)
	r.session("hs-c", "cat")
	c := r.dial("session=hs-c&cols=80&rows=24")
	resize := make([]byte, 5)
	resize[0] = msgResize
	binary.BigEndian.PutUint16(resize[1:], 132)
	binary.BigEndian.PutUint16(resize[3:], 40)
	send(t, c, resize)
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, _ := exec.Command("tmux", "-L", r.socket, "display-message", "-p", "-t", "hs-c", "#{window_width}x#{window_height}").Output()
		if strings.TrimSpace(string(out)) == "132x39" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("window is %s, want 132x39 (a row is the status line)", out)
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Paused for a capture, a page resize is remembered but not applied, and output stops.
	if cols, rows, ok := r.srv.Pause("hs-c"); !ok || cols != 132 || rows != 40 {
		t.Fatalf("pause %d×%d %v", cols, rows, ok)
	}
	if _, _, ok := r.srv.Pause("hs-c"); ok {
		t.Fatal("paused twice")
	}
	r.srv.Resume("hs-c")
}

func TestMissingSessionIsReportedAndClosed(t *testing.T) {
	r := newRig(t)
	r.session("hs-other", "cat") // the server exists, the session does not
	c := r.dial("session=hs-gone")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := c.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusInternalError {
		t.Fatalf("close %v", err)
	}
	r.bridge.mu.Lock()
	defer r.bridge.mu.Unlock()
	if len(r.bridge.reconcile) != 1 || r.bridge.reconcile[0] != "hs-gone" {
		t.Fatalf("reconcile %v", r.bridge.reconcile)
	}
}

func TestSessionExitClosesTheTerminal(t *testing.T) {
	r := newRig(t)
	r.session("hs-d", "cat")
	c := r.dial("session=hs-d")
	send(t, c, []byte{msgHeartbeat})
	until(t, c, msgHeartbeat, "", 3*time.Second)
	send(t, c, []byte{msgKill})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	for {
		_, _, err := c.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				t.Fatalf("close %v", err)
			}
			break
		}
	}
	if r.tm.HasSession("hs-d") {
		t.Fatal("the session survived the kill")
	}
}

func TestNewConnectionOfTheSamePageReplacesTheOld(t *testing.T) {
	r := newRig(t)
	r.session("hs-e", "cat")
	first := r.dial("session=hs-e&clientId=same")
	send(t, first, []byte{msgHeartbeat})
	until(t, first, msgHeartbeat, "", 3*time.Second)
	second := r.dial("session=hs-e&clientId=same")
	send(t, second, []byte{msgHeartbeat})
	until(t, second, msgHeartbeat, "", 3*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		if _, _, err := first.Read(ctx); err != nil {
			if websocket.CloseStatus(err) != websocket.StatusGoingAway {
				t.Fatalf("the replaced connection closed with %v", err)
			}
			break
		}
	}
}

func TestCompleteUTF8(t *testing.T) {
	word := []byte("ж€😀")
	for cut := 0; cut <= len(word); cut++ {
		got := completeUTF8(word[:cut])
		want := 0
		for _, end := range []int{0, 2, 5, 9} {
			if end <= cut {
				want = end
			}
		}
		if got != want {
			t.Errorf("cut %d: %d, want %d", cut, got, want)
		}
	}
	if string(validUTF8([]byte{'a', 0xff, 'b'})) != "a�b" {
		t.Error("an invalid byte is not replaced")
	}
}

func httpHandler(s *Server) http.Handler { return http.HandlerFunc(s.Handle) }
