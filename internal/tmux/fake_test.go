package tmux

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeTmux has servers by socket, each a list of [session, pane id]; a socket absent from servers has nothing listening.
type fakeTmux struct {
	mu      sync.Mutex
	servers map[string][][2]string
	silent  map[string]bool // sockets whose server does not answer in time
	broken  map[string]bool // sockets whose tmux fails with some other error
	calls   []call
	sleeps  []time.Duration
}

type call struct {
	dir, program string
	args         []string
}

func newFake() *fakeTmux {
	return &fakeTmux{servers: map[string][][2]string{}, silent: map[string]bool{}, broken: map[string]bool{}}
}

var fakeNames = Names{Socket: "main", LegacySocket: "old", Prefix: "nw-", LegacyPrefix: "od-"}

func (f *fakeTmux) tmux(cfg Config) *Tmux {
	t := NewConfig(cfg)
	t.run = f.run
	t.sleep = func(d time.Duration) { f.mu.Lock(); f.sleeps = append(f.sleeps, d); f.mu.Unlock() }
	return t
}

func (f *fakeTmux) run(dir, program string, args ...string) result {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{dir, program, append([]string(nil), args...)})
	i := slices.Index(args, "-L")
	socket := args[i+1]
	if f.silent[socket] {
		return result{err: errors.New("signal: killed"), timedOut: true}
	}
	if f.broken[socket] {
		return result{err: errors.New("exit status 1"), stderr: "protocol version mismatch\n"}
	}
	var command string
	for _, a := range args[i+2:] {
		if slices.Contains([]string{"ls", "list-panes", "display-message", "has-session", "send-keys", "copy-mode", "new-session", "source-file", "kill-server", "kill-session", "capture-pane"}, a) {
			command = a
			break
		}
	}
	if command == "new-session" {
		name := args[slices.Index(args, "-s")+1]
		f.servers[socket] = append(f.servers[socket], [2]string{name, "%99"})
		return result{}
	}
	panes, up := f.servers[socket]
	if !up {
		return result{err: errors.New("exit status 1"), stderr: "no server running on /tmp/tmux-1000/" + socket + "\n"}
	}
	var pane *[2]string
	if j := slices.Index(args, "-t"); j >= 0 {
		target := strings.TrimPrefix(args[j+1], "=")
		for k := range panes {
			if panes[k][0] == target || panes[k][1] == target {
				pane = &panes[k]
			}
		}
		if pane == nil {
			return result{err: errors.New("exit status 1"), stderr: "can't find " + target + "\n"}
		}
	}
	var out strings.Builder
	switch command {
	case "ls":
		for _, p := range panes {
			out.WriteString(p[0] + "\n")
		}
	case "list-panes":
		for _, p := range panes {
			out.WriteString(p[0] + "\tclaude\t/home\t" + p[1][1:] + "\t0\n")
		}
	case "display-message":
		out.WriteString(pane[0] + "\n")
	case "kill-server":
		delete(f.servers, socket)
	}
	return result{stdout: out.String()}
}

func (f *fakeTmux) last() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1].args
}

func (f *fakeTmux) reset() {
	f.mu.Lock()
	f.calls, f.sleeps = nil, nil
	f.mu.Unlock()
}

// Ported from the previous runtime's tests of the two servers during a server rename.

