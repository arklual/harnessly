package main

import (
	"slices"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
)

// A tab started by the CLI (harnessly tab create) gets the apps on in every chat, as a chat started from the page does;
// a request with apps of its own, and Codex, are left as they are (HRN-790).
func TestWithAlwaysApps(t *testing.T) {
	always := func() []string { return []string{"acme.meter"} }
	if got := withAlwaysApps("claude", nil, always); got == nil || !slices.Equal(got.Apps, []string{"acme.meter"}) {
		t.Fatalf("no launch: %+v", got)
	}
	model := "opus"
	got := withAlwaysApps("claude", &agents.Launch{Model: &model}, always)
	if got == nil || got.Model == nil || *got.Model != "opus" || !slices.Equal(got.Apps, []string{"acme.meter"}) {
		t.Fatalf("with a model: %+v", got)
	}
	own := &agents.Launch{Apps: []string{"x.y"}}
	if got := withAlwaysApps("claude", own, always); !slices.Equal(got.Apps, []string{"x.y"}) {
		t.Fatalf("own apps: %+v", got)
	}
	if got := withAlwaysApps("codex", nil, always); got == nil || !slices.Equal(got.Apps, []string{"acme.meter"}) {
		t.Fatalf("codex: %+v", got)
	}
	if got := withAlwaysApps("terminal", nil, always); got != nil {
		t.Fatalf("terminal: %+v", got)
	}
	if got := withAlwaysApps("claude", nil, func() []string { return nil }); got != nil {
		t.Fatalf("none: %+v", got)
	}
}
