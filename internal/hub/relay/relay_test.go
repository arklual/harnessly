package relay_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/api"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

type fixture struct {
	s       *hubtest.Stack
	owner   *hubtest.Person
	spaceID string
	a       *hubtest.Agent
	base    string
}

func setup(t *testing.T, opts ...hubtest.Option) *fixture {
	t.Helper()
	s := hubtest.New(t, opts...)
	owner := s.SignIn("owner@example.com")
	id, _ := owner.NewSpace("Команда")
	a := owner.Machine(id, nil)
	return &fixture{s: s, owner: owner, spaceID: id, a: a, base: "/api/machines/" + a.MachineID}
}

func (f *fixture) seal(t *testing.T, purpose, secret string) hubproto.Envelope {
	t.Helper()
	e, err := hubproto.Seal(f.a.Enc.PublicKey().Bytes(), f.a.MachineID, purpose, []byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

func param(c hubtest.Call, key string) any {
	var v any
	_ = json.Unmarshal(c.Params[key], &v)
	return v
}

func TestAgentsListCachedAndRefreshed(t *testing.T) {
	f := setup(t)
	before := len(f.a.Calls("agents.status"))
	r := f.owner.Do("GET", f.base+"/agents", nil)
	var out struct{ Agents []struct{ Provider string } }
	r.JSON(&out)
	if r.Code != 200 || len(out.Agents) != 3 || out.Agents[2].Provider != "gemini" {
		t.Fatalf("agents: %s", r)
	}
	if n := len(f.a.Calls("agents.status")); n != before {
		t.Errorf("the cached list asked the agent (%d calls, had %d)", n, before)
	}
	f.a.Handle("agents.status", func(hubtest.Call) (any, error) {
		return map[string]any{"agents": []hubproto.AgentStatus{{Provider: "claude", Installed: true, Login: hubproto.LoggedOut}, {Provider: "codex", Installed: true, Login: hubproto.LoggedIn}}}, nil
	})
	r = f.owner.Do("GET", f.base+"/agents?refresh=1", nil)
	var fresh struct {
		Agents []struct {
			Provider  string
			Installed bool
			Login     string
		}
	}
	r.JSON(&fresh)
	if r.Code != 200 || len(fresh.Agents) != 3 || fresh.Agents[0].Login != "logged_out" || fresh.Agents[1].Login != "logged_in" || !fresh.Agents[1].Installed {
		t.Fatalf("refresh: %s", r)
	}
	if n := len(f.a.Calls("agents.status")); n != before+1 {
		t.Errorf("refresh made %d calls", n-before)
	}
	// And what it learned is what the cached list now says.
	r = f.owner.Do("GET", f.base+"/agents", nil)
	var cached struct {
		Agents []struct{ Provider, Login string }
	}
	r.JSON(&cached)
	if len(cached.Agents) != 3 || cached.Agents[1].Provider != "codex" || cached.Agents[1].Login != "logged_in" {
		t.Errorf("not remembered: %s", r)
	}
}

func TestInstall(t *testing.T) {
	f := setup(t)
	f.a.Handle("agents.install", func(c hubtest.Call) (any, error) { return map[string]any{"jobId": param(c, "jobId")}, nil })

	r := f.owner.Do("POST", f.base+"/agents/claude/install", map[string]any{})
	var out struct{ JobID string }
	r.JSON(&out)
	if r.Code != 202 || !hex32.MatchString(out.JobID) {
		t.Fatalf("install: %s", r)
	}
	c := f.a.Calls("agents.install")[0]
	if param(c, "jobId") != out.JobID || param(c, "provider") != "claude" || param(c, "from") != nil {
		t.Errorf("call = %v", c.Params)
	}

	r = f.owner.Do("POST", f.base+"/agents/codex/install", map[string]any{"from": "provider"})
	if r.Code != 202 {
		t.Fatalf("from provider: %s", r)
	}
	if c := f.a.Calls("agents.install")[1]; param(c, "from") != "provider" || param(c, "provider") != "codex" {
		t.Errorf("call = %v", c.Params)
	}

	if r := f.owner.Do("POST", f.base+"/agents/claude/install", map[string]any{"from": "download"}); r.Code != 400 || r.Detail("field") != "from" {
		t.Errorf("another start step: %s", r)
	}
	if r := f.owner.Do("POST", f.base+"/agents/gemini/install", map[string]any{}); r.Code != 422 || r.Err() != "provider_off" {
		t.Errorf("gemini: %s", r)
	}
	if r := f.owner.Do("POST", f.base+"/agents/cursor/install", map[string]any{}); r.Code != 404 {
		t.Errorf("unknown provider: %s", r)
	}
	if n := len(f.a.Calls("agents.install")); n != 2 {
		t.Errorf("the agent got %d installs, want 2", n)
	}
}

func TestProviderCheck(t *testing.T) {
	f := setup(t)
	f.a.Handle("provider.check", func(c hubtest.Call) (any, error) {
		via := "direct"
		if param(c, "proxyUrl") != nil {
			via = "proxy"
		}
		status := 401
		return hubproto.ProviderCheck{Provider: c.Str("provider"), Verdict: "ok", At: f.s.Clock.Now().UnixMilli(), Via: via, Host: "api.anthropic.com", Status: &status}, nil
	})
	r := f.owner.Do("POST", f.base+"/agents/claude/check", map[string]any{})
	var out struct{ Check map[string]any }
	r.JSON(&out)
	if r.Code != 200 || out.Check["verdict"] != "ok" || out.Check["via"] != "direct" {
		t.Fatalf("check: %s", r)
	}
	if at, ok := out.Check["at"].(string); !ok || at != f.s.Clock.Now().UTC().Format(time.RFC3339) {
		t.Errorf("at = %v: times are RFC 3339 strings", out.Check["at"])
	}

	r = f.owner.Do("POST", f.base+"/agents/codex/check", map[string]any{"proxyUrl": "socks5://u:p@10.0.0.1:1080"})
	r.JSON(&out)
	if r.Code != 200 || out.Check["via"] != "proxy" {
		t.Fatalf("check through a proxy: %s", r)
	}
	if got := param(f.a.Calls("provider.check")[1], "proxyUrl"); got != "socks5://u:p@10.0.0.1:1080" {
		t.Errorf("the agent got proxyUrl %v", got)
	}
	if r := f.owner.Do("POST", f.base+"/agents/codex/check", map[string]any{"proxyUrl": "ftp://10.0.0.1"}); r.Code != 400 || r.Detail("field") != "proxyUrl" {
		t.Errorf("bad proxy: %s", r)
	}
	if r := f.owner.Do("POST", f.base+"/agents/gemini/check", map[string]any{}); r.Code != 422 {
		t.Errorf("gemini: %s", r)
	}
}

func TestLoginFlow(t *testing.T) {
	f := setup(t)
	f.a.Handle("login.start", func(c hubtest.Call) (any, error) {
		return hubproto.Login{ID: c.Str("loginId"), Provider: c.Str("provider"), Method: c.Str("method"), State: hubproto.LoginStarting}, nil
	})
	r := f.owner.Do("POST", f.base+"/agents/claude/login", map[string]any{"method": "link"})
	var out struct{ Login map[string]any }
	r.JSON(&out)
	id, _ := out.Login["id"].(string)
	if r.Code != 202 || !hex32.MatchString(id) || out.Login["state"] != "starting" || out.Login["provider"] != "claude" {
		t.Fatalf("login: %s", r)
	}
	if c := f.a.Calls("login.start")[0]; param(c, "loginId") != id || param(c, "method") != "link" {
		t.Errorf("call = %v", c.Params)
	}

	if r := f.owner.Do("POST", f.base+"/agents/claude/login", map[string]any{"method": "magic"}); r.Code != 400 {
		t.Errorf("bad method: %s", r)
	}
	if r := f.owner.Do("POST", f.base+"/agents/gemini/login", map[string]any{"method": "link"}); r.Code != 422 {
		t.Errorf("gemini: %s", r)
	}

	// The code is passed on and not kept.
	f.a.Handle("login.code", func(hubtest.Call) (any, error) { return map[string]any{}, nil })
	if r := f.owner.Do("POST", f.base+"/logins/"+id+"/code", map[string]any{"code": "  abc123#xyz  "}); r.Code != 204 {
		t.Fatalf("code: %s", r)
	}
	if c := f.a.Calls("login.code")[0]; param(c, "loginId") != id || param(c, "code") != "abc123#xyz" {
		t.Errorf("call = %v", c.Params)
	}
	for name, body := range map[string]map[string]any{"empty": {"code": " "}, "too long": {"code": strings.Repeat("x", 1025)}} {
		if r := f.owner.Do("POST", f.base+"/logins/"+id+"/code", body); r.Code != 400 {
			t.Errorf("%s code: %s", name, r)
		}
	}
	if r := f.owner.Do("POST", f.base+"/logins/not-hex/code", map[string]any{"code": "x"}); r.Code != 404 {
		t.Errorf("bad login id: %s", r)
	}

	f.a.Handle("login.cancel", func(hubtest.Call) (any, error) { return map[string]any{}, nil })
	if r := f.owner.Do("DELETE", f.base+"/logins/"+id, nil); r.Code != 204 {
		t.Errorf("cancel: %s", r)
	}
	if n := len(f.a.Calls("login.cancel")); n != 1 {
		t.Errorf("cancel calls = %d", n)
	}
}

func TestLoginLinkFromTheMachineMustBeTheProvidersPage(t *testing.T) {
	f := setup(t)
	evil := "https://evil.example.net/oauth/authorize"
	f.a.Handle("login.start", func(c hubtest.Call) (any, error) {
		return hubproto.Login{ID: c.Str("loginId"), Provider: c.Str("provider"), Method: c.Str("method"), State: hubproto.LoginURL, URL: &evil, NeedsCode: true}, nil
	})
	r := f.owner.Do("POST", f.base+"/agents/claude/login", map[string]any{"method": "link"})
	var out struct{ Login map[string]any }
	r.JSON(&out)
	if r.Code != 202 || out.Login["url"] != nil || out.Login["state"] != "failed" || out.Login["error"] == nil {
		t.Fatalf("the hub passed a foreign link on: %s", r)
	}
	if strings.Contains(r.String(), "evil.example.net") {
		t.Errorf("the answer still mentions the machine's address: %s", r)
	}

	good := "https://claude.ai/oauth/authorize?code=true"
	f.a.Handle("login.start", func(c hubtest.Call) (any, error) {
		return hubproto.Login{ID: c.Str("loginId"), Provider: c.Str("provider"), Method: c.Str("method"), State: hubproto.LoginURL, URL: &good, NeedsCode: true}, nil
	})
	r = f.owner.Do("POST", f.base+"/agents/claude/login", map[string]any{"method": "link"})
	r.JSON(&out)
	if r.Code != 202 || out.Login["url"] != good || out.Login["state"] != "url" {
		t.Errorf("the provider's own link was lost: %s", r)
	}
}

func TestSecondLoginIsRefusedWhileOneRuns(t *testing.T) {
	f := setup(t)
	f.a.Handle("login.start", func(hubtest.Call) (any, error) {
		return nil, hubtest.Fail(hubproto.CodeBusy, "a login is already running")
	})
	r := f.owner.Do("POST", f.base+"/agents/codex/login", map[string]any{"method": "console"})
	if r.Code != http.StatusConflict || r.Err() != "conflict" || r.Detail("reason") != "login_running" {
		t.Errorf("busy: %s", r)
	}
}

func TestKeyDelivery(t *testing.T) {
	f := setup(t)
	var seen hubproto.Envelope
	f.a.Handle("key.deliver", func(c hubtest.Call) (any, error) {
		_ = json.Unmarshal(c.Params["envelope"], &seen)
		return hubproto.KeyCheck{Provider: c.Str("provider"), Present: true, Verdict: hubproto.KeyRejected, Saved: true}, nil
	})
	env := f.seal(t, "key:claude", "sk-ant-test-key")
	r := f.owner.Do("POST", f.base+"/agents/claude/key", map[string]any{"envelope": env})
	var out struct{ Check hubproto.KeyCheck }
	r.JSON(&out)
	if r.Code != 200 || out.Check.Verdict != "rejected" || !out.Check.Saved || out.Check.Provider != "claude" {
		t.Fatalf("key: %s", r)
	}
	if seen != env {
		t.Errorf("the envelope changed on the way: %+v vs %+v", seen, env)
	}
	// The agent can open it: the hub passed it through untouched.
	plain, err := hubproto.Open(f.a.Enc.Bytes(), f.a.MachineID, "key:claude", seen)
	if err != nil || string(plain) != "sk-ant-test-key" {
		t.Errorf("open = %q %v", plain, err)
	}

	f.a.Handle("key.verify", func(c hubtest.Call) (any, error) {
		return hubproto.KeyCheck{Provider: c.Str("provider"), Present: true, Verdict: hubproto.KeyAccepted}, nil
	})
	r = f.owner.Do("POST", f.base+"/agents/codex/key/verify", map[string]any{})
	r.JSON(&out)
	if r.Code != 200 || out.Check.Verdict != "accepted" || out.Check.Provider != "codex" {
		t.Errorf("verify: %s", r)
	}
	if r := f.owner.Do("POST", f.base+"/agents/gemini/key/verify", map[string]any{}); r.Code != 422 {
		t.Errorf("gemini: %s", r)
	}
}

func TestEnvelopeShapeIsCheckedBeforeItTravels(t *testing.T) {
	f := setup(t)
	good := f.seal(t, "key:claude", "k")
	cases := map[string]func(e *hubproto.Envelope){
		"version":        func(e *hubproto.Envelope) { e.V = 2 },
		"epk not base64": func(e *hubproto.Envelope) { e.Epk = "!!!" },
		"epk too short":  func(e *hubproto.Envelope) { e.Epk = "AAAA" },
		"nonce":          func(e *hubproto.Envelope) { e.Nonce = "AAAA" },
		"empty ct":       func(e *hubproto.Envelope) { e.Ct = "" },
		"huge ct":        func(e *hubproto.Envelope) { e.Ct = strings.Repeat("QUFB", 4000) },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			e := good
			edit(&e)
			r := f.owner.Do("POST", f.base+"/agents/claude/key", map[string]any{"envelope": e})
			if r.Code != 400 || r.Detail("field") != "envelope" {
				t.Errorf("%s", r)
			}
			r = f.owner.Do("POST", f.base+"/env", map[string]any{"name": "GH_TOKEN", "envelope": e})
			if r.Code != 400 || r.Detail("field") != "envelope" {
				t.Errorf("env: %s", r)
			}
		})
	}
	if n := len(f.a.Calls("key.deliver")) + len(f.a.Calls("env.deliver")); n != 0 {
		t.Errorf("the agent received %d malformed envelopes", n)
	}
}

