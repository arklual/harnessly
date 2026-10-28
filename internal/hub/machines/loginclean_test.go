package machines_test

import (
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/machines"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestSafeLoginURL(t *testing.T) {
	good := []struct{ provider, raw, want string }{
		{"claude", "https://claude.ai/oauth/authorize?code=true&client_id=x&state=y", "https://claude.ai/oauth/authorize?code=true&client_id=x&state=y"},
		{"claude", "https://console.anthropic.com/oauth/authorize?x=1", "https://console.anthropic.com/oauth/authorize?x=1"},
		{"claude", "https://platform.claude.com/oauth/authorize", "https://platform.claude.com/oauth/authorize"},
		{"claude", "https://CLAUDE.AI/login", "https://claude.ai/login"},
		{"codex", "https://auth.openai.com/codex/device", "https://auth.openai.com/codex/device"},
	}
	for _, c := range good {
		if got, ok := machines.SafeLoginURL(c.provider, c.raw); !ok || got != c.want {
			t.Errorf("%s %q = %q %v, want %q", c.provider, c.raw, got, ok, c.want)
		}
	}
	bad := map[string]struct{ provider, raw string }{
		"empty":                 {"claude", ""},
		"http":                  {"claude", "http://claude.ai/login"},
		"another host":          {"claude", "https://evil.example.net/login"},
		"a subdomain":           {"claude", "https://evil.claude.ai/login"},
		"a lookalike":           {"claude", "https://claude.ai.evil.example.net/login"},
		"a suffix lookalike":    {"claude", "https://notclaude.ai/login"},
		"userinfo":              {"claude", "https://claude.ai@evil.example.net/login"},
		"userinfo of the host":  {"claude", "https://claude.ai:x@evil.example.net/"},
		"a port":                {"claude", "https://claude.ai:8443/login"},
		"a backslash":           {"claude", "https://claude.ai\\@evil.example.net/login"},
		"a space":               {"claude", "https://claude.ai/ login"},
		"a newline":             {"claude", "https://claude.ai/login\nhttps://evil.example.net"},
		"a javascript scheme":   {"claude", "javascript:alert(1)"},
		"a data scheme":         {"claude", "data:text/html,<script>alert(1)</script>"},
		"no scheme":             {"claude", "//claude.ai/login"},
		"opaque":                {"claude", "https:claude.ai"},
		"a trailing dot":        {"claude", "https://claude.ai./login"},
		"codex host for claude": {"claude", "https://auth.openai.com/codex/device"},
		"claude host for codex": {"codex", "https://claude.ai/login"},
		"gemini is off":         {"gemini", "https://accounts.google.com/"},
		"unknown provider":      {"", "https://claude.ai/login"},
		"too long":              {"claude", "https://claude.ai/" + strings.Repeat("a", 5000)},
	}
	for name, c := range bad {
		if got, ok := machines.SafeLoginURL(c.provider, c.raw); ok {
			t.Errorf("%s: %q was accepted as %q", name, c.raw, got)
		}
	}
}

func TestSafeConsoleURL(t *testing.T) {
	const id = "aabbccddeeff00112233"
	for _, raw := range []string{"/m/" + id + "/console/0123456789abcdef", "/m/" + id + "/?tab=tab-1_x"} {
		if !machines.SafeConsoleURL(id, raw) {
			t.Errorf("%q refused", raw)
		}
	}
	for name, raw := range map[string]string{
		"another machine":   "/m/other/console/0123456789abcdef",
		"an absolute url":   "https://evil.example.net/m/" + id + "/console/0123456789abcdef",
		"protocol relative": "//evil.example.net/m/" + id + "/console/0123456789abcdef",
		"outside the base":  "/api/account",
		"the machine root":  "/m/" + id + "/",
		"an api path":       "/m/" + id + "/api/terminal",
		"a short id":        "/m/" + id + "/console/0123",
		"a non hex id":      "/m/" + id + "/console/0123456789abcdeg",
		"dots":              "/m/" + id + "/console/../../x",
		"a bad tab":         "/m/" + id + "/?tab=a&x=1",
		"a javascript url":  "javascript:alert(1)",
		"empty":             "",
	} {
		if machines.SafeConsoleURL(id, raw) {
			t.Errorf("%s: %q accepted", name, raw)
		}
	}
}

func str(s string) *string { return &s }

func TestCleanLogin(t *testing.T) {
	const id = "aabbccddeeff00112233"
	t.Run("a good login passes unchanged", func(t *testing.T) {
		in := hubproto.Login{ID: "x", Provider: "codex", Method: "link", State: hubproto.LoginURL, URL: str("https://auth.openai.com/codex/device"), UserCode: str("K7QF-2M9P")}
		out, refused := machines.CleanLogin(in, id)
		if len(refused) != 0 || *out.URL != *in.URL || *out.UserCode != "K7QF-2M9P" || out.State != hubproto.LoginURL || out.Error != nil {
			t.Errorf("out = %+v refused %v", out, refused)
		}
	})
	t.Run("a link elsewhere ends the login", func(t *testing.T) {
		in := hubproto.Login{ID: "x", Provider: "claude", Method: "link", State: hubproto.LoginURL, URL: str("https://evil.example.net/phish"), NeedsCode: true}
		out, refused := machines.CleanLogin(in, id)
		if out.URL != nil || out.State != hubproto.LoginFailed || out.Error == nil || out.NeedsCode || len(refused) != 1 || refused[0] != "url" {
			t.Errorf("out = %+v refused %v", out, refused)
		}
		if strings.Contains(*out.Error, "evil") {
			t.Errorf("the error repeats the machine's text: %q", *out.Error)
		}
	})
	t.Run("a link in a state that does not wait for it is only dropped", func(t *testing.T) {
		in := hubproto.Login{ID: "x", Provider: "claude", Method: "link", State: hubproto.LoginVerifying, URL: str("https://evil.example.net/phish")}
		out, _ := machines.CleanLogin(in, id)
		if out.URL != nil || out.State != hubproto.LoginVerifying {
			t.Errorf("out = %+v", out)
		}
	})
	t.Run("a console elsewhere ends the console login", func(t *testing.T) {
		in := hubproto.Login{ID: "x", Provider: "claude", Method: "console", State: hubproto.LoginWaiting, ConsoleURL: str("https://evil.example.net/m/" + id + "/console/0123456789abcdef")}
		out, refused := machines.CleanLogin(in, id)
		if out.ConsoleURL != nil || out.State != hubproto.LoginFailed || out.Error == nil || len(refused) != 1 || refused[0] != "consoleUrl" {
			t.Errorf("out = %+v refused %v", out, refused)
		}
		ok := hubproto.Login{ID: "x", Provider: "claude", Method: "console", State: hubproto.LoginWaiting, ConsoleURL: str("/m/" + id + "/console/0123456789abcdef")}
		if out, refused := machines.CleanLogin(ok, id); out.State != hubproto.LoginWaiting || out.ConsoleURL == nil || len(refused) != 0 {
			t.Errorf("good console: %+v %v", out, refused)
		}
	})
	t.Run("the code and the error are bounded", func(t *testing.T) {
		in := hubproto.Login{ID: "x", Provider: "codex", Method: "link", State: hubproto.LoginFailed, UserCode: str("<script>"), Error: str(strings.Repeat("я", 900))}
		out, refused := machines.CleanLogin(in, id)
		if out.UserCode != nil || len([]rune(*out.Error)) != 500 || len(refused) != 1 || refused[0] != "userCode" {
			t.Errorf("out = %+v refused %v", out, refused)
		}
	})
}
