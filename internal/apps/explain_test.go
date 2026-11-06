package apps

import (
	"errors"
	"strings"
	"testing"
)

// Explain names the layer that keeps an app off.
func TestExplainNamesTheLayerThatKeepsAnAppOff(t *testing.T) {
	m := func(mut func(*Manifest)) *Manifest {
		x := &Manifest{ID: "acme.pulse", Trust: TrustSandboxed, Targets: []string{TargetChat}}
		if mut != nil {
			mut(x)
		}
		return x
	}
	off := func(lines []ExplainLine) string {
		for _, l := range lines {
			if l.Off {
				return l.Layer + ": " + l.Says
			}
		}
		return ""
	}
	chat := func(provider string, on ...string) *ExplainChat { return &ExplainChat{Provider: provider, Apps: on} }
	for _, c := range []struct {
		name string
		in   ExplainInput
		off  string
	}{
		{"not installed", ExplainInput{ID: "acme.pulse"}, "machine: acme.pulse is not installed"},
		{"on", ExplainInput{ID: "acme.pulse", App: &Installed{ID: "acme.pulse", Version: "1.0.0", Manifest: m(nil)},
			Chat: chat("claude", "acme.pulse")}, ""},
		{"off in the chat", ExplainInput{ID: "acme.pulse", App: &Installed{ID: "acme.pulse", Manifest: m(nil)},
			Chat: chat("claude")}, "chat: off in this chat"},
		{"on in codex", ExplainInput{ID: "acme.pulse", App: &Installed{ID: "acme.pulse", Manifest: m(nil)},
			Chat: chat("codex", "acme.pulse")}, ""},
		{"another agent", ExplainInput{ID: "acme.pulse", App: &Installed{ID: "acme.pulse", Manifest: m(nil)},
			Chat: chat("gemini", "acme.pulse")}, "chat: this chat runs gemini"},
		{"waits for a secret", ExplainInput{ID: "acme.pulse", App: &Installed{ID: "acme.pulse", Manifest: m(nil)},
			Waiting: []string{"gitlab-token"}}, "setup: waits for secrets: gitlab-token"},
		{"not a chat app", ExplainInput{ID: "acme.pulse", App: &Installed{ID: "acme.pulse",
			Manifest: m(func(x *Manifest) { x.Targets = []string{TargetApp} })}, Chat: chat("claude")}, "activation: works in app"},
		{"host too old", ExplainInput{ID: "acme.pulse", HostVersion: "0.10.0", App: &Installed{ID: "acme.pulse",
			Manifest: m(func(x *Manifest) { x.Harnessly = ">=0.16.0" })}}, "machine: this harnessly is 0.10.0"},
		{"muted team app", ExplainInput{ID: "acme.pulse", App: &Installed{ID: "acme.pulse", Team: true, Muted: true,
			Manifest: m(nil)}}, "team: the person turned it off"},
	} {
		got := off(Explain(c.in))
		if c.off == "" && got != "" || !strings.HasPrefix(got, c.off) {
			t.Errorf("%s: off %q, want %q", c.name, got, c.off)
		}
	}
	// The wall is told as it is: none here, down with its cause, or standing.
	wall := func(in ExplainInput) string {
		lines := Explain(in)
		return lines[len(lines)-1].Says
	}
	base := ExplainInput{ID: "acme.pulse", App: &Installed{ID: "acme.pulse", Manifest: m(nil)}}
	if s := wall(base); !strings.HasPrefix(s, "no wall") {
		t.Errorf("no wall: %q", s)
	}
	base.Wall, base.WallErr = true, errors.New("bwrap: not found")
	if s := wall(base); !strings.Contains(s, "bwrap: not found") {
		t.Errorf("wall down: %q", s)
	}
	base.WallErr = nil
	if s := wall(base); !strings.Contains(s, "inside the wall") {
		t.Errorf("wall up: %q", s)
	}
}
