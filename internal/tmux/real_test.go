package tmux

import (
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// realRig is a pair of private tmux servers of this test only; both are killed when it ends.
type realRig struct {
	t    *testing.T
	main string
	old  string
}

func newRealRig(t *testing.T) *realRig {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	n := rand.IntN(1_000_000_000)
	r := &realRig{t: t, main: fmt.Sprintf("hs-test-%d", n), old: fmt.Sprintf("hs-test-%d-old", n)}
	t.Cleanup(func() {
		for _, s := range []string{r.main, r.old} {
			_ = exec.Command("tmux", "-L", s, "kill-server").Run()
			removeSocketFile(s)
		}
	})
	return r
}

// removeSocketFile removes the socket file tmux leaves behind for a private test server.
func removeSocketFile(socket string) {
	dir := os.Getenv("TMUX_TMPDIR")
	if dir == "" {
		dir = "/tmp"
	}
	_ = os.Remove(filepath.Join(dir, "tmux-"+strconv.Itoa(os.Getuid()), socket))
}

func (r *realRig) names() Names {
	return Names{Socket: r.main, LegacySocket: r.old, Prefix: "hs-", LegacyPrefix: "ho-"}
}

// raw starts a session on one of the rig's servers without the package, running cmd.
func (r *realRig) raw(socket, name, cmd string) {
	r.t.Helper()
	out, err := exec.Command("tmux", "-f", "/dev/null", "-L", socket, "new-session", "-d", "-s", name, "-x", "80", "-y", "24", cmd).CombinedOutput()
	if err != nil {
		r.t.Fatalf("new-session: %v %s", err, out)
	}
}

// waitFor polls cond every 50 ms for up to d.
func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func screenHas(tm *Tmux, name, want string) func() bool {
	return func() bool { s, _ := tm.Capture(name); return strings.Contains(s, want) }
}

func niceOf(t *testing.T, pid string) int {
	t.Helper()
	b, err := os.ReadFile("/proc/" + pid + "/stat")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	f := strings.Fields(s[strings.LastIndexByte(s, ')')+2:])
	n, _ := strconv.Atoi(f[16]) // field 19 of stat
	return n
}

func TestRealCreateSessionStartsTheCleanLoginShell(t *testing.T) {
	r := newRealRig(t)
	dir := t.TempDir()
	report := filepath.Join(dir, "env.txt")
	shell := filepath.Join(dir, "login-shell")
	script := "#!/bin/sh\nprintf '%s\\n%s\\n%s\\n%s\\n%s\\n%s\\n' \"$PATH\" \"${RUNTIME_SECRET-unset}\" \"$TMUX\" \"$TMUX_PANE\" \"$1\" \"$(pwd)\" > " + report + ".tmp\nmv " + report + ".tmp " + report + "\nexec cat\n"
	if err := os.WriteFile(shell, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(dir, "work")
	_ = os.Mkdir(work, 0o755)
	tm := NewConfig(Config{
		Names: r.names(), ConfigFile: "/dev/null", PaneBinDir: "/opt/app/bin/pane", Nice: 3,
		Env: []string{"HOME=" + dir, "SHELL=" + shell, "PATH=/private/bin", "RUNTIME_SECRET=x"},
	})
	name := tm.SessionName("ws-a", "pane-b", "tab-c")
	if err := tm.CreateSession(name, 100, 30, work); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "the shell's report", func() bool { _, err := os.Stat(report); return err == nil })
	b, _ := os.ReadFile(report)
	got := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if got[0] != "/opt/app/bin/pane:"+loginShellBasePath || got[1] != "unset" || !strings.Contains(got[2], r.main) || got[4] != "-l" || got[5] != work {
		t.Fatalf("pane shell saw %q", got)
	}
	if id := tm.PaneID(name); id == "" || got[3] != id {
		t.Fatalf("TMUX_PANE %q, pane id %q", got[3], id)
	}
	if !tm.HasSession(name) || tm.SocketOf(name) != r.main {
		t.Fatal("not on the current server")
	}
	d := tm.PaneDetail(name)
	if d.Width != 100 || d.Height != 30 || d.Cwd != work || d.Command != "cat" || d.PID != tm.PanePID(name) || d.PID == 0 || d.SessionCreated < time.Now().Add(-time.Minute).Unix() {
		t.Fatalf("detail %+v", d)
	}
	if safe, cmd := tm.CheckTerminalProcess(name); safe || cmd != "cat" {
		t.Fatalf("terminal process %v %s", safe, cmd)
	}
	if tm.PanePath(name) != work {
		t.Fatalf("path %s", tm.PanePath(name))
	}
	if runtime.GOOS == "linux" {
		if got, want := niceOf(t, strconv.Itoa(d.PID)), min(19, niceOf(t, "self")+3); got != want {
			t.Fatalf("pane niceness %d, want %d", got, want)
		}
		server, _ := tm.Command(name, "display-message", "-p", "-t", name, "#{pid}")
		if got, want := niceOf(t, strings.TrimSpace(server)), niceOf(t, "self"); got != want {
			t.Fatalf("server niceness %d, want %d", got, want)
		}
	}
	tm.KillSession(name)
	if tm.HasSession(name) {
		t.Fatal("session survived")
	}
}

func TestRealSendVariantsAndCaptures(t *testing.T) {
	r := newRealRig(t)
	home := t.TempDir()
	tm := NewConfig(Config{Names: r.names(), ConfigFile: "/dev/null", Env: []string{"HOME=" + home, "SHELL=/bin/sh"}})
	name := tm.NewSessionName()
	if err := tm.CreateSession(name, 80, 24, ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "a prompt", func() bool { s, _ := tm.Capture(name); return strings.TrimSpace(s) != "" })

	// Typed literally: a word that is a key name stays a word; a trailing ; reaches the shell.
	if err := tm.SendLiteral(name, "Enter"); err != nil {
		t.Fatal(err)
	}
	if err := tm.SendRawKeys(name, "Enter"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "the word Enter run as a command", screenHas(tm, name, "Enter: not found"))
	if err := tm.SendLiteralThenEnter(name, "echo semi-\"$((6*7))\";"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "the literal line", screenHas(tm, name, "semi-42"))
	if s, _ := tm.Capture(name); !strings.Contains(s, `echo semi-"$((6*7))";`) {
		t.Fatalf("trailing ; lost: %q", s)
	}

	// A pane in copy mode is taken out of it before the keys.
	if _, err := tm.Command(name, "copy-mode", "-t", name); err != nil {
		t.Fatal(err)
	}
	if m, _ := tm.Display(name, "#{pane_in_mode}"); m != "1" {
		t.Fatalf("not in copy mode: %q", m)
	}
	if err := tm.SendKeysSeparated(name, "echo out-of-copy-mode"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "keys after copy mode", screenHas(tm, name, "\nout-of-copy-mode"))
	if m, _ := tm.Display(name, "#{pane_in_mode}"); m != "0" {
		t.Fatalf("still in copy mode: %q", m)
	}

	// Plain, styled and with scrollback.
	if err := tm.SendKeys(name, `printf '\033[1mBOLD\033[0m\n'`); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "bold text", screenHas(tm, name, "\nBOLD"))
	if s, _ := tm.CaptureStyled(name); !strings.Contains(s, "\x1b[1mBOLD") {
		t.Fatalf("styled capture %q", s)
	}
	if s, _ := tm.Capture(name); strings.Contains(s, "\x1b") {
		t.Fatalf("plain capture has SGR: %q", s)
	}
	if err := tm.SendKeys(name, "seq 1 200"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "the numbers", screenHas(tm, name, "\n200\n"))
	visible, _ := tm.Capture(name)
	history, ok := tm.CaptureHistory(name, 50)
	if !ok || strings.Contains(visible, "\n160\n") || !strings.Contains(history, "\n160\n") || strings.Contains(history, "\n100\n") {
		t.Fatalf("history capture:\n%s", history)
	}
	if _, ok := tm.Capture("hs-missing"); ok {
		t.Fatal("capture of a missing session")
	}
}