func TestProxy(t *testing.T) {
	f := setup(t)
	f.a.Handle("proxy.set", func(c hubtest.Call) (any, error) {
		sum := hubproto.ProxySummary{}
		e := &hubproto.ProxyEntry{Scheme: "http", Host: "10.0.0.1:3128"}
		if c.Str("scope") == "claude" {
			sum.Claude = e
		} else {
			sum.All = e
		}
		return sum, nil
	})
	f.a.Handle("proxy.clear", func(c hubtest.Call) (any, error) { return hubproto.ProxySummary{}, nil })

	r := f.owner.Do("PUT", f.base+"/proxy", map[string]any{"scope": "claude", "url": "http://user:pass@10.0.0.1:3128"})
	if r.Code != 200 || !strings.Contains(string(r.Body), `"claude":{"scheme":"http","host":"10.0.0.1:3128","bridge":null}`) {
		t.Fatalf("set: %s", r)
	}
	if c := f.a.Calls("proxy.set")[0]; param(c, "scope") != "claude" || param(c, "url") != "http://user:pass@10.0.0.1:3128" {
		t.Errorf("call = %v", c.Params)
	}
	// What the machine says about its proxy is remembered for the page, without credentials.
	var got struct {
		Machine struct{ Proxy map[string]any }
	}
	f.owner.Do("GET", f.base, nil).JSON(&got)
	if got.Machine.Proxy["claude"] == nil || got.Machine.Proxy["all"] != nil {
		t.Errorf("machine proxy = %v", got.Machine.Proxy)
	}

	f.a.Handle("proxy.get", func(hubtest.Call) (any, error) {
		return hubproto.ProxySummary{All: &hubproto.ProxyEntry{Scheme: "socks5", Host: "10.0.0.2:1080", Bridge: ptr("127.0.0.1:41000")}}, nil
	})
	r = f.owner.Do("GET", f.base+"/proxy", nil)
	if r.Code != 200 || !strings.Contains(string(r.Body), `"bridge":"127.0.0.1:41000"`) {
		t.Errorf("get: %s", r)
	}

	r = f.owner.Do("DELETE", f.base+"/proxy?scope=claude", nil)
	if r.Code != 200 || !strings.Contains(string(r.Body), `"all":null,"claude":null`) {
		t.Errorf("clear: %s", r)
	}
	if c := f.a.Calls("proxy.clear")[0]; param(c, "scope") != "claude" {
		t.Errorf("call = %v", c.Params)
	}

	bad := []struct {
		name string
		body map[string]any
		want string
	}{
		{"scope", map[string]any{"scope": "gemini", "url": "http://h:1"}, "scope"},
		{"no scope", map[string]any{"url": "http://h:1"}, "scope"},
		{"no url", map[string]any{"scope": "all"}, "url"},
		{"scheme", map[string]any{"scope": "all", "url": "ftp://h:1"}, "url"},
		{"no host", map[string]any{"scope": "all", "url": "http://"}, "url"},
		{"not a url", map[string]any{"scope": "all", "url": "just words"}, "url"},
		{"too long", map[string]any{"scope": "all", "url": "http://h:1/" + strings.Repeat("a", 2100)}, "url"},
	}
	for _, c := range bad {
		if r := f.owner.Do("PUT", f.base+"/proxy", c.body); r.Code != 400 || r.Detail("field") != c.want {
			t.Errorf("%s: %s", c.name, r)
		}
	}
	for _, scheme := range []string{"http", "https", "socks5", "socks5h"} {
		if r := f.owner.Do("PUT", f.base+"/proxy", map[string]any{"scope": "all", "url": scheme + "://h:1"}); r.Code != 200 {
			t.Errorf("%s: %s", scheme, r)
		}
	}
	if r := f.owner.Do("DELETE", f.base+"/proxy", nil); r.Code != 400 {
		t.Errorf("clear without a scope: %s", r)
	}
}

