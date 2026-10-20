package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/gate"
)

func TestValidateLaunchInEnglish(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	home := t.TempDir()
	a := newCoreAgents(coreAgentsConfig{DataDir: t.TempDir(), Home: home, ClaudeDir: filepath.Join(home, ".claude"), CodexDir: filepath.Join(home, ".codex")})
	for _, c := range []struct {
		provider      string
		model, effort any
		want          string
	}{
		{"claude", "Bad Model", nil, "Invalid model name"},
		{"claude", "opus", "ultra", "Invalid reasoning effort"},
		{"claude", "haiku", "high", `Model haiku does not support reasoning effort "High"`},
		{"claude", "gpt-5", nil, `Model "gpt-5" is not available for Claude on this machine`},
		{"codex", nil, "xhigh", `The default model does not support reasoning effort "Extra high"`},
	} {
		if _, got := a.ValidateLaunch(c.provider, c.model, c.effort); got != c.want {
			t.Errorf("%s %v %v: %q, want %q", c.provider, c.model, c.effort, got, c.want)
		}
	}
	if got := effortLabel("none"); got != "No reasoning" {
		t.Errorf("none: %q", got)
	}
	if got := string(errorJSON("Could not start the agent")); got != `{"error":"Could not start the agent"}` {
		t.Errorf("body: %s", got)
	}
}

// The editor routes answer in the runtime's language.
func TestEditorRoutesAnswerInTheRuntimesLanguage(t *testing.T) {
	d := deps{
		SessionOK:  func(r *http.Request) bool { return r.Header.Get("X-Test-Session") != "" },
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 0, nil, nil, false },
	}
	var list gate.JSONRoute
	for _, route := range fileRoutes(d) {
		if route.Match(httptest.NewRequest("GET", "/api/editor/files", nil)) {
			list = route
		}
	}
	if list.Compute == nil {
		t.Fatal("no editor route")
	}
	answer := func(session string) string {
		r := httptest.NewRequest("GET", "/api/editor/files?session="+session, nil)
		if session != "" {
			r.Header.Set("X-Test-Session", "1")
		}
		status, body, _ := list.Compute(r)
		raw, _ := json.Marshal(body)
		return strconv.Itoa(status) + " " + string(raw)
	}
	t.Setenv("RUNTIME_LOCALE", "en")
	if got := answer(""); !strings.Contains(got, "401") || !strings.Contains(got, "Sign in to the workspace.") {
		t.Errorf("en signed out: %s", got)
	}
	if got := answer("bad!"); !strings.Contains(got, "400") || !strings.Contains(got, "No active session is selected.") {
		t.Errorf("en no session: %s", got)
	}
	t.Setenv("RUNTIME_LOCALE", "ru")
	if got := answer(""); !strings.Contains(got, "Войдите в рабочее пространство.") {
		t.Errorf("ru signed out: %s", got)
	}
}
