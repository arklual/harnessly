package loginflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// The CLIs here are small shell scripts that print what the real ones print and read what they read. No account, no
// network; every key and code is a made-up one.

const fakeCode = "fake-CODE_0123456789abcdef"

type rig struct {
	t       *testing.T
	dir     string
	s       *Service
	changed chan wire.Provider
}

func newRig(t *testing.T, claude, codex string) *rig {
	t.Helper()
	dir := t.TempDir()
	r := &rig{t: t, dir: dir, changed: make(chan wire.Provider, 8)}
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
			t.Fatal(err)
		}
		return p
	}
	bins := map[wire.Provider]string{}
	if claude != "" {
		bins[wire.ProviderClaude] = write("claude", claude)
	}
	if codex != "" {
		bins[wire.ProviderCodex] = write("codex", codex)
	}
	r.s = New(Config{
		Bin: func(p wire.Provider) (string, error) {
			if b, ok := bins[p]; ok {
				return b, nil
			}
			return "", errors.New("none")
		},
		Env:      func() []string { return []string{"PATH=/usr/bin:/bin", "HOME=" + dir, "OUT=" + dir, "TERM=dumb"} },
		OnChange: func(p wire.Provider) { r.changed <- p },
	})
	return r
}

func (r *rig) read(name string) string {
	b, _ := os.ReadFile(filepath.Join(r.dir, name))
	return string(b)
}

func (r *rig) wait(p wire.Provider, want wire.LoginState) wire.LoginFlow {
	r.t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		st, _ := r.s.Login(context.Background(), p)
		if st.Flow != nil && st.Flow.State == want {
			return *st.Flow
		}
	}
	st, _ := r.s.Login(context.Background(), p)
	r.t.Fatalf("never %s: %+v", want, st.Flow)
	return wire.LoginFlow{}
}

// Claude prints its link twice (a terminal link and the plain text), then waits for the code.
const claudeScript = `printf 'Opening browser to sign in\n'
printf 'If the browser did not open, visit: \033]8;;https://claude.com/cai/oauth/authorize?code=true&state=abc\007\033[94mhttps://claude.com/cai/oauth/authorize?code=true&state=abc\033[39m\033]8;;\007\n'
printf 'Paste code here if prompted > '
read code
echo "$code" > "$OUT/typed"
[ "$code" = "` + fakeCode + `" ] || exit 1
exit 0
`