func TestCommandsGoToTheServerTheSessionLivesOn(t *testing.T) {
	f := newFake()
	tm := f.tmux(Config{Names: fakeNames})
	f.servers["old"] = [][2]string{{"od-ws-a-pane-b-tab-old", "%7"}}
	f.servers["main"] = [][2]string{{"nw-ws-a-pane-b-tab-new", "%7"}, {"od-ws-a-pane-b-tab-restarted", "%8"}}
	for name, want := range map[string]bool{
		"od-ws-a-pane-b-tab-old": true, "nw-ws-a-pane-b-tab-new": true, "od-ws-a-pane-b-tab-restarted": true, "od-ws-a-pane-b-tab-gone": false,
	} {
		if got := tm.HasSession(name); got != want {
			t.Errorf("HasSession(%s) = %v", name, got)
		}
	}
	for name, want := range map[string]string{
		"od-ws-a-pane-b-tab-old": "old", "nw-ws-a-pane-b-tab-new": "main", "od-ws-a-pane-b-tab-restarted": "main",
	} {
		if got := tm.SocketOf(name); got != want {
			t.Errorf("SocketOf(%s) = %s", name, got)
		}
	}
	_ = tm.SendRawKeys("od-ws-a-pane-b-tab-old", "Enter")
	if got := f.last(); !reflect.DeepEqual(got, []string{"-u", "-L", "old", "send-keys", "-t", "od-ws-a-pane-b-tab-old", "Enter"}) {
		t.Errorf("send went %q", got)
	}
	_ = tm.SendRawKeys("od-ws-a-pane-b-tab-restarted", "Enter")
	if got := f.last(); !reflect.DeepEqual(got, []string{"-u", "-L", "main", "send-keys", "-t", "od-ws-a-pane-b-tab-restarted", "Enter"}) {
		t.Errorf("send went %q", got)
	}
	// A legacy name found nowhere would be started on the current server.
	if got := tm.SocketOf("od-ws-a-pane-b-tab-gone"); got != "main" {
		t.Errorf("SocketOf(gone) = %s", got)
	}
}

func TestFollowsALegacySessionStartedAgainOnTheCurrentServer(t *testing.T) {
	f := newFake()
	tm := f.tmux(Config{Names: fakeNames})
	f.servers["old"] = [][2]string{{"od-ws-a-pane-b-tab-c", "%7"}}
	f.servers["main"] = nil
	if s := tm.SocketOf("od-ws-a-pane-b-tab-c"); s != "old" {
		t.Fatalf("first on %s", s)
	}
	f.servers["old"] = nil
	f.servers["main"] = [][2]string{{"od-ws-a-pane-b-tab-c", "%1"}}
	if !tm.HasSession("od-ws-a-pane-b-tab-c") {
		t.Fatal("not found again")
	}
	if s := tm.SocketOf("od-ws-a-pane-b-tab-c"); s != "main" {
		t.Fatalf("then on %s", s)
	}
}

func TestListsBothServersAndNoServerIsNoPanes(t *testing.T) {
	f := newFake()
	tm := f.tmux(Config{Names: fakeNames})
	f.servers["old"] = [][2]string{{"od-ws-a-pane-b-tab-old", "%7"}, {"scratch", "%8"}, {"nw-ws-a-pane-b-tab-stray", "%9"}}
	if got := tm.ListSessions(); !reflect.DeepEqual(got, []string{"od-ws-a-pane-b-tab-old"}) {
		t.Fatalf("sessions %q", got)
	}
	panes, ok := tm.ReadAllPanes()
	if !ok || len(panes) != 1 || panes["od-ws-a-pane-b-tab-old"].PID != 7 {
		t.Fatalf("panes %v %v", panes, ok)
	}
	f.servers["main"] = [][2]string{{"nw-ws-a-pane-b-tab-new", "%1"}}
	if got := tm.ListSessions(); !reflect.DeepEqual(got, []string{"nw-ws-a-pane-b-tab-new", "od-ws-a-pane-b-tab-old"}) {
		t.Fatalf("sessions %q", got)
	}
	panes, ok = tm.ReadAllPanes()
	if !ok || len(panes) != 2 {
		t.Fatalf("panes %v %v", panes, ok)
	}
}

func TestFindsAPaneIDOnEachServerOrOnlyTheNamedOne(t *testing.T) {
	f := newFake()
	tm := f.tmux(Config{Names: fakeNames})
	f.servers["old"] = [][2]string{{"od-ws-a-pane-b-tab-old", "%7"}}
	f.servers["main"] = [][2]string{{"nw-ws-a-pane-b-tab-new", "%7"}}
	got := tm.SessionsOfPane("%7", "")
	slices.Sort(got)
	if !reflect.DeepEqual(got, []string{"nw-ws-a-pane-b-tab-new", "od-ws-a-pane-b-tab-old"}) {
		t.Fatalf("both: %q", got)
	}
	if got := tm.SessionsOfPane("%7", "main"); !reflect.DeepEqual(got, []string{"nw-ws-a-pane-b-tab-new"}) {
		t.Fatalf("main: %q", got)
	}
	if got := tm.SessionsOfPane("%7", "elsewhere"); len(got) != 0 {
		t.Fatalf("elsewhere: %q", got)
	}
	if got := tm.SessionsOfPane("7", ""); len(got) != 0 {
		t.Fatalf("not a pane id: %q", got)
	}
}