func ptr[T any](v T) *T { return &v }

func TestSecrets(t *testing.T) {
	f := setup(t)
	stream := f.owner.Events(f.spaceID)
	f.a.Handle("env.deliver", func(c hubtest.Call) (any, error) {
		return map[string]any{"env": []string{"ZED", "GH_TOKEN"}}, nil
	})
	f.a.Handle("env.remove", func(c hubtest.Call) (any, error) { return map[string]any{"env": []string{"ZED"}}, nil })

	r := f.owner.Do("POST", f.base+"/env", map[string]any{"name": "GH_TOKEN", "envelope": f.seal(t, "env:GH_TOKEN", "ghp_x")})
	if r.Code != 200 || strings.TrimSpace(string(r.Body)) != `{"env":["GH_TOKEN","ZED"]}` {
		t.Fatalf("deliver: %s", r)
	}
	if c := f.a.Calls("env.deliver")[0]; param(c, "name") != "GH_TOKEN" {
		t.Errorf("call = %v", c.Params)
	}
	ev := stream.Await("the machine with its secrets", func(e hubtest.Event) bool {
		return e.Name == "machine" && strings.Contains(string(e.Data), `"env":["GH_TOKEN","ZED"]`)
	})
	if strings.Contains(string(ev.Data), "ghp_x") {
		t.Error("the secret is in the event")
	}
	names, err := f.s.DB.S().EnvNames(context.Background(), f.a.MachineID)
	if err != nil || strings.Join(names, ",") != "GH_TOKEN,ZED" {
		t.Errorf("recorded names = %v %v", names, err)
	}

	r = f.owner.Do("DELETE", f.base+"/env/GH_TOKEN", nil)
	if r.Code != 200 || !strings.Contains(string(r.Body), `["ZED"]`) {
		t.Errorf("remove: %s", r)
	}
	if names, _ := f.s.DB.S().EnvNames(context.Background(), f.a.MachineID); strings.Join(names, ",") != "ZED" {
		t.Errorf("names after remove = %v", names)
	}
	if r := f.owner.Do("DELETE", f.base+"/env/lower_case", nil); r.Code != 404 {
		t.Errorf("remove a bad name: %s", r)
	}
}