func TestClaudeSubscriptionFlow(t *testing.T) {
	r := newRig(t, claudeScript, "")
	st, err := r.s.StartLogin(context.Background(), wire.ProviderClaude, wire.LoginSubscription)
	if err != nil || st.Flow == nil {
		t.Fatal(err)
	}
	f := *st.Flow
	if f.State != wire.LoginAwaiting || f.Input != wire.LoginInputCode || f.URL == nil || *f.URL != "https://claude.com/cai/oauth/authorize?code=true&state=abc" || f.UserCode != nil {
		t.Fatalf("%+v", f)
	}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.StartLogin(context.Background(), wire.ProviderClaude, wire.LoginSubscription); !errors.Is(err, wire.ErrLoginBusy) {
		t.Fatalf("a second start: %v", err)
	}
	if _, err := r.s.SubmitLoginCode(context.Background(), wire.ProviderClaude, strings.Repeat("0", 32), fakeCode); !errors.Is(err, wire.ErrLoginStale) {
		t.Fatalf("a code for another flow: %v", err)
	}
	if got := r.read("typed"); got != "" {
		t.Fatalf("typed %q before the code", got)
	}
	if st, err := r.s.SubmitLoginCode(context.Background(), wire.ProviderClaude, f.ID, fakeCode); err != nil || st.Flow.State != wire.LoginChecking {
		t.Fatalf("%v %+v", err, st.Flow)
	}
	done := r.wait(wire.ProviderClaude, wire.LoginDone)
	if done.URL == nil && done.Validate() != nil {
		t.Fatal(done.Validate())
	}
	if r.read("typed") != fakeCode+"\n" {
		t.Fatalf("the CLI got %q", r.read("typed"))
	}
	select {
	case p := <-r.changed:
		if p != wire.ProviderClaude {
			t.Fatal(p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no change told")
	}
	if _, err := r.s.SubmitLoginCode(context.Background(), wire.ProviderClaude, f.ID, fakeCode); !errors.Is(err, wire.ErrLoginStale) {
		t.Fatalf("a code for a finished flow: %v", err)
	}
}

func TestWrongCodeFails(t *testing.T) {
	r := newRig(t, claudeScript, "")
	st, _ := r.s.StartLogin(context.Background(), wire.ProviderClaude, wire.LoginConsole)
	if _, err := r.s.SubmitLoginCode(context.Background(), wire.ProviderClaude, st.Flow.ID, "wrong-CODE_0123456789"); err != nil {
		t.Fatal(err)
	}
	f := r.wait(wire.ProviderClaude, wire.LoginFailed)
	if f.Reason == nil || *f.Reason != wire.LoginCodeRejected || f.Validate() != nil {
		t.Fatalf("%+v", f)
	}
	select {
	case <-r.changed:
		t.Fatal("a failed flow tells a change")
	case <-time.After(200 * time.Millisecond):
	}
}

// Whatever the page sends, only text reaches the terminal: a code with a line end or an escape is refused before it.
func TestCodeIsTextOnly(t *testing.T) {
	r := newRig(t, claudeScript, "")
	st, _ := r.s.StartLogin(context.Background(), wire.ProviderClaude, wire.LoginSubscription)
	for _, bad := range []string{"abcdefgh\r", "abcdefgh\nrm -rf /", "abcdefgh\x03", "abcdefgh\x1b[A", "short", "has space in it", strings.Repeat("a", 600)} {
		if _, err := r.s.SubmitLoginCode(context.Background(), wire.ProviderClaude, st.Flow.ID, bad); !errors.Is(err, wire.ErrLoginInput) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if r.read("typed") != "" {
		t.Fatalf("the terminal got %q", r.read("typed"))
	}
	_, _ = r.s.CancelLogin(context.Background(), wire.ProviderClaude)
}

func TestCancelEndsTheProcess(t *testing.T) {
	r := newRig(t, claudeScript+"", "")
	st, _ := r.s.StartLogin(context.Background(), wire.ProviderClaude, wire.LoginSubscription)
	got, err := r.s.CancelLogin(context.Background(), wire.ProviderClaude)
	if err != nil || got.Flow.State != wire.LoginCancelled || got.Flow.ID != st.Flow.ID || got.Flow.URL != nil {
		t.Fatalf("%v %+v", err, got.Flow)
	}
	if _, err := r.s.StartLogin(context.Background(), wire.ProviderClaude, wire.LoginSubscription); err != nil {
		t.Fatalf("a new flow after a cancel: %v", err)
	}
	_, _ = r.s.CancelLogin(context.Background(), wire.ProviderClaude)
}

func TestStartsAreLimited(t *testing.T) {
	r := newRig(t, claudeScript, "")
	var last error
	for i := 0; i < StartsPer+1; i++ {
		_, last = r.s.StartLogin(context.Background(), wire.ProviderClaude, wire.LoginSubscription)
		if last == nil {
			_, _ = r.s.CancelLogin(context.Background(), wire.ProviderClaude)
		}
	}
	if !errors.Is(last, wire.ErrLoginRate) {
		t.Fatalf("the %dth start: %v", StartsPer+1, last)
	}
}

func TestModesAndInstall(t *testing.T) {
	r := newRig(t, claudeScript, "")
	if _, err := r.s.StartLogin(context.Background(), wire.ProviderClaude, wire.LoginDevice); !errors.Is(err, wire.ErrLoginUnsupported) {
		t.Fatalf("%v", err)
	}
	if _, err := r.s.StartLogin(context.Background(), wire.ProviderCodex, wire.LoginDevice); !errors.Is(err, wire.ErrLoginNotInstalled) {
		t.Fatalf("%v", err)
	}
	if err := r.s.SaveAPIKey(context.Background(), wire.ProviderClaude, "sk-test-0000-abcdef"); !errors.Is(err, wire.ErrLoginUnsupported) {
		t.Fatalf("Claude takes no key this way: %v", err)
	}
}

// A CLI that prints a link to another host is not believed: the flow fails instead of showing it.
func TestOnlyTheVendorsLinkIsShown(t *testing.T) {
	old := StartTimeout
	StartTimeout = 600 * time.Millisecond
	defer func() { StartTimeout = old }()
	r := newRig(t, `printf 'visit https://evil.example/phish?x=1\n'; sleep 30`, "")
	st, err := r.s.StartLogin(context.Background(), wire.ProviderClaude, wire.LoginSubscription)
	if err != nil || st.Flow.State != wire.LoginFailed || *st.Flow.Reason != wire.LoginStartFailed || st.Flow.URL != nil {
		t.Fatalf("%v %+v", err, st.Flow)
	}
}

const codexDevice = `printf '\033[1mWelcome to Codex\033[0m\n\n1. Open this link\n   https://auth.openai.com/codex/device\n\n2. Enter this one-time code (expires in 15 minutes)\n   HM77-ESPN1\n\n'
while [ ! -e "$OUT/approved" ]; do sleep 0.05; done
exit 0
`

func TestCodexDeviceFlow(t *testing.T) {
	r := newRig(t, "", codexDevice)
	st, err := r.s.StartLogin(context.Background(), wire.ProviderCodex, wire.LoginDevice)
	if err != nil {
		t.Fatal(err)
	}
	f := *st.Flow
	if f.State != wire.LoginAwaiting || f.Input != wire.LoginInputNone || *f.URL != "https://auth.openai.com/codex/device" || f.UserCode == nil || *f.UserCode != "HM77-ESPN1" || f.Validate() != nil {
		t.Fatalf("%+v", f)
	}
	if _, err := r.s.SubmitLoginCode(context.Background(), wire.ProviderCodex, f.ID, fakeCode); !errors.Is(err, wire.ErrLoginStale) {
		t.Fatalf("a device flow takes no code: %v", err)
	}
	_ = os.WriteFile(filepath.Join(r.dir, "approved"), nil, 0o600)
	r.wait(wire.ProviderCodex, wire.LoginDone)
}

func TestFlowExpires(t *testing.T) {
	old := FlowTimeout
	FlowTimeout = 700 * time.Millisecond
	defer func() { FlowTimeout = old }()
	r := newRig(t, "", codexDevice)
	if _, err := r.s.StartLogin(context.Background(), wire.ProviderCodex, wire.LoginDevice); err != nil {
		t.Fatal(err)
	}
	r.wait(wire.ProviderCodex, wire.LoginExpired)
}

// An API key goes to the CLI's standard input: not its arguments, not its environment.
func TestAPIKeyGoesOnStdinOnly(t *testing.T) {
	const key = "sk-" + "test-0000-abcdef123456"
	r := newRig(t, "", `echo "$@" > "$OUT/argv"; env > "$OUT/env"; cat > "$OUT/stdin"; echo "Logged in using an API key - $(cat "$OUT/stdin")"; exit 0`)
	if err := r.s.SaveAPIKey(context.Background(), wire.ProviderCodex, key); err != nil {
		t.Fatal(err)
	}
	if r.read("stdin") != key+"\n" {
		t.Fatalf("stdin %q", r.read("stdin"))
	}
	if strings.TrimSpace(r.read("argv")) != "login --with-api-key" || strings.Contains(r.read("env"), key) {
		t.Fatalf("argv %q", r.read("argv"))
	}
	select {
	case <-r.changed:
	case <-time.After(time.Second):
		t.Fatal("no change told")
	}
	for _, bad := range []string{"short", "has space in key", "line\nbreak-1234", "--flag-looking\x00"} {
		if err := r.s.SaveAPIKey(context.Background(), wire.ProviderCodex, bad); !errors.Is(err, wire.ErrLoginInput) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	rejecting := newRig(t, "", `cat >/dev/null; exit 1`)
	if err := rejecting.s.SaveAPIKey(context.Background(), wire.ProviderCodex, key); !errors.Is(err, wire.ErrLoginInput) {
		t.Fatalf("%v", err)
	}
}

func TestLogout(t *testing.T) {
	r := newRig(t, `echo "$@" > "$OUT/argv"`, `echo "$@" > "$OUT/argv"`)
	if err := r.s.Logout(context.Background(), wire.ProviderClaude); err != nil || strings.TrimSpace(r.read("argv")) != "auth logout" {
		t.Fatalf("%v %q", err, r.read("argv"))
	}
	if err := r.s.Logout(context.Background(), wire.ProviderCodex); err != nil || strings.TrimSpace(r.read("argv")) != "logout" {
		t.Fatalf("%v %q", err, r.read("argv"))
	}
}

// The CLI starts with the environment the service was given and nothing of the runtime's own.
func TestEnvironmentIsTheGivenOne(t *testing.T) {
	t.Setenv("RT_SECRET_TOKEN", "runtime-secret-0000")
	r := newRig(t, `env > "$OUT/env"; printf 'https://claude.com/cai/oauth/authorize?x=1\n'; sleep 5`, "")
	if _, err := r.s.StartLogin(context.Background(), wire.ProviderClaude, wire.LoginSubscription); err != nil {
		t.Fatal(err)
	}
	if env := r.read("env"); strings.Contains(env, "runtime-secret") || strings.Contains(env, "RT_SECRET_TOKEN") || !strings.Contains(env, "HOME=") {
		t.Fatalf("env %q", env)
	}
	_, _ = r.s.CancelLogin(context.Background(), wire.ProviderClaude)
}

func TestScan(t *testing.T) {
	const url = "https://claude.com/cai/oauth/authorize?code=true&client_id=x&state=s"
	for name, c := range map[string]struct {
		p    wire.Provider
		m    wire.LoginMode
		out  string
		want string
		code string
		ok   bool
	}{
		"terminal link":                 {wire.ProviderClaude, wire.LoginSubscription, "x \x1b]8;;" + url + "\x07\x1b[94m" + url + "\x1b[39m\x1b]8;;\x07\r\n", url, "", true},
		"plain link":                    {wire.ProviderClaude, wire.LoginConsole, "visit " + url + "\r\nPaste code", url, "", true},
		"other host":                    {wire.ProviderClaude, wire.LoginSubscription, "visit https://evil.example/a\n", "", "", false},
		"look-alike":                    {wire.ProviderClaude, wire.LoginSubscription, "visit https://claude.com.evil.example/a\n", "", "", false},
		"user-info":                     {wire.ProviderClaude, wire.LoginSubscription, "visit https://claude.com@evil.example/a\n", "", "", false},
		"terminal link to another host": {wire.ProviderClaude, wire.LoginSubscription, "\x1b]8;;https://evil.example/\x07" + url + "\x1b]8;;\x07", url, "", true},
		"device":                        {wire.ProviderCodex, wire.LoginDevice, "1. https://auth.openai.com/codex/device\n\n2. code\n   AB12-CD345\n\n", "https://auth.openai.com/codex/device", "AB12-CD345", true},
		"device no code":                {wire.ProviderCodex, wire.LoginDevice, "1. https://auth.openai.com/codex/device\n", "", "", false},
		"codex on claude host":          {wire.ProviderCodex, wire.LoginDevice, "https://claude.com/x\nAB12-CD345\n", "", "", false},
		"nothing":                       {wire.ProviderClaude, wire.LoginSubscription, "Opening browser\n", "", "", false},
	} {
		got, code, ok := Scan(c.p, c.m, []byte(c.out))
		if ok != c.ok || got != c.want || (code != nil && *code != c.code) || (code == nil && c.code != "") {
			t.Errorf("%s: %q %v %v", name, got, code, ok)
		}
	}
}