func TestStartsSessionsOnTheCurrentServerUnderItsPrefix(t *testing.T) {
	f := newFake()
	tm := f.tmux(Config{Names: fakeNames})
	name := tm.SessionName("ws-a", "pane-b", "tab-c")
	if name != "nw-ws-a-pane-b-tab-c" {
		t.Fatalf("name %s", name)
	}
	if n := tm.NewSessionName(); !tm.IsAppSession(n) || len(n) != len("nw-")+20 {
		t.Fatalf("ad hoc name %s", n)
	}
	f.servers["old"], f.servers["main"] = nil, nil
	if err := tm.CreateSession(name, 80, 24, ""); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(f.calls[0].args, "main") || tm.SocketOf(name) != "main" {
		t.Fatalf("created with %q", f.calls[0].args)
	}
	// A legacy name being started again goes to the current server too, and is found there.
	if err := tm.CreateSession("od-ws-a-pane-b-tab-c", 80, 24, ""); err != nil {
		t.Fatal(err)
	}
	if tm.SocketOf("od-ws-a-pane-b-tab-c") != "main" {
		t.Fatal("legacy name not on the current server")
	}
}

// Ported from the previous runtime's pane snapshot tests.

func TestPaneSnapshotTellsASilentTmuxFromNoPanes(t *testing.T) {
	f := newFake()
	tm := f.tmux(Config{Names: fakeNames})
	f.silent["main"] = true
	if p, ok := tm.ReadAllPanes(); ok || p != nil {
		t.Fatalf("silent tmux read as %v", p)
	}
	if len(tm.AllPanes()) != 0 {
		t.Fatal("AllPanes of a silent tmux")
	}
	f.silent["main"] = false
	f.broken["old"] = true
	if _, ok := tm.ReadAllPanes(); ok {
		t.Fatal("a failing server read as no panes")
	}
	f.broken["old"] = false
	if p, ok := tm.ReadAllPanes(); !ok || len(p) != 0 {
		t.Fatalf("no servers: %v %v", p, ok)
	}
}

func TestPaneSnapshotReadsEveryPaneBySession(t *testing.T) {
	tm := New(Names{Socket: "main", LegacySocket: "", Prefix: "nw-", LegacyPrefix: "od-"})
	tm.run = func(string, string, ...string) result {
		return result{stdout: "od-a\tclaude-5cd9\t/root/work\t2031087\t1791986560\nod-b\tbash\t/tmp\t42\t0\nod-c\t\t/tmp\t1\t0\n"}
	}
	p, ok := tm.ReadAllPanes()
	if !ok || len(p) != 2 {
		t.Fatalf("%v %v", p, ok)
	}
	if got := p["od-a"]; got != (PaneInfo{Command: "claude-5cd9", Path: "/root/work", PID: 2031087, WindowActivity: 1791986560}) {
		t.Fatalf("od-a %+v", got)
	}
	if p["od-b"].Command != "bash" {
		t.Fatalf("od-b %+v", p["od-b"])
	}
}

// Ported from the previous runtime's send sanitising tests.

func typed(f *fakeTmux) []string {
	var out []string
	for _, c := range f.calls {
		if slices.Contains(c.args, "-l") {
			out = append(out, c.args[len(c.args)-1])
		}
	}
	return out
}

func sendRig(t *testing.T) (*fakeTmux, *Tmux) {
	f := newFake()
	f.servers["main"] = [][2]string{{"nw-s", "%1"}}
	return f, f.tmux(Config{Names: Names{Socket: "main", Prefix: "nw-"}})
}

