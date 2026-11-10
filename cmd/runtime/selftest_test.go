package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/tmux"
)

// fakePanes is one pane whose foreground is driven by what is typed into it.
type fakePanes struct {
	mu       sync.Mutex
	created  bool
	killed   bool
	fg       string // what the runtime sees in the foreground
	raw      string // what tmux on its own says; "" means the same
	waitRead bool   // `read` is waiting for a line
	typed    string
	screen   string
	// faults
	createErr  error
	stuckShell string // the foreground never leaves this
	keysLost   bool
	// rcLooks is how many looks at the pane its shell's rc files take: the shell is in the foreground at once, and what
	// is typed before they end waits for them (HRN-901). queued holds those lines.
	rcLooks int
	queued  []string
}

func (f *fakePanes) Names() tmux.Names { return tmux.Names{Prefix: "hs-"} }
func (f *fakePanes) CreateSession(string, int, int, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	f.created = true
	if f.fg == "" {
		f.fg = "zsh"
	}
	return nil
}
func (f *fakePanes) KillSession(string) { f.mu.Lock(); f.killed = true; f.mu.Unlock() }
func (f *fakePanes) Display(string, string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.raw != "" {
		return f.raw, true
	}
	return f.fg, true
}
func (f *fakePanes) PaneCurrentCommand(string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.look()
	return f.fg
}

// look is one look at the pane: it is also the time the rc files take.
func (f *fakePanes) look() {
	if f.rcLooks == 0 {
		return
	}
	if f.rcLooks--; f.rcLooks == 0 {
		for _, l := range f.queued {
			f.typeLine(l)
		}
		f.queued = nil
	}
}

func (f *fakePanes) SendKeys(_ string, line string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rcLooks > 0 {
		f.queued = append(f.queued, line)
		return nil
	}
	f.typeLine(line)
	return nil
}

func (f *fakePanes) typeLine(line string) {
	switch {
	case f.stuckShell != "":
	case line == "echo "+selftestReadyTyped:
		f.screen += "$ " + line + "\n" + selftestReady + "\n"
	case line == "sleep 30":
		f.fg = "sleep"
	case strings.HasPrefix(line, "read hs_answer"):
		f.waitRead = true
	}
}
func (f *fakePanes) SendRawKeys(_ string, keys string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.keysLost {
		return nil
	}
	switch {
	case keys == "C-c":
		f.fg = "zsh"
	case keys == "Enter" && f.waitRead:
		f.screen += "hs-selftest:" + f.typed + "\n"
		f.waitRead = false
	case f.waitRead:
		f.typed += keys
	}
	return nil
}
func (f *fakePanes) Capture(string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.look()
	return f.screen, true
}

// fakeClock: sleeping moves the clock, so waits end at once.
func fakeSelftest(p selftestPanes) selftest {
	now := time.Unix(0, 0)
	var mu sync.Mutex
	return selftest{
		panes: p,
		sleep: func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() },
		now:   func() time.Time { mu.Lock(); defer mu.Unlock(); return now },
	}
}

func selftestByID(checks []machineCheck) map[string]machineCheck {
	m := map[string]machineCheck{}
	for _, c := range checks {
		m[c.ID] = c
	}
	return m
}

func TestSelftestPassesAndCleansUp(t *testing.T) {
	p := &fakePanes{}
	checks := fakeSelftest(p).run(context.Background())
	if bad := selftestFailed(checks); bad != "" || len(checks) != 3 {
		t.Fatalf("failed: %s (%d checks)", bad, len(checks))
	}
	if !p.killed {
		t.Fatal("the throwaway pane was left behind")
	}
}

// HRN-777's sign: tmux says `nice`, /proc says the shell — the test passes and says so.
func TestSelftestNamesWhatTmuxAloneSays(t *testing.T) {
	p := &fakePanes{raw: "nice"}
	c := selftestByID(fakeSelftest(p).run(context.Background()))["pane_shell"]
	if !c.OK || !strings.Contains(c.Detail, "«nice»") {
		t.Fatalf("%+v", c)
	}
}

func TestSelftestFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		panes  *fakePanes
		failed string // the first check that fails; the ones after it are not tried
	}{
		"no pane":           {&fakePanes{createErr: errors.New("server exited")}, "pane_shell"},
		"wrapper seen":      {&fakePanes{fg: "nice"}, "pane_shell"},
		"program unseen":    {&fakePanes{stuckShell: "zsh"}, "pane_shell"}, // it answers nothing typed
		"shell never reads": {&fakePanes{rcLooks: 1 << 30}, "pane_shell"},
		"keys lost":         {&fakePanes{keysLost: true}, "pane_program"}, // Ctrl-C is lost first
	} {
		t.Run(name, func(t *testing.T) {
			got := selftestByID(fakeSelftest(tc.panes).run(context.Background()))
			if got[tc.failed].OK || got[tc.failed].Detail == "" {
				t.Fatalf("%s should fail with a reason: %+v", tc.failed, got[tc.failed])
			}
			if tc.panes.created && !tc.panes.killed {
				t.Fatal("the throwaway pane was left behind")
			}
		})
	}
}

// HRN-901: on 1.187.10 the shell's rc files ran ~10 s under the load of a start; the line typed waited for them, and
// the program's step (8 s) failed. The shell's step waits until the shell answers, and the program's step is the
// program's alone.
func TestSelftestWaitsForASlowShell(t *testing.T) {
	looks := int(12*time.Second/selftestPoll) + 5 // longer than a step's wait, shorter than the shell's
	p := &fakePanes{fg: "bash", rcLooks: looks}
	checks := fakeSelftest(p).run(context.Background())
	if bad := selftestFailed(checks); bad != "" {
		t.Fatalf("a shell slow to start failed the self-test: %s", bad)
	}
}

// The typed line's own echo is not the shell's answer: a shell that never reads fails, though the line is on screen.
func TestSelftestTypedEchoIsNotTheAnswer(t *testing.T) {
	if strings.Contains("echo "+selftestReadyTyped, selftestReady) {
		t.Fatal("the typed line holds the answer it waits for")
	}
}

// The real thing: a private tmux server, a pane started as a tab's pane, real keys.
func TestSelftestOnRealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	if testing.Short() {
		t.Skip("short")
	}
	socket := fmt.Sprintf("hs-test-st-%d", rand.IntN(1_000_000_000))
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", socket, "kill-server").Run()
		dir := os.Getenv("TMUX_TMPDIR")
		if dir == "" {
			dir = "/tmp"
		}
		_ = os.Remove(filepath.Join(dir, "tmux-"+strconv.Itoa(os.Getuid()), socket))
	})
	home := t.TempDir()
	tm := tmux.NewConfig(tmux.Config{
		Names: tmux.Names{Socket: socket, Prefix: "hs-"},
		Nice:  10,
		Env:   []string{"HOME=" + home, "SHELL=/bin/sh", "PATH=/usr/bin:/bin", "LANG=C.UTF-8"},
		Home:  home,
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	checks := newSelftest(tm).run(ctx)
	if bad := selftestFailed(checks); bad != "" {
		t.Fatalf("self-test on a real tmux failed: %s", bad)
	}
	if out, _ := exec.Command("tmux", "-L", socket, "ls").CombinedOutput(); strings.Contains(string(out), "selftest") {
		t.Fatalf("the throwaway pane is still there: %s", out)
	}
}

// TestAgentScenarioLive runs the agent's scenario on a real Claude (a haiku turn): HARNESSLY_LIVE_CLAUDE=<binary>
// [RUNTIME_SIDE_CHAT_CONFIG=…] go test ./cmd/runtime -run TestAgentScenarioLive -v. It is how a new Claude is tried by
// hand; the runtime does the same on the machine (selftest_keeper.go).
func TestAgentScenarioLive(t *testing.T) {
	binary := os.Getenv("HARNESSLY_LIVE_CLAUDE")
	if binary == "" {
		t.Skip("HARNESSLY_LIVE_CLAUDE is not set")
	}
	socket := fmt.Sprintf("hs-test-st-%d", rand.IntN(1_000_000_000))
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "kill-server").Run() })
	tm := tmux.NewConfig(tmux.Config{Names: tmux.Names{Socket: socket, Prefix: "hs-"}, Nice: 10})
	a, err := newAgentSelftest(tm, t.TempDir(), binary)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	start := time.Now()
	checks := a.run(ctx)
	for _, c := range checks {
		t.Logf("%v %s %s", c.OK, c.ID, c.Detail)
	}
	if bad := selftestFailed(checks); bad != "" {
		t.Fatalf("failed after %s: %s", time.Since(start).Round(time.Second), bad)
	}
}
