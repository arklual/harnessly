package agentlogin

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents/agentstest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Logins run in a pane of a tmux server of their own. These tests start real ones, on a socket made for the test, and
// end only that server.

func tmuxProgram(t *testing.T) string {
	t.Helper()
	for _, p := range []string{"/usr/bin/tmux", "/usr/local/bin/tmux", "/opt/homebrew/bin/tmux"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	p, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("no tmux")
	}
	return p
}

type bench struct {
	t   *testing.T
	rig *agentstest.Rig
	mgr *Manager

	mu       sync.Mutex
	logins   []hubproto.Login
	finished []string
	console  []string // the commands the console was asked to run
	socket   string
	tmux     string

	consoleURL string
	consoleErr error
}

// statusOf is a stand-in for the status probe: signed in when the fake CLI has made ~/.signed-in.
func (b *bench) status(_ context.Context, provider string) hubproto.AgentStatus {
	st := hubproto.AgentStatus{Provider: provider, Installed: true, Login: hubproto.LoggedOut}
	if _, err := os.Stat(filepath.Join(b.rig.Home, ".signed-in")); err == nil {
		st.Login = hubproto.LoggedIn
		mode := hubproto.AuthSubscription
		st.AuthMode = &mode
	}
	return st
}

func newBench(t *testing.T, timing Timing) *bench {
	t.Helper()
	tm := tmuxProgram(t)
	rig := agentstest.New(t)
	b := &bench{t: t, rig: rig, tmux: tm, socket: fmt.Sprintf("hl-agentlogin-test-%d", rand.IntN(1_000_000_000)), consoleURL: "/m/abc/?tab=t1"}
	t.Cleanup(func() {
		_ = exec.Command(tm, "-L", b.socket, "kill-server").Run()
		dir := os.Getenv("TMUX_TMPDIR")
		if dir == "" {
			dir = "/tmp"
		}
		_ = os.Remove(filepath.Join(dir, fmt.Sprintf("tmux-%d", os.Getuid()), b.socket))
	})
	if timing.ScreenEvery == 0 {
		timing.ScreenEvery = 40 * time.Millisecond
	}
	if timing.StatusEvery == 0 {
		timing.StatusEvery = 150 * time.Millisecond
	}
	if timing.URLWait == 0 {
		timing.URLWait = 20 * time.Second
	}
	if timing.AppServer == 0 {
		timing.AppServer = 10 * time.Second
	}
	if timing.VerifyWait == 0 {
		timing.VerifyWait = 20 * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		b.mgr.Wait()
	})
	b.mgr = New(ctx, Config{
		Shell: rig.Checker, Status: b.status, EnvDir: rig.EnvDir, Dir: filepath.Join(rig.Home, ".harnessly", "login"),
		Tmux: tm, Socket: b.socket, Timing: timing,
		Emit: func(l hubproto.Login) { b.mu.Lock(); b.logins = append(b.logins, l); b.mu.Unlock() },
		Finished: func(p string) {
			b.mu.Lock()
			b.finished = append(b.finished, p)
			b.mu.Unlock()
		},
		Console: func(_ context.Context, provider, command string) (string, error) {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.console = append(b.console, provider+": "+command)
			return b.consoleURL, b.consoleErr
		},
	})
	return b
}

// states are the states the hub was told about, in order, without repeats.
func (b *bench) states() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, l := range b.logins {
		if len(out) == 0 || out[len(out)-1] != l.State {
			out = append(out, l.State)
		}
	}
	return out
}

func (b *bench) last() hubproto.Login {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.logins) == 0 {
		return hubproto.Login{}
	}
	return b.logins[len(b.logins)-1]
}

// waitFor polls until the latest login state is state.
func (b *bench) waitFor(state string) hubproto.Login {
	b.t.Helper()
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		if l := b.last(); l.State == state {
			return l
		}
		time.Sleep(20 * time.Millisecond)
	}
	b.t.Fatalf("no state %q; states %v, last %+v", state, b.states(), b.last())
	return hubproto.Login{}
}

