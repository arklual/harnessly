package preflight

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAntigravityModelsAndCache(t *testing.T) {
	dir := t.TempDir()
	script(t, dir, "agy", `if [ "$1" = "--version" ]; then printf 'agy 1.2.3'; else printf 'Fetching available models...\nmodel-flash-low\tFlash Low\nmodel-flash-low\tDuplicate\n'; fi`)
	c := &Checker{Home: t.TempDir(), Env: []string{"PATH=" + dir}}
	r := c.antigravityStatus(dir)
	if !r.Installed || !r.LoggedIn || r.Version == nil || *r.Version != "1.2.3" || len(r.Models) != 1 || r.Models[0].Label != "Flash Low" || r.BinaryPath == nil || *r.BinaryPath != filepath.Join(dir, "agy") {
		t.Fatalf("probe: %+v", r)
	}
	r.Models[0].ID = "mutated"
	script(t, dir, "agy", "exit 1")
	if got := c.antigravityStatus(dir); !got.LoggedIn || got.Models[0].ID != "model-flash-low" {
		t.Fatalf("cache: %+v", got)
	}
	c.antigravityAt = time.Now().Add(-2 * time.Minute)
	if got := c.antigravityStatus(dir); got.LoggedIn {
		t.Fatalf("failed probe authenticated: %+v", got)
	}
}

func TestAntigravityTimeoutNeedsEvidence(t *testing.T) {
	old := cmdTimeout
	cmdTimeout = 20 * time.Millisecond
	defer func() { cmdTimeout = old }()
	dir := t.TempDir()
	script(t, dir, "agy", `if [ "$1" = "--version" ]; then printf 'agy 1.0.0'; else exec /bin/sleep 2; fi`)
	c := &Checker{Home: t.TempDir(), Env: []string{"PATH=" + dir}}
	r := c.antigravityStatus(dir)
	if !r.Installed || r.LoggedIn || !r.Uncertain {
		t.Fatalf("timeout: %+v", r)
	}
	c.antigravity = &Antigravity{LoggedIn: true, Models: []AntigravityModel{{ID: "real", Label: "Real"}}}
	c.antigravityAt = time.Now().Add(-2 * time.Minute)
	if got := c.antigravityStatus(dir); !got.LoggedIn || len(got.Models) != 1 {
		t.Fatalf("prior success: %+v", got)
	}
}

func TestAntigravityParserRejectsNoise(t *testing.T) {
	for _, raw := range []string{"Fetching available models...", "Sign in first", "bad id\tName", "model\t"} {
		if got := parseAntigravityModels(raw); len(got) != 0 {
			t.Fatalf("accepted %q: %+v", raw, got)
		}
	}
}

func TestAntigravityOfficialInstallWithoutPath(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script(t, dir, "agy", `if [ "$1" = "--version" ]; then printf 'agy 1.0.1'; else printf 'Fetching available models...'; fi`)
	c := &Checker{Home: home}
	r := c.antigravityStatus("/nonexistent")
	if !r.Installed || r.BinaryPath == nil || *r.BinaryPath != filepath.Join(dir, "agy") || r.LoggedIn {
		t.Fatalf("official install: %+v", r)
	}
}