func TestBracketedPasteFramesText(t *testing.T) {
	f, tm := sendRig(t)
	text := "Привет\n\t- пункт `код`\n"
	if err := tm.SendBracketedPaste("nw-s", text); err != nil {
		t.Fatal(err)
	}
	if got := typed(f); !reflect.DeepEqual(got, []string{"\x1b[200~" + text + "\x1b[201~"}) {
		t.Fatalf("%q", got)
	}
	var seq [][]string
	for _, c := range f.calls {
		seq = append(seq, c.args[3:])
	}
	want := [][]string{
		{"copy-mode", "-q", "-t", "nw-s"},
		{"send-keys", "-t", "nw-s", "-l", "\x1b[200~" + text + "\x1b[201~"},
		{"send-keys", "-t", "nw-s", "Enter"},
		{"send-keys", "-t", "nw-s", "Enter"},
	}
	if !reflect.DeepEqual(seq, want) || !reflect.DeepEqual(f.sleeps, []time.Duration{600 * time.Millisecond}) {
		t.Fatalf("calls %q sleeps %v", seq, f.sleeps)
	}
}

func TestBracketedPasteCannotBeEndedEarly(t *testing.T) {
	f, tm := sendRig(t)
	_ = tm.SendBracketedPaste("nw-s", "a\x1b[201~\r/exit\r")
	sent := typed(f)[0]
	if strings.Count(sent, "\x1b") != 2 || strings.Count(sent, "\x1b[201~") != 1 || !strings.HasSuffix(sent, "\x1b[201~") || strings.Contains(sent, "\r") {
		t.Fatalf("%q", sent)
	}
}

func TestBracketedPasteDropsControls(t *testing.T) {
	f, tm := sendRig(t)
	_ = tm.SendBracketedPasteOnly("nw-s", "a\x00\x03\x04\x7f\u009bb")
	if got := typed(f); !reflect.DeepEqual(got, []string{"\x1b[200~ab\x1b[201~"}) {
		t.Fatalf("%q", got)
	}
	if len(f.calls) != 2 {
		t.Fatalf("paste only pressed something: %d calls", len(f.calls))
	}
}

func TestLiteralThenEnter(t *testing.T) {
	f, tm := sendRig(t)
	_ = tm.SendLiteralThenEnter("nw-s", "/rename Привет")
	if got := typed(f); !reflect.DeepEqual(got, []string{"/rename Привет"}) {
		t.Fatalf("%q", got)
	}
	if !reflect.DeepEqual(f.last()[3:], []string{"send-keys", "-t", "nw-s", "Enter"}) || !reflect.DeepEqual(f.sleeps, []time.Duration{50 * time.Millisecond}) {
		t.Fatalf("enter %q sleeps %v", f.last(), f.sleeps)
	}
	f.reset()
	_ = tm.SendLiteralThenEnter("nw-s", "/rename a\x1b[201~\x03b")
	if sent := typed(f)[0]; strings.Contains(sent, "\x1b") || strings.Contains(sent, "\x03") {
		t.Fatalf("%q", sent)
	}
	f.reset()
	_ = tm.SendLiteral("nw-s", "/rename a;")
	if got := typed(f); !reflect.DeepEqual(got, []string{`/rename a\;`}) {
		t.Fatalf("%q", got)
	}
}

func TestKeyVariants(t *testing.T) {
	f, tm := sendRig(t)
	_ = tm.SendKeySequence("nw-s", "C-x", "C-s")
	_ = tm.SendKeys("nw-s", "claude --resume x")
	_ = tm.SendKeysSeparated("nw-s", "hello")
	var seq []string
	for _, c := range f.calls {
		seq = append(seq, strings.Join(c.args[3:], " "))
	}
	want := []string{
		"copy-mode -q -t nw-s", "send-keys -t nw-s C-x C-s",
		"copy-mode -q -t nw-s", "send-keys -t nw-s claude --resume x Enter",
		"copy-mode -q -t nw-s", "send-keys -t nw-s hello", "send-keys -t nw-s Enter",
	}
	if !reflect.DeepEqual(seq, want) {
		t.Fatalf("%q", seq)
	}
}