func (b *bench) sessions() string {
	out, _ := exec.Command(b.tmux, "-L", b.socket, "list-sessions").CombinedOutput()
	if strings.Contains(string(out), "no server") || strings.Contains(string(out), "error connecting") {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (b *bench) noSessions() {
	b.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if b.sessions() == "" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	b.t.Fatalf("a session is left: %s", b.sessions())
}

func (b *bench) readHome(name string) string {
	got, _ := os.ReadFile(filepath.Join(b.rig.Home, name))
	return strings.TrimSpace(string(got))
}

// fakeClaudeLogin shows the screen the real CLI shows (testdata/claude_login.txt, one logical line for the address),
// reads the pasted code and takes it or not.
const fakeClaudeLogin = `
case "$1 $2" in
  "auth login")
    echo "Opening browser to sign in…"
    echo "If the browser didn't open, visit: https://claude.com/cai/oauth/authorize?code=true&client_id=9d1c250a&response_type=code&state=st1-$BROWSER-$DISPLAY"
    printf "Paste code here if prompted > "
    read -r code
    printf '%s' "$code" > "$HOME/typed-code"
    if [ "$code" = "good-code" ]; then touch "$HOME/.signed-in"; echo; echo "Login successful."; exit 0; fi
    echo; echo "OAuth error: invalid code ($code)"; exit 1 ;;
esac
`

func TestClaudeLinkLogin(t *testing.T) {
	b := newBench(t, Timing{})
	b.rig.Script("claude", fakeClaudeLogin)
	b.rig.WriteEnv("claude.env", "HTTPS_PROXY='http://proxy.example.com:3128'\n")

	l, err := b.mgr.Start("login1", "claude", "link")
	if err != nil {
		t.Fatal(err)
	}
	if l.State != "starting" || l.ID != "login1" || l.Provider != "claude" || l.Method != "link" || l.URL != nil {
		t.Fatalf("%+v", l)
	}
	if _, busy := b.mgr.Busy(); !busy {
		t.Error("not busy while it runs")
	}
	at := b.waitFor("url")
	// The address is the one on the screen, with the browser and the display unset for the CLI.
	if at.URL == nil || *at.URL != "https://claude.com/cai/oauth/authorize?code=true&client_id=9d1c250a&response_type=code&state=st1--" {
		t.Fatalf("url %v", at.URL)
	}
	if !at.NeedsCode || at.UserCode != nil || at.ExpiresAt == nil {
		t.Fatalf("%+v", at)
	}
	if ms := *at.ExpiresAt - time.Now().UnixMilli(); ms < 9*60*1000 || ms > 10*60*1000 {
		t.Errorf("expires in %d ms", ms)
	}

	// The code goes to the pane as typed, with its odd characters.
	if err := b.mgr.Code("login1", "good-code"); err != nil {
		t.Fatal(err)
	}
	if err := b.mgr.Code("login1", "again"); err == nil {
		t.Error("a second code was taken")
	}
	done := b.waitFor("done")
	if done.Status == nil || done.Status.Login != "logged_in" || done.URL != nil || done.NeedsCode || done.Error != nil {
		t.Fatalf("%+v", done)
	}
	if got := b.states(); strings.Join(got, ",") != "url,verifying,done" {
		t.Errorf("states %v", got)
	}
	b.mgr.Wait()
	b.noSessions()
	if _, busy := b.mgr.Busy(); busy {
		t.Error("still busy")
	}
	b.mu.Lock()
	if len(b.finished) != 1 || b.finished[0] != "claude" {
		t.Errorf("finished %v", b.finished)
	}
	b.mu.Unlock()
	// No launcher is left behind.
	if left, _ := filepath.Glob(filepath.Join(b.rig.Home, ".harnessly", "login", "*")); len(left) != 0 {
		t.Errorf("left %v", left)
	}
}

func TestClaudeCodeIsTypedLiterally(t *testing.T) {
	b := newBench(t, Timing{})
	b.rig.Script("claude", fakeClaudeLogin)
	if _, err := b.mgr.Start("l", "claude", "link"); err != nil {
		t.Fatal(err)
	}
	b.waitFor("url")
	// Key names, quotes, a semicolon and a dash: all text.
	const weird = `Enter; 'x' "y" $HOME -n C-c #5`
	if err := b.mgr.Code("l", weird); err != nil {
		t.Fatal(err)
	}
	failed := b.waitFor("failed")
	if got := b.readHome("typed-code"); got != weird {
		t.Errorf("the pane got %q", got)
	}
	// The failure says what the CLI said, without the code and without addresses.
	if failed.Error == nil || !strings.Contains(*failed.Error, "invalid code") || strings.Contains(*failed.Error, "Enter;") || strings.Contains(*failed.Error, "https://") {
		t.Fatalf("%v", failed.Error)
	}
	if failed.Status != nil || failed.URL != nil {
		t.Fatalf("%+v", failed)
	}
	b.mgr.Wait()
	b.noSessions()
}

func TestClaudeNoAddressOnScreen(t *testing.T) {
	b := newBench(t, Timing{URLWait: 1500 * time.Millisecond})
	b.rig.Script("claude", `echo "Something went wrong"; sleep 30`)
	if _, err := b.mgr.Start("l", "claude", "link"); err != nil {
		t.Fatal(err)
	}
	l := b.waitFor("failed")
	if l.Error == nil || *l.Error != "Не нашли ссылку входа, войдите в консоли" {
		t.Fatalf("%v", l.Error)
	}
	b.mgr.Wait()
	b.noSessions()
	if got := b.states(); strings.Join(got, ",") != "failed" {
		t.Errorf("%v", got)
	}
}

func TestClaudeNotInstalled(t *testing.T) {
	b := newBench(t, Timing{})
	l, err := b.mgr.Start("l", "claude", "link")
	if err != nil {
		t.Fatal(err)
	}
	_ = l
	f := b.waitFor("failed")
	got := "<nil>"
	if f.Error != nil {
		got = *f.Error
	}
	if got != "Claude Code не установлен на машине" {
		t.Fatalf("error %q", got)
	}
}

func TestClaudeExpires(t *testing.T) {
	b := newBench(t, Timing{ClaudeTTL: 2500 * time.Millisecond})
	b.rig.Script("claude", fakeClaudeLogin)
	if _, err := b.mgr.Start("l", "claude", "link"); err != nil {
		t.Fatal(err)
	}
	b.waitFor("url")
	l := b.waitFor("expired")
	if l.Error == nil || l.URL != nil || l.NeedsCode {
		t.Fatalf("%+v", l)
	}
	b.mgr.Wait()
	b.noSessions()
	if err := b.mgr.Code("l", "late"); err == nil {
		t.Error("a code after the end")
	}
}

func TestClaudeCancel(t *testing.T) {
	b := newBench(t, Timing{})
	b.rig.Script("claude", fakeClaudeLogin)
	if _, err := b.mgr.Start("l", "claude", "link"); err != nil {
		t.Fatal(err)
	}
	b.waitFor("url")
	if err := b.mgr.Cancel("l"); err != nil {
		t.Fatal(err)
	}
	l := b.waitFor("cancelled")
	if l.Error != nil || l.URL != nil {
		t.Fatalf("%+v", l)
	}
	b.mgr.Wait()
	b.noSessions()
	// Cancelling again, or a login that ended, is not an error; one that never was, is.
	if err := b.mgr.Cancel("l"); err != nil {
		t.Error(err)
	}
	if err := b.mgr.Cancel("nope"); hubErr(err) != "not_found" {
		t.Errorf("%v", err)
	}
	// And it is free again.
	if _, err := b.mgr.Start("l2", "claude", "link"); err != nil {
		t.Fatal(err)
	}
	b.waitFor("url")
}

func hubErr(err error) string {
	if e, ok := err.(*hubproto.Error); ok {
		return e.Code
	}
	return fmt.Sprint(err)
}

func TestClaudeSignedInMeanwhile(t *testing.T) {
	// The CLI ends by itself (the login was completed in another way) before any code: the status decides.
	b := newBench(t, Timing{})
	b.rig.Script("claude", `
echo "If the browser didn't open, visit: https://claude.ai/oauth/authorize?code=true&state=s"
sleep 1
touch "$HOME/.signed-in"
exit 0`)
	if _, err := b.mgr.Start("l", "claude", "link"); err != nil {
		t.Fatal(err)
	}
	l := b.waitFor("done")
	if l.Status == nil || l.Status.Login != "logged_in" {
		t.Fatalf("%+v", l)
	}
}

func TestOneLoginAtATimeAndBadRequests(t *testing.T) {
	b := newBench(t, Timing{})
	b.rig.Script("claude", fakeClaudeLogin)
	if _, err := b.mgr.Start("a", "claude", "link"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, provider, method, code string }{
		{"b", "codex", "link", "busy"},
		{"b", "claude", "console", "busy"},
		{"b", "gemini", "link", "unsupported"},
		{"b", "claude", "sms", "invalid"},
		{"", "claude", "link", "invalid"},
		{"a", "claude", "link", "invalid"},
	} {
		_, err := b.mgr.Start(tc.id, tc.provider, tc.method)
		if hubErr(err) != tc.code {
			t.Errorf("%+v: %v", tc, err)
		}
	}
	// Codes: not before the address, never an empty or multi-line one, never to a login that is not there.
	if err := b.mgr.Code("a", "x"); hubErr(err) != "invalid" && hubErr(err) != "not_found" {
		t.Errorf("%v", err)
	}
	if err := b.mgr.Code("zzz", "x"); hubErr(err) != "not_found" {
		t.Errorf("%v", err)
	}
	b.waitFor("url")
	for _, bad := range []string{"", "   ", "a\nb", "a\x1b[A", strings.Repeat("x", 600)} {
		if err := b.mgr.Code("a", bad); hubErr(err) != "invalid" {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if l, ok := b.mgr.Get("a"); !ok || l.State != "url" || !l.NeedsCode {
		t.Errorf("%+v", l)
	}
	if _, ok := b.mgr.Get("zzz"); ok {
		t.Error("a login out of nothing")
	}
}

// fakeCodex speaks the app-server protocol as the real one: initialize, a device-code login, and the completion
// notice. The login is confirmed a second after it starts.
const fakeCodexAppServer = `
case "$1" in
  app-server)
    while read -r line; do
      id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
      case "$line" in
        *'"method":"initialize"'*)
          printf '{"id":%s,"result":{"userAgent":"x","codexHome":"/h","platformFamily":"unix","platformOs":"linux"}}\n' "$id" ;;
        *'"method":"account/login/start"'*)
          printf '%s\n' "$line" > "$HOME/login-start"
          printf '{"method":"remoteControl/status/changed","params":{"status":"disabled"}}\n'
          printf '{"id":%s,"result":{"type":"chatgptDeviceCode","loginId":"L1","verificationUrl":"https://auth.openai.com/codex/device","userCode":"K7QF-2M9P"}}\n' "$id"
          if [ ! -f "$HOME/hold" ]; then
            ( sleep 1; touch "$HOME/.signed-in"; printf '{"method":"account/login/completed","params":{"success":true,"loginId":"L1","error":null}}\n' ) &
          fi ;;
        *'"method":"account/login/cancel"'*)
          printf '%s\n' "$line" > "$HOME/login-cancel"
          printf '{"id":%s,"result":{}}\n' "$id" ;;
      esac
    done ;;
esac
`

func TestCodexLoginThroughAppServer(t *testing.T) {
	b := newBench(t, Timing{})
	b.rig.Script("codex", fakeCodexAppServer)
	if _, err := b.mgr.Start("c1", "codex", "link"); err != nil {
		t.Fatal(err)
	}
	at := b.waitFor("url")
	if at.URL == nil || *at.URL != "https://auth.openai.com/codex/device" || at.UserCode == nil || *at.UserCode != "K7QF-2M9P" || at.NeedsCode {
		t.Fatalf("%+v", at)
	}
	if ms := *at.ExpiresAt - time.Now().UnixMilli(); ms < 14*60*1000 || ms > 15*60*1000 {
		t.Errorf("expires in %d ms", ms)
	}
	done := b.waitFor("done")
	if done.Status == nil || done.Status.Login != "logged_in" || done.UserCode != nil {
		t.Fatalf("%+v", done)
	}
	if got := b.readHome("login-start"); !strings.Contains(got, `"type":"chatgptDeviceCode"`) {
		t.Errorf("login start: %s", got)
	}
	if got := strings.Join(b.states(), ","); got != "url,done" {
		t.Errorf("states %s", got)
	}
	b.mgr.Wait()
	// No pane was needed.
	b.noSessions()
	// A code does not belong to this login.
	if err := b.mgr.Code("c1", "123"); err == nil {
		t.Error("a code for Codex")
	}
}

func TestCodexAppServerCancelTellsTheServer(t *testing.T) {
	b := newBench(t, Timing{})
	b.rig.WriteHome("hold", "x", 0o644)
	b.rig.Script("codex", fakeCodexAppServer)
	if _, err := b.mgr.Start("c1", "codex", "link"); err != nil {
		t.Fatal(err)
	}
	b.waitFor("url")
	if err := b.mgr.Cancel("c1"); err != nil {
		t.Fatal(err)
	}
	b.waitFor("cancelled")
	b.mgr.Wait()
	// The request may land a moment before the process is stopped.
	deadline := time.Now().Add(3 * time.Second)
	for b.readHome("login-cancel") == "" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := b.readHome("login-cancel"); !strings.Contains(got, `"loginId":"L1"`) {
		t.Errorf("cancel: %q", got)
	}
}

func TestCodexAppServerFailedLogin(t *testing.T) {
	b := newBench(t, Timing{})
	b.rig.Script("codex", `
read -r line; printf '{"id":1,"result":{}}\n'
read -r line
read -r line; printf '{"id":2,"result":{"type":"chatgptDeviceCode","loginId":"L9","verificationUrl":"https://auth.openai.com/codex/device","userCode":"AAAA-BBBB"}}\n'
sleep 0.5
printf '{"method":"account/login/completed","params":{"success":false,"loginId":"L9","error":"device code expired https://auth.openai.com/x?s=1"}}\n'
sleep 5`)
	if _, err := b.mgr.Start("c1", "codex", "link"); err != nil {
		t.Fatal(err)
	}
	f := b.waitFor("failed")
	if f.Error == nil || !strings.Contains(*f.Error, "device code expired") || strings.Contains(*f.Error, "https://") {
		t.Fatalf("%v", f.Error)
	}
}

func TestCodexExpires(t *testing.T) {
	b := newBench(t, Timing{CodexTTL: 2500 * time.Millisecond})
	b.rig.WriteHome("hold", "x", 0o644)
	b.rig.Script("codex", fakeCodexAppServer)
	if _, err := b.mgr.Start("c1", "codex", "link"); err != nil {
		t.Fatal(err)
	}
	b.waitFor("url")
	b.waitFor("expired")
}

// Where the app-server cannot be used, the screen of `codex login --device-auth` gives the same address and code.
func TestCodexLoginFromTheScreen(t *testing.T) {
	b := newBench(t, Timing{})
	screen, err := os.ReadFile("testdata/codex_device.txt")
	if err != nil {
		t.Fatal(err)
	}
	b.rig.WriteHome("screen.txt", string(screen), 0o644)
	b.rig.Script("codex", `
case "$1 $2" in
  "app-server ") echo "error: unrecognized subcommand 'app-server'" >&2; exit 2 ;;
  "login --device-auth")
    cat "$HOME/screen.txt"
    sleep 1
    touch "$HOME/.signed-in"
    echo "Successfully logged in"
    exit 0 ;;
esac
exit 2`)
	if _, err := b.mgr.Start("c1", "codex", "link"); err != nil {
		t.Fatal(err)
	}
	at := b.waitFor("url")
	if at.URL == nil || *at.URL != "https://auth.openai.com/codex/device" || at.UserCode == nil || *at.UserCode != "H3CF-2WRYQ" {
		t.Fatalf("%+v", at)
	}
	done := b.waitFor("done")
	if done.Status == nil || done.Status.Login != "logged_in" {
		t.Fatalf("%+v", done)
	}
	b.mgr.Wait()
	b.noSessions()
}

func TestCodexNoAddressAnywhere(t *testing.T) {
	b := newBench(t, Timing{URLWait: 1500 * time.Millisecond, AppServer: 1500 * time.Millisecond})
	b.rig.Script("codex", `echo "nothing useful"; sleep 30`)
	if _, err := b.mgr.Start("c1", "codex", "link"); err != nil {
		t.Fatal(err)
	}
	f := b.waitFor("failed")
	if f.Error == nil || *f.Error != "Не нашли ссылку входа, войдите в консоли" {
		t.Fatalf("%v", f.Error)
	}
	b.mgr.Wait()
	b.noSessions()
}

func TestConsoleLogin(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			b := newBench(t, Timing{})
			b.rig.WriteEnv("all.env", "HTTPS_PROXY='http://proxy.example.com:3128'\n")
			if _, err := b.mgr.Start("k1", provider, "console"); err != nil {
				t.Fatal(err)
			}
			w := b.waitFor("waiting")
			if w.ConsoleURL == nil || *w.ConsoleURL != "/m/abc/?tab=t1" || w.URL != nil || w.ExpiresAt == nil {
				t.Fatalf("%+v", w)
			}
			if ms := *w.ExpiresAt - time.Now().UnixMilli(); ms < 14*60*1000 || ms > 15*60*1000 {
				t.Errorf("expires in %d ms", ms)
			}
			// The person signs in by hand in the tab.
			time.Sleep(300 * time.Millisecond)
			b.rig.WriteHome(".signed-in", "x", 0o644)
			done := b.waitFor("done")
			if done.Status == nil || done.ConsoleURL != nil {
				t.Fatalf("%+v", done)
			}
			if got := strings.Join(b.states(), ","); got != "waiting,done" {
				t.Errorf("states %s", got)
			}
			b.mu.Lock()
			cmds := b.console
			b.mu.Unlock()
			want := map[string]string{"claude": "claude auth login --claudeai", "codex": "codex login --device-auth"}[provider]
			if len(cmds) != 1 || !strings.HasPrefix(cmds[0], provider+": set -a; . '") || !strings.HasSuffix(cmds[0], "; set +a; "+want) {
				t.Errorf("console asked for %v", cmds)
			}
		})
	}
}

func TestConsoleFailureAndExpiry(t *testing.T) {
	b := newBench(t, Timing{})
	b.consoleErr = fmt.Errorf("terminal-tab: 503 runtime is starting https://x/y")
	if _, err := b.mgr.Start("k1", "claude", "console"); err != nil {
		t.Fatal(err)
	}
	f := b.waitFor("failed")
	if f.Error == nil || !strings.Contains(*f.Error, "runtime is starting") || strings.Contains(*f.Error, "https://") {
		t.Fatalf("%v", f.Error)
	}
	b.mgr.Wait()

	b2 := newBench(t, Timing{ConsoleTTL: 1200 * time.Millisecond})
	if _, err := b2.mgr.Start("k2", "codex", "console"); err != nil {
		t.Fatal(err)
	}
	b2.waitFor("waiting")
	b2.waitFor("expired")
}

func TestConsoleCancel(t *testing.T) {
	b := newBench(t, Timing{})
	if _, err := b.mgr.Start("k1", "codex", "console"); err != nil {
		t.Fatal(err)
	}
	b.waitFor("waiting")
	if err := b.mgr.Cancel("k1"); err != nil {
		t.Fatal(err)
	}
	b.waitFor("cancelled")
}

// The agent going down ends a login in progress the same way, and takes its pane with it.
func TestManagerShutdown(t *testing.T) {
	b := newBench(t, Timing{})
	b.rig.Script("claude", fakeClaudeLogin)
	ctx, cancel := context.WithCancel(context.Background())
	m := New(ctx, b.mgr.cfg)
	if _, err := m.Start("l", "claude", "link"); err != nil {
		t.Fatal(err)
	}
	b.waitFor("url")
	cancel()
	m.Wait()
	b.waitFor("cancelled")
	b.noSessions()
}
