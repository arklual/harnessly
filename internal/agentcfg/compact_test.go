package agentcfg

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

func compactStore(t *testing.T, env map[string]string) (*Store, string, string) {
	t.Helper()
	home := t.TempDir()
	s := New(Config{
		Home:      home,
		ClaudeDir: filepath.Join(home, ".claude"),
		CodexDir:  filepath.Join(home, ".codex"),
		Getenv:    func(k string) string { return env[k] },
	})
	return s, filepath.Join(home, ".claude"), filepath.Join(home, ".codex")
}

func writeAt(t *testing.T, path, text string, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeCompactWindowFromSettings(t *testing.T) {
	s, claude, _ := compactStore(t, nil)
	if got := s.CompactOf(wire.ProviderClaude, "", "claude-opus-5-5"); got.Window != 0 {
		t.Fatalf("no file: %+v", got)
	}
	t0 := time.Now().Add(-time.Hour)
	writeAt(t, filepath.Join(claude, "settings.json"), `{"autoCompactWindow": 400000}`, t0)
	if got := s.CompactOf(wire.ProviderClaude, "", "claude-opus-5-5[1m]"); got.Window != 400_000 {
		t.Fatalf("user setting: %+v", got)
	}
	// A changed file is read again.
	writeAt(t, filepath.Join(claude, "settings.json"), `{"autoCompactWindow": "300k"}`, t0.Add(time.Minute))
	if got := s.CompactOf(wire.ProviderClaude, "", "claude-opus-5-5"); got.Window != 300_000 {
		t.Fatalf("changed file: %+v", got)
	}
	// The project's settings are over the user's, its local file over both, and modelSettings over the top-level key.
	proj := t.TempDir()
	writeAt(t, filepath.Join(proj, ".claude", "settings.json"), `{"autoCompactWindow": 500000}`, t0)
	if got := s.CompactOf(wire.ProviderClaude, proj, "claude-opus-5-5"); got.Window != 500_000 {
		t.Fatalf("project: %+v", got)
	}
	writeAt(t, filepath.Join(proj, ".claude", "settings.local.json"),
		`{"autoCompactWindow": 600000, "modelSettings": {"claude-opus-5-5": {"autoCompactWindow": "auto"}}}`, t0)
	if got := s.CompactOf(wire.ProviderClaude, proj, "claude-opus-5-5[1m]"); got.Window != 0 {
		t.Fatalf("auto for the model: %+v", got)
	}
	if got := s.CompactOf(wire.ProviderClaude, proj, "claude-sonnet-5-5"); got.Window != 600_000 {
		t.Fatalf("local file: %+v", got)
	}
}

func TestClaudeCompactWindowRefusesOutOfRangeAndEnvWins(t *testing.T) {
	s, claude, _ := compactStore(t, map[string]string{claudeCompactEnv: "250000"})
	writeAt(t, filepath.Join(claude, "settings.json"), `{"autoCompactWindow": 400000}`, time.Now())
	if got := s.CompactOf(wire.ProviderClaude, "", "x"); got.Window != 250_000 {
		t.Fatalf("env: %+v", got)
	}
	s2, claude2, _ := compactStore(t, nil)
	writeAt(t, filepath.Join(claude2, "settings.json"), `{"autoCompactWindow": 5000}`, time.Now())
	if got := s2.CompactOf(wire.ProviderClaude, "", "x"); got.Window != 0 {
		t.Fatalf("out of range: %+v", got)
	}
}

func TestCodexCompactAt(t *testing.T) {
	s, _, codex := compactStore(t, nil)
	writeAt(t, filepath.Join(codex, "config.toml"), "model = \"gpt-5\"\nmodel_auto_compact_token_limit = 200000\n", time.Now())
	if got := s.CompactOf(wire.ProviderCodex, "", "gpt-5"); got.At != 200_000 || got.Window != 0 {
		t.Fatalf("codex: %+v", got)
	}
}