// Ported from the previous runtime's tmux literal argument test.
func TestLiteralArg(t *testing.T) {
	for in, want := range map[string]string{`/rename a;`: `/rename a\;`, `/rename a\;`: `/rename a\\;`, `/rename a;b`: `/rename a;b`} {
		if got := LiteralArg(in); got != want {
			t.Errorf("LiteralArg(%q) = %q", in, got)
		}
	}
}

// Ported from the previous runtime's terminal-safe text tests.
func TestStripControlChars(t *testing.T) {
	text := "Привет, мир 👋\n\t- пункт `код` **жирный**\n```ts\nconst a = \"<b>\" && 1;\n```\n"
	if got := StripControlChars(text); got != text {
		t.Fatalf("ordinary text changed: %q", got)
	}
	if got := StripControlChars("a\x1b[201~b"); got != "a"+EscapeMark+"[201~b" {
		t.Fatalf("%q", got)
	}
	if got := StripControlChars("\x1b\x1b"); got != EscapeMark+EscapeMark {
		t.Fatalf("%q", got)
	}
	var c0, c1 strings.Builder
	for r := rune(0); r < 32; r++ {
		if r != '\n' && r != '\t' && r != 0x1b {
			c0.WriteRune(r)
		}
		c1.WriteRune(0x80 + r)
	}
	for _, in := range []string{"x" + c0.String() + "y", "x\x7fy", "x" + c1.String() + "y"} {
		if got := StripControlChars(in); got != "xy" {
			t.Fatalf("%q → %q", in, got)
		}
	}
	if got := StripControlChars("/exit\rnext"); got != "/exitnext" {
		t.Fatalf("%q", got)
	}
	if got := StripControlChars(" ¡ é"); got != " ¡ é" {
		t.Fatalf("%q", got)
	}
	if got := StripControlChars("a\x9bb"); got != "a�b" {
		t.Fatalf("invalid byte: %q", got)
	}
	if got := OneLineSafe(" a\n\tb \x1b[0m\r\n c "); got != "a b "+EscapeMark+"[0m c" {
		t.Fatalf("one line %q", got)
	}
}

// Ported from the previous runtime's agent niceness tests.
func TestParseNiceness(t *testing.T) {
	if n := ParseNiceness("", false); n != 10 {
		t.Fatalf("unset %d", n)
	}
	for raw, want := range map[string]int{"0": 0, "25": 19, "high": 10, "5": 5, "-3": 0, "2.5": 10, " 7 ": 7, "": 0} {
		if got := ParseNiceness(raw, true); got != want {
			t.Errorf("ParseNiceness(%q) = %d, want %d", raw, got, want)
		}
	}
}

func TestCreateSessionCommandLine(t *testing.T) {
	f := newFake()
	tm := f.tmux(Config{Names: fakeNames, ConfigFile: "/app/tmux.conf", Nice: 10, Env: []string{"HOME=/home/u", "SHELL=/bin/zsh"}})
	if err := tm.CreateSession("nw-x", 120, 40, "/work"); err != nil {
		t.Fatal(err)
	}
	c := f.calls[0]
	want := append([]string{"-n", "10", "tmux", "-u", "-L", "main", "-f", "/app/tmux.conf", "new-session", "-d", "-s", "nw-x", "-x", "120", "-y", "40"}, tm.ShellLaunchCommand())
	if c.program != "nice" || c.dir != "/work" || !reflect.DeepEqual(c.args, want) {
		t.Fatalf("%s in %s: %q", c.program, c.dir, c.args)
	}
	if got := f.calls[1].args; !reflect.DeepEqual(got, []string{"-u", "-L", "main", "source-file", "/app/tmux.conf"}) {
		t.Fatalf("then %q", got)
	}
	f.reset()
	plain := f.tmux(Config{Names: fakeNames, Env: []string{"HOME=/home/u"}})
	_ = plain.CreateSession("nw-y", 80, 24, "")
	if c := f.calls[0]; c.program != "tmux" || c.dir != "/home/u" || slices.Contains(c.args, "-f") || len(f.calls) != 1 {
		t.Fatalf("%s in %s: %q (+%d)", c.program, c.dir, c.args, len(f.calls)-1)
	}
}

