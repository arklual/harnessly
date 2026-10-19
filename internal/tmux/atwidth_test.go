package tmux_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"harnessly/internal/terminal"
	"harnessly/internal/tmux"
)

// The terminal server is what the capture resizes.
var _ tmux.Terminal = (*terminal.Server)(nil)

type nopBridge struct{}

func (nopBridge) ClaimPrompt(string, string, string) bool { return true }
func (nopBridge) PromptEnter(string)                      {}
func (nopBridge) ReconcileCwd(string)                     {}

// A pane shown in a narrow page terminal is captured at a width where a long line is one line, and the terminal gets
// its own size back.
func TestCaptureAtWidthThroughThePageTerminal(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	socket := fmt.Sprintf("hs-test-%d", rand.IntN(1_000_000_000))
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", socket, "kill-server").Run()
		dir := os.Getenv("TMUX_TMPDIR")
		if dir == "" {
			dir = "/tmp"
		}
		_ = os.Remove(filepath.Join(dir, "tmux-"+strconv.Itoa(os.Getuid()), socket))
	})
	tm := tmux.New(tmux.Names{Socket: socket, Prefix: "hs-"})
	if out, err := exec.Command("tmux", "-f", "/dev/null", "-L", socket, "new-session", "-d", "-s", "hs-w", "-x", "80", "-y", "24", "cat").CombinedOutput(); err != nil {
		t.Fatalf("new-session: %v %s", err, out)
	}
	env := []string{"TERM=xterm-256color", "HOME=" + os.Getenv("HOME"), "PATH=" + os.Getenv("PATH")}
	srv := terminal.New(terminal.Config{Tmux: tm, Bridge: nopBridge{}, Home: t.TempDir(), Env: env})
	h := httptest.NewServer(http.HandlerFunc(srv.Handle))
	t.Cleanup(h.Close)
	t.Cleanup(srv.Shutdown)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(h.URL, "http")+"/api/terminal?session=hs-w&cols=40&rows=20&clientId=p", nil)
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadLimit(32 << 20)
	t.Cleanup(func() { c.CloseNow() })
	go func() { // the page reads everything, so the terminal never stalls
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	}()
	waitUntil(t, "the page terminal", func() bool { _, _, ok := srv.Size("hs-w"); return ok })
	waitUntil(t, "the narrow window", func() bool { w, _ := tm.Display("hs-w", "#{window_width}"); return w == "40" })

	line := strings.Repeat("0123456789", 10)
	if err := tm.SendLiteralThenEnter("hs-w", line); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the typed line", func() bool {
		s, _ := tm.Capture("hs-w")
		return strings.Count(strings.ReplaceAll(s, "\n", ""), line) == 2
	})
	if s, _ := tm.Capture("hs-w"); strings.Contains(s, line) {
		t.Fatalf("the line is not wrapped at 40 columns:\n%s", s)
	}

	start := time.Now()
	got, ok := tm.CaptureAtWidth(srv, "hs-w", 120, 50)
	if !ok || strings.Count(got, line) != 2 {
		t.Fatalf("capture at 120 columns:\n%s", got)
	}
	if took := time.Since(start); took < 600*time.Millisecond {
		t.Fatalf("no settle time: %v", took)
	}
	if cols, rows, ok := srv.Size("hs-w"); !ok || cols != 40 || rows != 20 {
		t.Fatalf("terminal size afterwards %d×%d", cols, rows)
	}
	waitUntil(t, "the window back at 40 columns", func() bool { w, _ := tm.Display("hs-w", "#{window_width}"); return w == "40" })
	// Paused no more: a second narrow capture pauses again rather than falling back to the plain screen.
	if _, _, ok := srv.Pause("hs-w"); !ok {
		t.Fatal("terminal left paused")
	}
	srv.Resume("hs-w")
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
