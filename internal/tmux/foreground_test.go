package tmux

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// fakeProc points procRoot at a folder with the given processes: pid → [stat line, cmdline].
func fakeProc(t *testing.T, procs map[int][2]string) {
	t.Helper()
	root := t.TempDir()
	for pid, p := range procs {
		dir := filepath.Join(root, strconv.Itoa(pid))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(p[0]), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(p[1]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := procRoot
	procRoot = root
	t.Cleanup(func() { procRoot = old })
}

// HRN-777: tmux answered `nice` (its fallback, the pane's start command) for a pane with nothing but bash in it.
func TestForegroundTrustsProcOverTmuxFallback(t *testing.T) {
	fakeProc(t, map[int][2]string{
		// The pane's shell: session leader on a tty, its own group in the foreground.
		500: {"500 (bash) S 1 500 500 34817 500 4194560 0 0", "/bin/bash\x00-l\x00"},
		// An agent in the foreground of another pane.
		600: {"600 (zsh) S 1 600 600 34818 611 4194560 0 0", "-zsh\x00"},
		611: {"611 (claude) S 600 611 600 34818 611 4194560 0 0", "claude\x00--resume\x00x\x00"},
		// A pid that is not a pane's shell in this namespace (not a session leader).
		700: {"700 (weird) name) S 1 1 1 34819 700 0", "/usr/bin/cat\x00"},
	})
	for _, c := range []struct {
		tmux string
		pid  int
		want string
	}{
		{"nice", 500, "bash"},
		{"env", 500, "bash"},
		{"nice", 600, "claude"},
		{"nice", 700, "nice"},     // not the pane: tmux's word stands
		{"nice", 999, "nice"},     // gone or invisible: tmux's word stands
		{"zsh", 0, "zsh"},         // no pid known
		{"claude", 611, "claude"}, // not a session leader: tmux's word
	} {
		if got := ForegroundCommand(c.tmux, c.pid); got != c.want {
			t.Errorf("ForegroundCommand(%q, %d) = %q, want %q", c.tmux, c.pid, got, c.want)
		}
	}
}

func TestWindowNameIsTmuxs(t *testing.T) {
	for in, want := range map[string]string{
		"/bin/bash": "bash", "-zsh": "zsh", "exec /usr/bin/fish": "fish", `"/opt/x y/claude"`: "x", "node": "node", "": "",
	} {
		if got := windowName(in); got != want {
			t.Errorf("windowName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPaneCurrentCommandReadsProcBehindTmux(t *testing.T) {
	fakeProc(t, map[int][2]string{500: {"500 (bash) S 1 500 500 34817 500 0", "/bin/bash\x00-l\x00"}})
	tm := New(fakeNames)
	tm.run = func(string, string, ...string) result { return result{stdout: "nice\t500\n"} }
	if got := tm.PaneCurrentCommand("nw-a"); got != "bash" {
		t.Fatalf("PaneCurrentCommand = %q", got)
	}
	if safe, cmd := tm.CheckTerminalProcess("nw-a"); !safe || cmd != "bash" {
		t.Fatalf("CheckTerminalProcess = %v %q", safe, cmd)
	}
	tm.run = func(string, string, ...string) result { return result{stdout: "nw-a\tnice\t/w\t500\t0\n"} }
	if p, ok := tm.ReadAllPanes(); !ok || p["nw-a"].Command != "bash" {
		t.Fatalf("ReadAllPanes = %+v %v", p, ok)
	}
	tm.run = func(string, string, ...string) result { return result{stdout: "/w\tnice\t500\t80\t24\t1\n"} }
	if d := tm.PaneDetail("nw-a"); d.Command != "bash" || d.PID != 500 {
		t.Fatalf("PaneDetail = %+v", d)
	}
}

// GitHub issue 37: under gVisor /proc/<pid>/stat gives tty_nr 0 and tpgid 0. The session's processes still tell an idle
// shell from a running agent, and an unclear session leaves tmux's word, which reads as busy.
func TestForegroundWithoutTheTerminalsGroup(t *testing.T) {
	fakeProc(t, map[int][2]string{
		// An idle shell: nothing else in its session.
		500: {"500 (bash) S 1 500 500 0 0 4194560 0 0", "/bin/bash\x00-l\x00"},
		// An agent in the foreground; a subshell of the prompt stays in the shell's own group.
		600: {"600 (bash) S 1 600 600 0 0 4194560 0 0", "bash\x00"},
		611: {"611 (claude) S 600 611 600 0 0 4194560 0 0", "claude\x00--resume\x00x\x00"},
		612: {"612 (node) S 611 611 600 0 0 4194560 0 0", "node\x00"},
		613: {"613 (bash) S 600 600 600 0 0 4194560 0 0", "bash\x00"},
		// A stopped job does not hold the foreground.
		700: {"700 (bash) S 1 700 700 0 0 4194560 0 0", "bash\x00"},
		711: {"711 (vim) T 700 711 700 0 0 4194560 0 0", "vim\x00"},
		// Two running groups: which is in the foreground is not known.
		800: {"800 (bash) S 1 800 800 0 0 4194560 0 0", "bash\x00"},
		811: {"811 (claude) S 800 811 800 0 0 4194560 0 0", "claude\x00"},
		822: {"822 (sleep) S 800 822 800 0 0 4194560 0 0", "sleep\x0099\x00"},
		// Not a session leader.
		900: {"900 (claude) S 1 900 1 0 0 4194560 0 0", "claude\x00"},
	})
	for _, c := range []struct {
		pid  int
		want string
	}{{500, "bash"}, {600, "claude"}, {700, "bash"}, {800, "nice"}, {900, "nice"}} {
		if got := ForegroundCommand("nice", c.pid); got != c.want {
			t.Errorf("ForegroundCommand(nice, %d) = %q, want %q", c.pid, got, c.want)
		}
	}
}