func TestApplyConfigAndKillServerTouchEveryServer(t *testing.T) {
	f := newFake()
	tm := f.tmux(Config{Names: fakeNames, ConfigFile: "/c"})
	f.servers["main"], f.servers["old"] = nil, nil
	tm.ApplyConfig("")
	tm.KillServer()
	var got []string
	for _, c := range f.calls {
		got = append(got, strings.Join(c.args, " "))
	}
	slices.Sort(got)
	want := []string{"-u -L main kill-server", "-u -L main source-file /c", "-u -L old kill-server", "-u -L old source-file /c"}
	if !reflect.DeepEqual(got, want) || len(f.servers) != 0 {
		t.Fatalf("%q", got)
	}
}

// Ported from the previous runtime's pane environment test: the pane shell keeps the operations tools' variables and
// the TMUX of its pane, gets the PATH floor behind the pane bin folder, and loses the runtime's own variables.
func TestShellLaunchCommandEnvironment(t *testing.T) {
	dir := t.TempDir()
	shell := filepath.Join(dir, "capture-shell")
	script := "#!/bin/sh\nprintf '%s\\n%s\\n%s\\n%s\\n%s\\n%s\\n%s\\n%s' \"$PATH\" \"${GATEWAY_SECRET-unset}\" \"$TMUX\" \"$KUBECONFIG\" \"$TELEPORT_IDENTITY_FILE\" \"$TELEPORT_PROXY\" \"$TERM\" \"$LC_TIME\"\n"
	if err := os.WriteFile(shell, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	tm := NewConfig(Config{Names: Names{Socket: "main"}, PaneBinDir: "/app/bin/pane", Env: []string{
		"NODE_ENV=test", "HOME=" + dir, "SHELL=" + shell, "PATH=/service/private/bin", "KUBECONFIG=/machine/kube config's.yaml",
		"TELEPORT_IDENTITY_FILE=/machine/identity", "TELEPORT_PROXY=proxy.example:443", "GATEWAY_SECRET=private", "TERM=dumb", "LC_TIME=C",
	}})
	cmd := exec.Command("/bin/sh", "-c", tm.ShellLaunchCommand())
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GATEWAY_SECRET=also-private", "TMUX=/test/socket"}
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(out), "\n")
	want := []string{
		"/app/bin/pane:/usr/local/sbin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin", "unset", "/test/socket",
		"/machine/kube config's.yaml", "/machine/identity", "proxy.example:443", "xterm-256color", "C",
	}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("%q", lines)
	}
	if !strings.HasPrefix(tm.ShellLaunchCommand(), `env -i TMUX="$TMUX" TMUX_PANE="$TMUX_PANE" HOME=`) || !strings.HasSuffix(tm.ShellLaunchCommand(), "'"+shell+"' -l") {
		t.Fatalf("%s", tm.ShellLaunchCommand())
	}
}

func TestSessionNames(t *testing.T) {
	tm := New(fakeNames)
	for name, want := range map[string][3]string{
		"nw-ws-AbC123-pane-x1y2z3-tab-q9w8e7": {"ws-AbC123", "pane-x1y2z3", "tab-q9w8e7"},
		"od-ws-a-pane-b-tab-c-d":              {"ws-a", "pane-b", "tab-c-d"},
	} {
		ws, pane, tab, ok := tm.ParseSessionName(name)
		if !ok || [3]string{ws, pane, tab} != want {
			t.Errorf("%s → %s %s %s %v", name, ws, pane, tab, ok)
		}
	}
	for _, name := range []string{"nw-shell-tab1", "xx-ws-a-pane-b-tab-c", "nw-ws-a-tab-c"} {
		if _, _, _, ok := tm.ParseSessionName(name); ok {
			t.Errorf("%s parsed", name)
		}
	}
	if n, ok := tm.PaneShellName("tab-1_x", false); !ok || n != "nw-shell-tab-1_x" {
		t.Fatalf("%s %v", n, ok)
	}
	if n, ok := tm.PaneShellName("tab-1", true); !ok || n != "od-shell-tab-1" {
		t.Fatalf("%s %v", n, ok)
	}
	if _, ok := tm.PaneShellName("a/b", false); ok {
		t.Fatal("bad tab id accepted")
	}
	if !tm.IsAppSession("od-x") || !tm.IsAppSession("nw-x") || tm.IsAppSession("x") {
		t.Fatal("IsAppSession")
	}
}

