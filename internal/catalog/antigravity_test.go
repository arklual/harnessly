package catalog

import (
	"encoding/json"
	"testing"
)

func TestAntigravityLaunch(t *testing.T) {
	cfg := LaunchConfig{AntigravityOptions: func() LaunchOptions {
		return LaunchOptions{Models: []Model{{ID: "actual-flash-low", Label: "Actual", Efforts: []string{"high"}, DefaultEffort: strPtr("high")}}}
	}}
	r := cfg.Launch("antigravity")
	if r.Provider != "antigravity" || len(r.Models) != 1 || r.Models[0].ID != "actual-flash-low" || len(r.Models[0].Efforts) != 0 || r.Models[0].DefaultEffort != nil || r.CliDefault.Model != nil {
		t.Fatalf("launch: %+v", r)
	}
	for _, provider := range []string{"antigravity", "unknown"} {
		r := (LaunchConfig{}).Launch(provider)
		if r.Provider != provider || len(r.Models) != 0 || len(r.Warnings) == 0 {
			t.Fatalf("fallback for %s: %+v", provider, r)
		}
	}
}

func TestAntigravityCommandsFailClosed(t *testing.T) {
	if !IsProvider("antigravity") || IsProvider("unknown") {
		t.Fatal("provider recognition")
	}
	c := New(Config{WorkspaceRoot: t.TempDir(), CodexDir: t.TempDir()}, func(v any) []byte { b, _ := json.Marshal(v); return b })
	for _, provider := range []string{"antigravity", "unknown"} {
		r := c.Get(provider, c.root(), c.root())
		for _, item := range r.Catalog.Items {
			if provider != "antigravity" || item.Source != "quick" {
				t.Fatalf("unexpected native command for %s: %+v", provider, item)
			}
		}
		if _, ok := Builtin(provider, "help"); ok {
			t.Fatalf("invented builtin for %s", provider)
		}
	}
}
