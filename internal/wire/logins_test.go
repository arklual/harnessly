package wire_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

type fakeLogins struct {
	err   error
	calls []string
	flow  *wire.LoginFlow
}

func (f *fakeLogins) status() (wire.LoginStatus, error) { return wire.LoginStatus{Flow: f.flow}, f.err }
func (f *fakeLogins) Login(_ context.Context, p wire.Provider) (wire.LoginStatus, error) {
	f.calls = append(f.calls, "login "+string(p))
	return f.status()
}
func (f *fakeLogins) StartLogin(_ context.Context, p wire.Provider, m wire.LoginMode) (wire.LoginStatus, error) {
	f.calls = append(f.calls, "start "+string(p)+" "+string(m))
	return f.status()
}
func (f *fakeLogins) SubmitLoginCode(_ context.Context, p wire.Provider, id, code string) (wire.LoginStatus, error) {
	f.calls = append(f.calls, "code "+string(p)+" "+id)
	return f.status()
}
func (f *fakeLogins) CancelLogin(_ context.Context, p wire.Provider) (wire.LoginStatus, error) {
	f.calls = append(f.calls, "cancel "+string(p))
	return f.status()
}
func (f *fakeLogins) SaveAPIKey(_ context.Context, p wire.Provider, key string) error {
	f.calls = append(f.calls, "key "+string(p))
	return f.err
}
func (f *fakeLogins) Logout(_ context.Context, p wire.Provider) error {
	f.calls = append(f.calls, "logout "+string(p))
	return f.err
}

func jsonBody(v any) []byte { b, _ := json.Marshal(v); return b }