func TestOwnShellNames(t *testing.T) {
	tm := New(fakeNames)
	name := tm.OwnShellName("tab-k3Jx9QmZ", "mh3k9x2a1bc")
	if name != "nw-v2t-tab-k3Jx9QmZ-mh3k9x2a1bc" {
		t.Fatal(name)
	}
	if id, ok := tm.OwnShellID("tab-k3Jx9QmZ", name); !ok || id != "mh3k9x2a1bc" {
		t.Fatalf("%s %v", id, ok)
	}
	for _, other := range []string{"nw-v2t-tab-other-mh3k9x2a1bc", "nw-v2t-tab-k3Jx9QmZ-", "nw-v2t-tab-k3Jx9QmZ-a-b", "nw-shell-tab-k3Jx9QmZ"} {
		if id, ok := tm.OwnShellID("tab-k3Jx9QmZ", other); ok {
			t.Errorf("%s read as terminal %s", other, id)
		}
	}
	// It is not a tab of the layout, nor a conversation's pane shell.
	if _, _, _, ok := tm.ParseSessionName(name); ok || tm.TabOf(name) != "" {
		t.Fatal("an own terminal looks like a tab")
	}
}

func TestResolveExistingDir(t *testing.T) {
	home := t.TempDir()
	tm := NewConfig(Config{Names: fakeNames, Home: home})
	deep := filepath.Join(home, "a")
	_ = os.Mkdir(deep, 0o755)
	if got := tm.ResolveExistingDir(filepath.Join(deep, "gone", "deeper")); got != deep {
		t.Fatalf("%s", got)
	}
	if got := tm.ResolveExistingDir(""); got != home {
		t.Fatalf("%s", got)
	}
}

// fakeTerminal records what CaptureAtWidth asks of the page's terminal.
type fakeTerminal struct {
	cols, rows int
	open       bool
	paused     bool
	log        []string
}

func (f *fakeTerminal) Size(string) (int, int, bool) { return f.cols, f.rows, f.open }
func (f *fakeTerminal) Pause(string) (int, int, bool) {
	if !f.open || f.paused {
		return 0, 0, false
	}
	f.paused = true
	f.log = append(f.log, "pause")
	return f.cols, f.rows, true
}
func (f *fakeTerminal) Resize(_ string, c, r int) {
	f.log = append(f.log, "resize "+strconv.Itoa(c)+"x"+strconv.Itoa(r))
}
func (f *fakeTerminal) Resume(string) { f.paused = false; f.log = append(f.log, "resume") }