func TestEnvNames(t *testing.T) {
	f := setup(t)
	f.a.Handle("env.deliver", func(c hubtest.Call) (any, error) { return map[string]any{"env": []string{c.Str("name")}}, nil })
	env := f.seal(t, "env:X", "v")
	ok := []string{"GH_TOKEN", "GITLAB_TOKEN", "_X_TOKEN", "MY_SECRET", "DB_PASSWORD", "SENTRY_DSN", "DB_USER", strings.Repeat("A", 58) + "_TOKEN"}
	refused := []string{
		"", "gh_token", "1ABC", "A-B", "A B", "A.B", strings.Repeat("A", 65), "PATH", "HOME", "SHELL", "USER",
		"LD_PRELOAD", "LD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
		"ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL", "OPENAI_API_KEY", "HARNESSLY_HUB_URL", "RUNTIME_GATE_LISTEN", "AGENT_PORT",
		// The machine's allowlist: a name that does not end like a secret or an address, or that changes how a program runs.
		"A", "_UNDERSCORE", "MY_SECRET_2", "LDX", "PATHS", "HOMEWORK", "AGENT", "DEBUG", "NODE_ENV", "TOKEN",
		"GIT_SSH_COMMAND", "GIT_TOKEN", "PYTHONPATH", "NODE_OPTIONS", "BASH_ENV", "PAGER", "SSL_CERT_FILE", "FTP_PROXY", "AWS_CA_KEY",
		"CLAUDE_CONFIG_DIR", "CODEX_API_KEY",
		// An address is not a secret, and what points a package manager or a CLI at another server is not set from here.
		"GITLAB_HOST", "REGISTRY_URL", "GH_HOST", "PIP_INDEX_URL", "NPM_CONFIG_REGISTRY", "UV_INDEX_URL", "DOCKER_HOST", "GOPROXY", "HF_ENDPOINT",
	}
	for _, name := range ok {
		if r := f.owner.Do("POST", f.base+"/env", map[string]any{"name": name, "envelope": env}); r.Code != 200 {
			t.Errorf("%q: %s", name, r)
		}
	}
	for _, name := range refused {
		if r := f.owner.Do("POST", f.base+"/env", map[string]any{"name": name, "envelope": env}); r.Code != 400 || r.Detail("field") != "name" {
			t.Errorf("%q: %s", name, r)
		}
	}
	if n := len(f.a.Calls("env.deliver")); n != len(ok) {
		t.Errorf("the agent got %d deliveries, want %d", n, len(ok))
	}
}

