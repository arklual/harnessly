package codex

import (
	"context"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents/agentstest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func fakeCodex(statusLine string, exit int) string {
	code := "0"
	if exit != 0 {
		code = "1"
	}
	return `
case "$1" in
  --version) echo "codex-cli 0.152.0" ;;
  login) echo "` + statusLine + `" >&2; exit ` + code + ` ;;
esac
`
}

func TestStatus(t *testing.T) {
	tests := []struct {
		name      string
		script    string // "" removes the CLI
		installed bool
		login     string
		mode      string
	}{
		{"missing", "", false, "logged_out", ""},
		{"signed out", fakeCodex("Not logged in", 1), true, "logged_out", ""},
		{"ChatGPT", fakeCodex("Logged in using ChatGPT", 0), true, "logged_in", "subscription"},
		{"API key", fakeCodex("Logged in using an API key - sk-proj-***abcd", 0), true, "logged_in", "api_key"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := agentstest.New(t)
			if tc.script != "" {
				r.Script("codex", tc.script)
			}
			st := Status(context.Background(), r.Checker, r.EnvDir)
			if st.Provider != "codex" || st.Installed != tc.installed || st.Login != tc.login {
				t.Fatalf("%+v", st)
			}
			got := ""
			if st.AuthMode != nil {
				got = *st.AuthMode
			}
			if got != tc.mode || st.OverriddenBy != nil {
				t.Errorf("mode %q, overridden %v", got, st.OverriddenBy)
			}
			if tc.installed && (st.Version == nil || *st.Version != "0.152.0") {
				t.Errorf("version %v", st.Version)
			}
		})
	}
}

// The status is asked with the env files sourced, so a key the agent delivered counts as a launch would see it.
func TestStatusSeesTheEnvFiles(t *testing.T) {
	r := agentstest.New(t)
	r.Script("codex", `case "$1" in --version) echo "codex-cli 0.152.0";; login) if [ -n "$MARKER" ]; then echo "Logged in using an API key" >&2; exit 0; fi; echo "Not logged in" >&2; exit 1;; esac`)
	if st := Status(context.Background(), r.Checker, r.EnvDir); st.Login != hubproto.LoggedOut {
		t.Fatalf("%+v", st)
	}
	r.WriteEnv("codex.env", "MARKER='1'\n")
	if st := Status(context.Background(), r.Checker, r.EnvDir); st.Login != hubproto.LoggedIn {
		t.Fatalf("%+v", st)
	}
	// Claude's file is not Codex's.
	r.WriteEnv("codex.env", "")
	r.WriteEnv("claude.env", "MARKER='1'\n")
	if st := Status(context.Background(), r.Checker, r.EnvDir); st.Login != hubproto.LoggedOut {
		t.Fatalf("%+v", st)
	}
}

func TestLoginMode(t *testing.T) {
	for in, want := range map[string]string{
		"Logged in using ChatGPT":                "subscription",
		"Logged in using an API key - sk-***":    "api_key",
		"Logged in using API-key":                "api_key",
		"":                                       "subscription",
		"Logged in using ChatGPT (Plus, a key )": "subscription",
	} {
		if got := LoginMode(in); got != want {
			t.Errorf("%q: %s want %s", in, got, want)
		}
	}
}