func TestRealBracketedPasteIntoAProgram(t *testing.T) {
	r := newRealRig(t)
	tm := New(r.names())
	r.raw(r.main, "hs-cat", "cat")
	if err := tm.SendBracketedPasteOnly("hs-cat", "pasted\x1bX\x03"); err != nil {
		t.Fatal(err)
	}
	if err := tm.SendRawKeys("hs-cat", "Enter"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "the paste", screenHas(tm, "hs-cat", "pasted␛X"))
	if safe, cmd := tm.CheckTerminalProcess("hs-cat"); safe || cmd != "cat" {
		t.Fatalf("%v %s", safe, cmd)
	}
	if err := tm.SendKeySequence("hs-cat", "C-d"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "cat to end", func() bool { return !tm.HasSession("hs-cat") })
}

// A message of 26.10 (122 lines, 21 KB of links, tokens and quotes) never reached the agent: one send-keys of it was
// over tmux's 16 KB command limit, and tmux answered «command too long» on every try. It reaches a program whole now.
func TestRealLongBracketedPasteArrivesWhole(t *testing.T) {
	r := newRealRig(t)
	tm := New(r.names())
	out := filepath.Join(t.TempDir(), "got")
	r.raw(r.main, "hs-raw", "stty raw -echo; exec dd bs=1 of="+out+" 2>/dev/null")
	var b strings.Builder
	for i := 0; b.Len() < 21_000; i++ {
		fmt.Fprintf(&b, "%d. Ссылка https://example.test/x?next=%%2Fa%%2Fb%%2Fc&jwt=eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2ln \"q\" 'x' -- `y`;\n", i)
	}
	text := strings.TrimSuffix(b.String(), "\n") + ";"
	want := pasteStart + text + pasteEnd
	waitFor(t, 5*time.Second, "the reader", func() bool { _, err := os.Stat(out); return err == nil })
	if err := tm.SendBracketedPasteOnly("hs-raw", text); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "the whole paste", func() bool { got, _ := os.ReadFile(out); return len(got) >= len(want) })
	got, _ := os.ReadFile(out)
	if string(got) != want {
		t.Fatalf("got %d bytes, want %d; differs from byte %d", len(got), len(want), firstDiff(string(got), want))
	}
}

