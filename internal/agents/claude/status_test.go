package claude

import (
	"context"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents/agentstest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// fakeClaude answers `claude auth status` with the JSON the real 2.1.x prints; the variable decides whether a key beats
// the subscription, as the real one does.
const fakeClaude = `
case "$1" in
  --version) echo "2.1.310 (Claude Code)" ;;
  auth)
    if [ -n "$ANTHROPIC_API_KEY" ]; then
      echo '{"loggedIn":true,"authMethod":"api_key","apiProvider":"firstParty"}'
    elif [ -n "$CLAUDE_CODE_USE_BEDROCK" ]; then
      echo '{"loggedIn":true,"authMethod":"third_party","apiProvider":"bedrock"}'
    elif [ -f "$HOME/.signed-in" ]; then
      echo '{"loggedIn":true,"authMethod":"claude.ai","apiProvider":"firstParty","email":"a@example.com"}'
    else
      echo '{"loggedIn":false,"authMethod":"none","apiProvider":"firstParty"}'; exit 1
    fi ;;
esac
`

func TestStatus(t *testing.T) {
	signIn := func(r *agentstest.Rig) { r.WriteHome(".signed-in", "x", 0o644) }
	tests := []struct {
		name       string
		setup      func(r *agentstest.Rig)
		installed  bool
		login      string
		mode       string
		overridden string
	}{
		{"not installed", func(r *agentstest.Rig) { r.Remove("claude") }, false, "logged_out", "", ""},
		{"installed, signed out", func(r *agentstest.Rig) {}, true, "logged_out", "", ""},
		{"subscription", signIn, true, "logged_in", "subscription", ""},
		{"only a key", func(r *agentstest.Rig) { r.WriteEnv("claude.env", "ANTHROPIC_API_KEY='sk-ant-x'\n") }, true, "logged_in", "api_key", ""},
		{"a key over a subscription login", func(r *agentstest.Rig) {
			signIn(r)
			r.WriteEnv("claude.env", "ANTHROPIC_API_KEY='sk-ant-x'\n")
		}, true, "logged_in", "api_key", "ANTHROPIC_API_KEY"},
		{"a key from the shared file", func(r *agentstest.Rig) {
			signIn(r)
			r.WriteEnv("all.env", "ANTHROPIC_API_KEY='sk-ant-x'\n")
		}, true, "logged_in", "api_key", "ANTHROPIC_API_KEY"},
		{"cloud", func(r *agentstest.Rig) { r.WriteEnv("claude.env", "CLAUDE_CODE_USE_BEDROCK='1'\n") }, true, "logged_in", "cloud", ""},
		{"cloud over a subscription login", func(r *agentstest.Rig) {
			signIn(r)
			r.WriteEnv("claude.env", "CLAUDE_CODE_USE_BEDROCK='1'\n")
		}, true, "logged_in", "cloud", "CLAUDE_CODE_USE_BEDROCK"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := agentstest.New(t)
			r.Script("claude", fakeClaude)
			tc.setup(r)
			st := Status(context.Background(), r.Checker, r.EnvDir)
			if st.Provider != "claude" || st.Installed != tc.installed || st.Login != tc.login {
				t.Fatalf("%+v", st)
			}
			got := ""
			if st.AuthMode != nil {
				got = *st.AuthMode
			}
			if got != tc.mode {
				t.Errorf("mode %q want %q", got, tc.mode)
			}
			over := ""
			if st.OverriddenBy != nil {
				over = *st.OverriddenBy
			}
			if over != tc.overridden {
				t.Errorf("overriddenBy %q want %q", over, tc.overridden)
			}
			if tc.installed && (st.Version == nil || *st.Version != "2.1.310") {
				t.Errorf("version %v", st.Version)
			}
			if st.Access != nil || st.Soon {
				t.Errorf("the probe fills neither: %+v", st)
			}
		})
	}
}

func TestStatusBinaryInLocalBin(t *testing.T) {
	// Where the native installer puts it, off the PATH.
	r := agentstest.New(t)
	r.WriteHome(".local/bin/claude", "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 2.0.1; else echo '{\"loggedIn\":false}'; exit 1; fi\n", 0o755)
	st := Status(context.Background(), r.Checker, r.EnvDir)
	if !st.Installed || st.Login != hubproto.LoggedOut || st.Version == nil || *st.Version != "2.0.1" {
		t.Fatalf("%+v", st)
	}
}

func TestStatusUnreadableOutput(t *testing.T) {
	r := agentstest.New(t)
	r.Script("claude", `case "$1" in --version) echo "2.1.310";; auth) echo "garbage"; exit 2;; esac`)
	st := Status(context.Background(), r.Checker, r.EnvDir)
	if !st.Installed || st.Login != hubproto.LoginUnknown || st.AuthMode != nil {
		t.Fatalf("%+v", st)
	}
	// No JSON at all, but the CLI says it is signed out by its exit code.
	r.Script("claude", `case "$1" in --version) echo "2.1.310";; auth) exit 1;; esac`)
	if st := Status(context.Background(), r.Checker, r.EnvDir); st.Login != hubproto.LoggedOut {
		t.Fatalf("%+v", st)
	}
	// A shell that does not run at all: unknown, not "signed out".
	r.Checker.Shell = "/nonexistent"
	if st := Status(context.Background(), r.Checker, r.EnvDir); st.Login != hubproto.LoginUnknown || st.Installed {
		t.Fatalf("%+v", st)
	}
}

func TestAuthMode(t *testing.T) {
	for in, want := range map[authJSON]string{
		{AuthMethod: "claude.ai", APIProvider: "firstParty"}: "subscription",
		{AuthMethod: "oauth_token"}:                          "subscription",
		{AuthMethod: "api_key"}:                              "api_key",
		{AuthMethod: "console"}:                              "api_key",
		{AuthMethod: "apiKeyHelper"}:                         "api_key",
		{AuthMethod: "auth_token"}:                           "api_key",
		{AuthMethod: "third_party"}:                          "cloud",
		{AuthMethod: "x", APIProvider: "vertex"}:             "cloud",
		{AuthMethod: "something new"}:                        "subscription",
	} {
		if got := authMode(in); got != want {
			t.Errorf("%+v: %s want %s", in, got, want)
		}
	}
}

func TestStatusScriptNeverPrintsValues(t *testing.T) {
	s := statusScript("")
	if !strings.Contains(s, "echo \"$v\"") || strings.Contains(s, "echo \"$val\"") {
		t.Fatalf("the probe must print names of set variables, never their values:\n%s", s)
	}
}
