package typist

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/screen"
)

func tui(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "tui", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

const idleLine = "\x1b[39m❯ "

// withInput is the idle Claude screen with text in its box; lines after the first are the box's wrapped lines.
func withInput(t *testing.T, text string) string {
	t.Helper()
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	if !strings.Contains(idle, idleLine+"\n") {
		t.Fatal("fixture has no idle input line")
	}
	return strings.Replace(idle, idleLine+"\n", idleLine+text+"\n", 1)
}

// permissionScreen is a Claude permission prompt (synthetic, the shape of 2.1.x): no input line, a numbered menu.
const permissionScreen = `● I'll clean the build folder.

● Bash(rm -rf build)
  ⎿  Running…

────────────────────────────────────────────────────────────────────────────────
 Bash command

   rm -rf build
   Remove the build output

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for rm commands in /home/coder/app
   3. No, and tell Claude what to do differently (esc)
`

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
	// onSleep runs after every sleep with the time slept so far.
	onSleep func(total time.Duration)
	slept   time.Duration
}

func newClock() *fakeClock { return &fakeClock{now: time.Unix(1_800_000_000, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.slept += d
	f, total := c.onSleep, c.slept
	c.mu.Unlock()
	if f != nil {
		f(total)
	}
}

// fakePane is a pane whose screen the test sets; every key and paste is recorded, and onSend may change the screen.
type fakePane struct {
	mu       sync.Mutex
	screen   string
	readable bool
	calls    []string
	onSend   func(call string)
	fail     map[string]error // call prefix → error
}

func newPane(screen string) *fakePane {
	return &fakePane{screen: screen, readable: true, fail: map[string]error{}}
}

func (p *fakePane) set(s string) {
	p.mu.Lock()
	p.screen = s
	p.mu.Unlock()
}

func (p *fakePane) CaptureStyled() (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.screen, p.readable
}

func (p *fakePane) Capture() (string, bool) {
	s, ok := p.CaptureStyled()
	return screen.StripANSI(s), ok
}

func (p *fakePane) send(call string) error {
	p.mu.Lock()
	for prefix, err := range p.fail {
		if strings.HasPrefix(call, prefix) {
			p.mu.Unlock()
			return err
		}
	}
	p.calls = append(p.calls, call)
	f := p.onSend
	p.mu.Unlock()
	if f != nil {
		f(call)
	}
	return nil
}

func (p *fakePane) Paste(text string) error    { return p.send("paste:" + text) }
func (p *fakePane) TypeLine(text string) error { return p.send("line:" + text) }
func (p *fakePane) Keys(keys ...string) error  { return p.send("keys:" + strings.Join(keys, " ")) }

func (p *fakePane) sent() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.calls...)
}

var errTmux = errors.New("tmux: send-keys failed")

// planScreen is Claude's plan approval (synthetic, the shape of 2.1.x).
const planScreen = `● Here is the plan: clean the build folder, then rebuild.

────────────────────────────────────────────────────────────────────────────────
 Ready to code?

 Would you like to proceed?
 ❯ 1. Yes, and auto-accept edits
   2. Yes, and manually approve edits
   3. No, keep planning

   ctrl+g to edit in your editor
`