func firstDiff(a, b string) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

func TestRealTwoServers(t *testing.T) {
	r := newRealRig(t)
	tm := New(r.names())
	if p, ok := tm.ReadAllPanes(); !ok || len(p) != 0 {
		t.Fatalf("no servers yet: %v %v", p, ok)
	}
	if got := tm.ListSessions(); len(got) != 0 {
		t.Fatalf("%q", got)
	}
	r.raw(r.main, "hs-a", "cat")
	r.raw(r.main, "ho-restarted", "cat")
	r.raw(r.old, "ho-b", "cat")
	r.raw(r.old, "hs-stray", "cat")
	r.raw(r.old, "scratch", "cat")

	got := tm.ListSessions()
	if !slices.Equal(got, []string{"ho-restarted", "hs-a", "ho-b"}) {
		t.Fatalf("sessions %q", got)
	}
	p, ok := tm.ReadAllPanes()
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !ok || !slices.Equal(keys, []string{"ho-b", "ho-restarted", "hs-a"}) || p["hs-a"].Command != "cat" || p["hs-a"].PID == 0 || p["hs-a"].WindowActivity == 0 {
		t.Fatalf("panes %v", p)
	}
	if tm.SocketOf("ho-b") != r.old || tm.SocketOf("ho-restarted") != r.main || tm.SocketOf("hs-a") != r.main {
		t.Fatal("wrong servers")
	}
	if tm.HasSession("hs-stray") {
		t.Fatal("a current-prefix session on the legacy server")
	}
	id := tm.PaneID("ho-b")
	if s := tm.SessionsOfPane(id, r.old); !slices.Equal(s, []string{"ho-b"}) {
		t.Fatalf("pane %s: %q", id, s)
	}
	if _, err := tm.Command("ho-b", "rename-window", "-t", "ho-b", "renamed"); err != nil {
		t.Fatal(err)
	}
	if w, _ := tm.Display("ho-b", "#{window_name}"); w != "renamed" {
		t.Fatalf("window %q", w)
	}
	tm.KillSession("ho-b")
	if tm.HasSession("ho-b") {
		t.Fatal("legacy session survived")
	}

	conf := filepath.Join(t.TempDir(), "tmux.conf")
	_ = os.WriteFile(conf, []byte("set -g history-limit 1234\n"), 0o600)
	NewConfig(Config{Names: r.names(), ConfigFile: conf}).ApplyConfig("")
	if v, _ := tm.Command("hs-a", "show", "-gv", "history-limit"); strings.TrimSpace(v) != "1234" {
		t.Fatalf("config not applied on the current server: %q", v)
	}
	if v, _ := exec.Command("tmux", "-L", r.old, "show", "-gv", "history-limit").Output(); strings.TrimSpace(string(v)) != "1234" {
		t.Fatalf("config not applied on the legacy server: %q", v)
	}
}