func TestGitkey(t *testing.T) {
	f := setup(t)
	f.a.Handle("gitkey.ensure", func(hubtest.Call) (any, error) {
		return map[string]string{"publicKey": "ssh-ed25519 AAAAC3 harnessly@dev-box"}, nil
	})
	r := f.owner.Do("POST", f.base+"/gitkey", map[string]any{})
	if r.Code != 200 || !strings.Contains(string(r.Body), `"publicKey":"ssh-ed25519 AAAAC3 harnessly@dev-box"`) {
		t.Errorf("gitkey: %s", r)
	}
}

// ---- who may, and what the agent can answer ----

func TestEveryEndpointIsForOwners(t *testing.T) {
	f := setup(t)
	member := f.s.SignIn("member@example.com")
	f.owner.Join(f.spaceID, member)
	stranger := f.s.SignIn("stranger@example.com")
	anonymous := f.s.Browser()
	env := f.seal(t, "key:claude", "k")
	id := strings.Repeat("a", 32)

	type call struct {
		method, path string
		body         any
	}
	calls := []call{
		{"GET", "/agents", nil},
		{"POST", "/agents/claude/install", map[string]any{}},
		{"POST", "/agents/claude/check", map[string]any{}},
		{"POST", "/agents/claude/login", map[string]any{"method": "link"}},
		{"POST", "/logins/" + id + "/code", map[string]any{"code": "x"}},
		{"DELETE", "/logins/" + id, nil},
		{"POST", "/agents/claude/key", map[string]any{"envelope": env}},
		{"POST", "/agents/claude/key/verify", map[string]any{}},
		{"GET", "/proxy", nil},
		{"PUT", "/proxy", map[string]any{"scope": "all", "url": "http://h:1"}},
		{"DELETE", "/proxy?scope=all", nil},
		{"POST", "/env", map[string]any{"name": "GH_TOKEN", "envelope": env}},
		{"DELETE", "/env/GH_TOKEN", nil},
		{"POST", "/gitkey", map[string]any{}},
	}
	for _, c := range calls {
		for who, want := range map[string]struct {
			p    *hubtest.Person
			code int
		}{"member": {member, 403}, "stranger": {stranger, 403}, "anonymous": {anonymous, 401}} {
			if r := want.p.Do(c.method, f.base+c.path, c.body); r.Code != want.code {
				t.Errorf("%s %s as %s: %s", c.method, c.path, who, r)
			}
		}
	}
	if n := len(f.a.Calls("")); n > 5 {
		t.Errorf("the agent was asked %d times by people who may not ask", n)
	}
	for _, op := range []string{"agents.install", "provider.check", "login.start", "login.code", "login.cancel", "key.deliver", "key.verify", "proxy.set", "proxy.clear", "env.deliver", "env.remove", "gitkey.ensure"} {
		if n := len(f.a.Calls(op)); n != 0 {
			t.Errorf("%s reached the agent %d times", op, n)
		}
	}
}

