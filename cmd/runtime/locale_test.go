package main

import (
	"path/filepath"
	"testing"
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
