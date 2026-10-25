package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/catalog"
	"github.com/ly-solutions/harnessly/internal/preflight"
	"github.com/ly-solutions/harnessly/internal/wire"
)

func machineModels(t *testing.T, settings, cache string) *core {
	t.Helper()
	claudeDir, codexDir := filepath.Join(t.TempDir(), "claude"), filepath.Join(t.TempDir(), "codex")
	for dir, files := range map[string]map[string]string{claudeDir: {"settings.json": settings}, codexDir: {"models_cache.json": cache}} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, body := range files {
			if body == "" {
				continue
			}
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return &core{agents: &coreAgents{options: catalog.LaunchConfig{ClaudeDir: claudeDir, CodexDir: codexDir}}}
}

func byID(ms []wire.AgentModel, id string) wire.AgentModel {
	for _, m := range ms {
		if m.ID == id {
			return m
		}
	}
	return wire.AgentModel{}
}

// The page picks a depth from the five levels the CLI takes, and ultracode beside them on the models that run it; the
// model Claude cannot give a choice of (Haiku) offers none and no ultracode; the default is the one the person saved in Claude's settings, and unknown when they saved none.
func TestClaudeModelsOfTheMachine(t *testing.T) {
	t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "")
	ms := machineModels(t, `{"model":"sonnet","effortLevel":"medium"}`, "").v2Models(wire.ProviderClaude)
	opus := byID(ms, "opus")
	want := []wire.Effort{wire.EffortLow, wire.EffortMedium, wire.EffortHigh, wire.EffortXHigh, wire.EffortMax}
	if len(ms) < 3 || len(opus.Efforts) != len(want) || !opus.Ultracode || !byID(ms, "sonnet").Ultracode || byID(ms, "haiku").Ultracode {
		t.Fatalf("models: %+v", ms)
	}
	for i, e := range want {
		if opus.Efforts[i] != e {
			t.Fatalf("opus efforts: %v", opus.Efforts)
		}
	}
	if opus.DefaultEffort == nil || *opus.DefaultEffort != wire.EffortMedium || opus.Hint != "The strongest" && opus.Hint != "Самая сильная" {
		t.Fatalf("opus: %+v", opus)
	}
	if h := byID(ms, "haiku"); len(h.Efforts) != 0 || h.DefaultEffort != nil || h.Hint == "" {
		t.Fatalf("haiku: %+v", h)
	}
	if f := byID(ms, "fable"); f.ID == "" || f.Hint == "" || f.DefaultEffort == nil || *f.DefaultEffort != wire.EffortMedium {
		t.Fatalf("fable: %+v", f) // the person's saved level wins over the model's own
	}
	for _, m := range ms {
		if err := m.Validate(); err != nil {
			t.Errorf("%s: %v", m.ID, err)
		}
	}
	// Nothing saved: the model's own default; the environment of the runtime outranks the settings.
	t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "")
	none := machineModels(t, `{}`, "").v2Models(wire.ProviderClaude)
	for id, want := range map[string]wire.Effort{"opus": wire.EffortMedium, "sonnet": wire.EffortMedium, "fable": wire.EffortHigh} {
		if m := byID(none, id); m.DefaultEffort == nil || *m.DefaultEffort != want {
			t.Fatalf("%s without a saved level: %+v, want %s", id, m.DefaultEffort, want)
		}
	}
	if h := byID(none, "haiku"); h.DefaultEffort != nil {
		t.Fatalf("haiku has no depth to default: %+v", h)
	}
	t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "xhigh")
	if o := byID(machineModels(t, `{"effortLevel":"low"}`, "").v2Models(wire.ProviderClaude), "opus"); o.DefaultEffort == nil || *o.DefaultEffort != wire.EffortXHigh {
		t.Fatalf("the environment's level: %+v", o.DefaultEffort)
	}
}

// Codex says in its model cache what each model is for and which depth it starts at.
func TestCodexModelsOfTheMachine(t *testing.T) {
	cache := `{"fetched_at":"2026-10-22T10:00:00Z","models":[
	  {"slug":"gpt-5.5","display_name":"GPT-5.5","description":"Frontier model for complex coding.","visibility":"list",
	   "default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"high"},{"effort":"xhigh"}]},
	  {"slug":"hidden","display_name":"Hidden","description":"x","visibility":"hide","supported_reasoning_levels":[]}]}`
	ms := machineModels(t, `{}`, cache).v2Models(wire.ProviderCodex)
	if len(ms) != 1 {
		t.Fatalf("models: %+v", ms)
	}
	m := ms[0]
	if m.ID != "gpt-5.5" || m.Hint != "Frontier model for complex coding." || m.DefaultEffort == nil || *m.DefaultEffort != wire.EffortMedium ||
		len(m.Efforts) != 4 || m.Efforts[3] != wire.EffortXHigh {
		t.Fatalf("model: %+v", m)
	}
}

// ultra is a level of its own on the models that have it, and a machine whose Codex has not written a model cache yet
// still offers the models Codex ships with.
func TestCodexModelsWithoutCacheAndUltra(t *testing.T) {
	ms := machineModels(t, `{}`, "").v2Models(wire.ProviderCodex)
	if len(ms) != 7 || ms[0].ID != "gpt-5.6-sol" {
		t.Fatalf("built-in models: %+v", ms)
	}
	for _, id := range []string{"gpt-5.6-sol", "gpt-5.6-terra"} {
		m := byID(ms, id)
		if len(m.Efforts) != 6 || m.Efforts[5] != wire.EffortUltra || m.DefaultEffort == nil {
			t.Errorf("%s: %+v", id, m)
		}
	}
	if m := byID(ms, "gpt-5.6-luna"); len(m.Efforts) != 5 || m.Efforts[4] != wire.EffortMax {
		t.Errorf("luna: %+v", m)
	}
	for _, m := range ms {
		if err := m.Validate(); err != nil {
			t.Errorf("%s: %v", m.ID, err)
		}
	}
	cache := `{"models":[{"slug":"gpt-5.6-sol","visibility":"list","default_reasoning_level":"low","supported_reasoning_levels":[{"effort":"high"},{"effort":"ultra"}]}]}`
	if got := byID(machineModels(t, `{}`, cache).v2Models(wire.ProviderCodex), "gpt-5.6-sol"); len(got.Efforts) != 2 || got.Efforts[1] != wire.EffortUltra {
		t.Errorf("from the cache: %+v", got)
	}
}

// Codex that is installed but not signed in is not ready.
func TestAgentsNowCodexLogin(t *testing.T) {
	status := func(r preflight.Codex) wire.AgentInstallStatus {
		return agentsNow(preflight.Result{Claude: preflight.Claude{Installed: true, LoggedIn: true}, Codex: r}, nil)[1].Status
	}
	if got := status(preflight.Codex{}); got != wire.InstallMissing {
		t.Errorf("missing: %s", got)
	}
	if got := status(preflight.Codex{Installed: true}); got != wire.InstallLoginRequired {
		t.Errorf("signed out: %s", got)
	}
	if got := status(preflight.Codex{Installed: true, LoggedIn: true}); got != wire.InstallReady {
		t.Errorf("signed in: %s", got)
	}
}