func TestAgentErrorsBecomeTheContractsAnswers(t *testing.T) {
	f := setup(t)
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"policy", hubtest.Fail(hubproto.CodePolicyDenied, "keys are off here"), 403, "machine_policy"},
		{"unsupported", hubtest.Fail(hubproto.CodeUnsupported, "no such op"), 422, "provider_off"},
		{"busy", hubtest.Fail(hubproto.CodeBusy, "busy"), 409, "conflict"},
		{"not found", hubtest.Fail(hubproto.CodeNotFound, "no such job"), 404, "not_found"},
		{"invalid", hubtest.Fail(hubproto.CodeInvalid, "bad envelope"), 400, "invalid_request"},
		{"failed", hubtest.Fail(hubproto.CodeFailed, "disk full"), 502, "machine_error"},
		{"no code", hubtest.ErrUnexpected, 502, "machine_error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f.a.Handle("gitkey.ensure", func(hubtest.Call) (any, error) { return nil, c.err })
			r := f.owner.Do("POST", f.base+"/gitkey", map[string]any{})
			if r.Code != c.status || r.Err() != c.code {
				t.Fatalf("%s", r)
			}
			if c.code == "machine_policy" && r.Detail("op") != "gitkey.ensure" {
				t.Errorf("details = %s", r.Body)
			}
			if c.code == "machine_error" && r.Detail("agentError") == nil {
				t.Errorf("no agentError in %s", r.Body)
			}
		})
	}
}