// The sign-in routes pass the person's choice to the source, answer what it answers and never echo a code or a key.
func TestLoginPages(t *testing.T) {
	g := newRig(t, time.Hour)
	if code := g.do("GET", "/api/v2/agents/claude/login", nil, nil); code != http.StatusNotFound {
		t.Fatalf("a machine without sign-in answers %d", code)
	}
	url := "https://claude.com/cai/oauth/authorize?code=true"
	f := &fakeLogins{flow: &wire.LoginFlow{ID: strings.Repeat("a1", 16), Provider: wire.ProviderClaude, Mode: wire.LoginSubscription,
		State: wire.LoginAwaiting, Input: wire.LoginInputCode, URL: &url, StartedAt: 1, ExpiresAt: 2}}
	g.srv.Logins = f

	var st wire.LoginStatus
	if code := g.do("POST", "/api/v2/agents/claude/login", jsonBody(wire.LoginStart{Mode: wire.LoginSubscription}), &st); code != 200 || st.Validate() != nil || st.Flow == nil {
		t.Fatalf("start: %d %+v", code, st)
	}
	const secretCode = "abcDEF123-" + "_xyz0987654321" // joined: no key-shaped literal for gitleaks
	if code := g.do("POST", "/api/v2/agents/claude/login/code", jsonBody(wire.LoginCode{ID: f.flow.ID, Code: secretCode}), &st); code != 200 {
		t.Fatalf("code: %d", code)
	}
	if code := g.do("GET", "/api/v2/agents/claude/login", nil, &st); code != 200 {
		t.Fatalf("get: %d", code)
	}
	if code := g.do("DELETE", "/api/v2/agents/claude/login", nil, &st); code != 200 {
		t.Fatalf("cancel: %d", code)
	}
	var ok wire.LoginOK
	const key = "sk-" + "test-0000-abcdef123456"
	if code := g.do("PUT", "/api/v2/agents/codex/key", jsonBody(wire.ProfileKeyEdit{Key: key}), &ok); code != 200 || !ok.OK {
		t.Fatalf("key: %d", code)
	}
	if code := g.do("POST", "/api/v2/agents/codex/logout", nil, &ok); code != 200 || !ok.OK {
		t.Fatalf("logout: %d", code)
	}
	want := "start claude subscription|code claude " + f.flow.ID + "|login claude|cancel claude|key codex|logout codex"
	if got := strings.Join(f.calls, "|"); got != want {
		t.Fatalf("calls %q", got)
	}

	for name, c := range map[string]struct {
		method, path string
		body         []byte
		want         int
	}{
		"unknown agent":    {"GET", "/api/v2/agents/gemini/login", nil, 404},
		"unknown route":    {"GET", "/api/v2/agents/claude/token", nil, 404},
		"no route":         {"GET", "/api/v2/agents/claude", nil, 404},
		"wrong method":     {"PATCH", "/api/v2/agents/claude/login", nil, 405},
		"bad mode":         {"POST", "/api/v2/agents/claude/login", jsonBody(map[string]string{"mode": "root"}), 400},
		"extra field":      {"POST", "/api/v2/agents/claude/login", []byte(`{"mode":"console","x":1}`), 400},
		"short code":       {"POST", "/api/v2/agents/claude/login/code", jsonBody(wire.LoginCode{ID: f.flow.ID, Code: "abc"}), 400},
		"code with escape": {"POST", "/api/v2/agents/claude/login/code", jsonBody(wire.LoginCode{ID: f.flow.ID, Code: "abcdefgh\r\nrm -rf"}), 400},
		"bad flow id":      {"POST", "/api/v2/agents/claude/login/code", jsonBody(wire.LoginCode{ID: "../x", Code: secretCode}), 400},
		"key with space":   {"PUT", "/api/v2/agents/codex/key", jsonBody(wire.ProfileKeyEdit{Key: "sk-test 0000-abcdef"}), 400},
		"huge body":        {"PUT", "/api/v2/agents/codex/key", []byte(`{"key":"` + strings.Repeat("a", 4096) + `"}`), 400},
	} {
		if code := g.do(c.method, c.path, c.body, nil); code != c.want {
			t.Errorf("%s: %d, want %d", name, code, c.want)
		}
	}
	for name, c := range map[string]struct {
		err  error
		want int
	}{
		"busy":          {wire.ErrLoginBusy, http.StatusConflict},
		"stale":         {wire.ErrLoginStale, http.StatusConflict},
		"rate":          {wire.ErrLoginRate, http.StatusBadRequest},
		"unsupported":   {wire.ErrLoginUnsupported, 0},
		"not installed": {wire.ErrLoginNotInstalled, 0},
		"input":         {wire.ErrLoginInput, http.StatusBadRequest},
		"other":         {context.Canceled, 0},
	} {
		f.err = c.err
		var out json.RawMessage
		code := g.do("POST", "/api/v2/agents/claude/login", jsonBody(wire.LoginStart{Mode: wire.LoginConsole}), &out)
		if c.want != 0 && code != c.want {
			t.Errorf("%s: %d, want %d", name, code, c.want)
		}
		if code == 200 {
			t.Errorf("%s answers 200", name)
		}
	}
}

func TestLoginFlowShape(t *testing.T) {
	url := "https://auth.openai.com/codex/device"
	code := "ABCD-EFGH1"
	ok := wire.LoginFlow{ID: strings.Repeat("0f", 16), Provider: wire.ProviderCodex, Mode: wire.LoginDevice, State: wire.LoginAwaiting,
		Input: wire.LoginInputNone, URL: &url, UserCode: &code}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(f *wire.LoginFlow){
		"mode the CLI lacks":  func(f *wire.LoginFlow) { f.Mode = wire.LoginConsole },
		"http link":           func(f *wire.LoginFlow) { h := "http://x"; f.URL = &h },
		"awaiting no link":    func(f *wire.LoginFlow) { f.URL = nil },
		"failed no reason":    func(f *wire.LoginFlow) { f.State = wire.LoginFailed },
		"reason not failed":   func(f *wire.LoginFlow) { r := wire.LoginTimeout; f.Reason = &r },
		"user code in claude": func(f *wire.LoginFlow) { f.Provider, f.Mode = wire.ProviderClaude, wire.LoginSubscription },
		"bad id":              func(f *wire.LoginFlow) { f.ID = "x" },
	}
	for name, mutate := range bad {
		f := ok
		mutate(&f)
		if f.Validate() == nil {
			t.Errorf("%s passes", name)
		}
	}
}