func TestCaptureAtWidth(t *testing.T) {
	f, tm := sendRig(t)
	wide := &fakeTerminal{cols: 100, rows: 60, open: true}
	if _, ok := tm.CaptureAtWidth(wide, "nw-s", 120, 50); !ok {
		t.Fatal("capture failed")
	}
	if got := f.last()[3:]; !reflect.DeepEqual(got, []string{"capture-pane", "-p", "-S", "-50", "-t", "nw-s"}) || len(wide.log) != 0 {
		t.Fatalf("wide terminal: %q %q", got, wide.log)
	}

	f.reset()
	narrow := &fakeTerminal{cols: 40, rows: 60, open: true}
	_, _ = tm.CaptureAtWidth(narrow, "nw-s", 120, 50)
	if want := []string{"pause", "resize 120x60", "resize 40x60", "resume"}; !reflect.DeepEqual(narrow.log, want) {
		t.Fatalf("narrow: %q", narrow.log)
	}
	if got := f.last()[3:]; !reflect.DeepEqual(got, []string{"capture-pane", "-p", "-t", "nw-s"}) || !reflect.DeepEqual(f.sleeps, []time.Duration{300 * time.Millisecond, 300 * time.Millisecond}) {
		t.Fatalf("narrow capture %q sleeps %v", got, f.sleeps)
	}

	f.reset()
	short := &fakeTerminal{cols: 100, rows: 20, open: true}
	_, _ = tm.CaptureAtWidth(short, "nw-s", 80, 24)
	if want := []string{"pause", "resize 100x24", "resize 100x20", "resume"}; !reflect.DeepEqual(short.log, want) {
		t.Fatalf("short: %q", short.log)
	}

	for _, term := range []Terminal{nil, &fakeTerminal{}, &fakeTerminal{cols: 40, rows: 20, open: true, paused: true}} {
		f.reset()
		_, _ = tm.CaptureAtWidth(term, "nw-s", 120, 50)
		if got := f.last()[3:]; !reflect.DeepEqual(got, []string{"capture-pane", "-p", "-t", "nw-s"}) || len(f.sleeps) != 0 {
			t.Fatalf("no terminal: %q %v", got, f.sleeps)
		}
	}
}

// Integrations hand a new pane shell their variables, keyed by the tab the session belongs to; values are quoted for
// the shell and names that are not variable names are dropped.
func TestCreateSessionPaneEnv(t *testing.T) {
	dir := t.TempDir()
	shell := filepath.Join(dir, "capture-shell")
	script := "#!/bin/sh\nprintf '%s|%s|%s|%s' \"$ARTIFACTS_SESSION\" \"$BOARDS_TOKEN\" \"${BAD-unset}\" \"$HOME\"\n"
	if err := os.WriteFile(shell, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	var asked []string
	f := newFake()
	tm := f.tmux(Config{Names: fakeNames, Env: []string{"HOME=" + dir, "SHELL=" + shell}, PaneEnv: func(tab string) []string {
		asked = append(asked, tab)
		return []string{"ARTIFACTS_SESSION=" + tab, "BOARDS_TOKEN=it's $secret", "BAD NAME=x", "noequals"}
	}})
	session := tm.SessionName("ws-a", "pane-b", "tab-c")
	if err := tm.CreateSession(session, 80, 24, dir); err != nil {
		t.Fatal(err)
	}
	args := f.calls[0].args
	command := args[len(args)-1]
	if !strings.Contains(command, ` ARTIFACTS_SESSION='tab-c' BOARDS_TOKEN='it'\''s $secret' '`) || strings.Contains(command, "BAD") {
		t.Fatalf("%s", command)
	}
	out, err := exec.Command("/bin/sh", "-c", command).Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "tab-c|it's $secret|unset|"+dir {
		t.Fatalf("%q", out)
	}
	shellOfTab, _ := tm.PaneShellName("tab-d", false)
	_ = tm.CreateSession(shellOfTab, 80, 24, dir)
	_ = tm.CreateSession(tm.NewSessionName(), 80, 24, dir)
	if !reflect.DeepEqual(asked, []string{"tab-c", "tab-d", ""}) {
		t.Fatalf("tabs asked for: %q", asked)
	}
	if tm.ShellLaunchCommand() == command {
		t.Fatal("the plain launch command has no pane variables")
	}
}

func TestTabOf(t *testing.T) {
	tm := New(Names{Socket: "main", Prefix: "nw-", LegacyPrefix: "ow-"})
	for session, want := range map[string]string{
		"nw-ws-a-pane-b-tab-c": "tab-c", "ow-ws-a-pane-b-tab-c": "tab-c", "nw-shell-tab-x": "tab-x", "ow-shell-tab-y": "tab-y",
		"nw-abc-def-ghi": "", "other-ws-a-pane-b-tab-c": "", "nw-shell-": "",
	} {
		if got := tm.TabOf(session); got != want {
			t.Errorf("TabOf(%q) = %q, want %q", session, got, want)
		}
	}
}