func TestOfflineMachine(t *testing.T) {
	f := setup(t)
	f.a.Close()
	f.s.Eventually("the link to go", func() bool {
		var got struct{ Machine struct{ Status string } }
		f.owner.Do("GET", f.base, nil).JSON(&got)
		return got.Machine.Status == "offline"
	})
	// The cached list still answers; everything that needs the machine says it is away.
	if r := f.owner.Do("GET", f.base+"/agents", nil); r.Code != 200 {
		t.Errorf("cached list: %s", r)
	}
	for _, c := range []struct{ method, path string }{
		{"GET", "/agents?refresh=1"}, {"POST", "/agents/claude/check"}, {"GET", "/proxy"}, {"POST", "/gitkey"},
	} {
		r := f.owner.Do(c.method, f.base+c.path, map[string]any{})
		if r.Code != 502 || r.Err() != "machine_offline" {
			t.Errorf("%s %s: %s", c.method, c.path, r)
		}
	}
}

func TestAgentThatDoesNotAnswer(t *testing.T) {
	f := setup(t, hubtest.WithOptions(func(o *api.Options) { o.AgentTimeout = 300 * time.Millisecond }))
	release := make(chan struct{})
	defer close(release)
	f.a.Handle("gitkey.ensure", func(hubtest.Call) (any, error) { <-release; return nil, nil })
	start := time.Now()
	r := f.owner.Do("POST", f.base+"/gitkey", map[string]any{})
	if r.Code != http.StatusGatewayTimeout || r.Err() != "machine_timeout" {
		t.Errorf("%s", r)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("answered after %v", d)
	}
}

func TestRevokedMachine(t *testing.T) {
	f := setup(t)
	f.owner.Do("DELETE", f.base, nil)
	if r := f.owner.Do("POST", f.base+"/gitkey", map[string]any{}); r.Code != 404 {
		t.Errorf("revoked: %s", r)
	}
}

// ---- nothing secret stays ----

// dump is every value of every table, as text.
func dump(t *testing.T, s *hubtest.Stack) string {
	t.Helper()
	ctx := context.Background()
	// The tables of the database in use: SQLite's catalog, or the schema this test's Postgres connections are set to.
	list := `SELECT name FROM sqlite_master WHERE type = 'table'`
	if s.DB.Driver() == store.Postgres {
		list = `SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'`
	}
	rows, err := s.DB.Query(ctx, list)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		tables = append(tables, n)
	}
	rows.Close()
	var b strings.Builder
	for _, tb := range tables {
		rs, err := s.DB.Query(ctx, `SELECT * FROM `+tb)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rs.Columns()
		for rs.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			_ = rs.Scan(ptrs...)
			for _, v := range vals {
				switch x := v.(type) {
				case []byte:
					b.Write(x)
				case string:
					b.WriteString(x)
				}
				b.WriteByte('|')
			}
			b.WriteByte('\n')
		}
		rs.Close()
	}
	return b.String()
}

func TestSecretsAreNeverLoggedOrStored(t *testing.T) {
	f := setup(t)
	f.a.Handle("key.deliver", func(c hubtest.Call) (any, error) {
		return hubproto.KeyCheck{Provider: "claude", Present: true, Verdict: "accepted", Saved: true}, nil
	})
	f.a.Handle("env.deliver", func(c hubtest.Call) (any, error) { return map[string]any{"env": []string{"GH_TOKEN"}}, nil })
	f.a.Handle("proxy.set", func(c hubtest.Call) (any, error) {
		return hubproto.ProxySummary{All: &hubproto.ProxyEntry{Scheme: "http", Host: "10.0.0.1:3128"}}, nil
	})
	f.a.Handle("login.code", func(c hubtest.Call) (any, error) { return map[string]any{}, nil })
	f.a.Handle("provider.check", func(c hubtest.Call) (any, error) {
		return hubproto.ProviderCheck{Provider: "claude", Verdict: "ok", Via: "proxy", Host: "api.anthropic.com"}, nil
	})

	const (
		keySecret   = "KEYSECRET-sk-ant-0123456789"
		envSecret   = "ENVSECRET-ghp-0123456789"
		proxyPass   = "PROXYPASS-hunter2"
		loginSecret = "LOGINCODE-abc-0123456789"
	)
	loginID := strings.Repeat("c", 32)
	// The sealed text itself is ciphertext; what must never appear is the plaintext, and the ciphertext too, since the
	// hub has no business keeping it.
	keyEnv := f.seal(t, "key:claude", keySecret)
	envEnv := f.seal(t, "env:GH_TOKEN", envSecret)

	f.owner.Do("POST", f.base+"/agents/claude/key", map[string]any{"envelope": keyEnv})
	f.owner.Do("POST", f.base+"/env", map[string]any{"name": "GH_TOKEN", "envelope": envEnv})
	f.owner.Do("PUT", f.base+"/proxy", map[string]any{"scope": "all", "url": "http://user:" + proxyPass + "@10.0.0.1:3128"})
	f.owner.Do("POST", f.base+"/agents/claude/check", map[string]any{"proxyUrl": "http://user:" + proxyPass + "@10.0.0.1:3128"})
	f.owner.Do("POST", f.base+"/logins/"+loginID+"/code", map[string]any{"code": loginSecret})

	// Refused requests must not echo what they were given either.
	bad := f.owner.Do("PUT", f.base+"/proxy", map[string]any{"scope": "all", "url": "ftp://user:" + proxyPass + "@10.0.0.1"})
	if bad.Code != 400 {
		t.Fatalf("bad proxy: %s", bad)
	}
	bad2 := f.owner.Do("POST", f.base+"/logins/"+loginID+"/code", map[string]any{"code": strings.Repeat(loginSecret, 100)})
	if bad2.Code != 400 {
		t.Fatalf("long code: %s", bad2)
	}
	badEnv := keyEnv
	badEnv.Ct = keySecret
	bad3 := f.owner.Do("POST", f.base+"/agents/claude/key", map[string]any{"envelope": badEnv})
	if bad3.Code != 400 {
		t.Fatalf("bad envelope: %s", bad3)
	}

	needles := []string{keySecret, envSecret, proxyPass, loginSecret, keyEnv.Ct, envEnv.Ct, keyEnv.Epk, envEnv.Epk}
	logs, db := f.s.Logs.String(), dump(t, f.s)
	for _, n := range needles {
		if strings.Contains(logs, n) {
			t.Errorf("%q is in the hub log", n)
		}
		if strings.Contains(db, n) {
			t.Errorf("%q is in the database", n)
		}
		for _, r := range []hubtest.Resp{bad, bad2, bad3} {
			if strings.Contains(string(r.Body), n) {
				t.Errorf("%q is in an error answer: %s", n, r.Body)
			}
		}
	}
	// The access log names the routes and nothing of the query or the body.
	if !strings.Contains(logs, "route=/api/machines/{machineId}/env") {
		t.Error("the requests were not logged at all: the check above proves nothing")
	}
}

func TestQueryOfAnOpenRequestIsNotLogged(t *testing.T) {
	f := setup(t)
	f.owner.Do("GET", f.base+"/open?path="+url.QueryEscape("/m/"+f.a.MachineID+"/?token=QUERYSECRET"), nil)
	if strings.Contains(f.s.Logs.String(), "QUERYSECRET") {
		t.Error("a query string reached the log")
	}
}
