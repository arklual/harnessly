package preflight

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAntigravityCustomBinary(t *testing.T) {
	dir, custom := t.TempDir(), t.TempDir()
	log := filepath.Join(t.TempDir(), "probe.log")
	script(t, dir, "agy", `printf 'official\n' >> "$PROBE_LOG"; exit 1`)
	script(t, custom, "custom agy", `printf '%s\n' "$1" >> "$PROBE_LOG"; if [ "$1" = "--version" ]; then printf 'agy 4.5.6'; else printf 'custom-model\tCustom Model\n'; fi`)
	binary := filepath.Join(custom, "custom agy")
	c := &Checker{Home: t.TempDir(), Env: []string{"PATH=" + dir, "PROBE_LOG=" + log}}
	c.SetAntigravityBinary(binary)
	r := c.antigravityStatus(dir)
	if !r.Installed || !r.LoggedIn || r.Version == nil || *r.Version != "4.5.6" || r.BinaryPath == nil || *r.BinaryPath != binary || len(r.Models) != 1 || r.Models[0].ID != "custom-model" {
		t.Fatalf("custom binary: %+v", r)
	}
	c.SetAntigravityBinary(binary)
	c.antigravityStatus(dir)
	if out, err := os.ReadFile(log); err != nil || string(out) != "--version\nmodels\n" {
		t.Fatalf("probe commands or unchanged selection bypassed cache: %q, %v", out, err)
	}
}

func TestAntigravityMissingOverrideDoesNotFallBack(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	local := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "probe.log")
	for _, bin := range []string{dir, local} {
		script(t, bin, "agy", `printf 'official\n' >> "$PROBE_LOG"; if [ "$1" = "--version" ]; then printf 'agy 1.0.0'; else printf 'official-model\tOfficial Model\n'; fi`)
	}
	c := &Checker{Home: home, Env: []string{"PATH=" + dir, "PROBE_LOG=" + log}}
	for _, binary := range []string{filepath.Join(t.TempDir(), "missing-agy"), "missing-agy"} {
		c.SetAntigravityBinary(binary)
		if r := c.antigravityStatus(dir); r.Installed || r.LoggedIn || r.Version != nil || len(r.Models) != 0 {
			t.Fatalf("missing override %q fell back: %+v", binary, r)
		}
	}
	if out, err := os.ReadFile(log); !os.IsNotExist(err) {
		t.Fatalf("official agy was invoked: %q, %v", out, err)
	}
}

func TestAntigravityBinaryInvalidatesSharedCaches(t *testing.T) {
	home, dir, custom := t.TempDir(), t.TempDir(), t.TempDir()
	script(t, dir, "agy", `if [ "$1" = "--version" ]; then printf 'agy 1.0.0'; else printf 'official-model\tOfficial Model\n'; fi`)
	script(t, custom, "custom-agy", `if [ "$1" = "--version" ]; then printf 'agy 2.0.0'; else printf 'custom-model\tCustom Model\n'; fi`)
	c := Shared(home)
	c.Env, c.Shell = []string{"PATH=" + dir}, "/nonexistent/login-shell"
	check := func(r Antigravity, version, model string) {
		t.Helper()
		if !r.Installed || !r.LoggedIn || r.Version == nil || *r.Version != version || len(r.Models) != 1 || r.Models[0].ID != model {
			t.Fatalf("want %s / %s: %+v", version, model, r)
		}
	}
	check(c.Runtime().Antigravity, "1.0.0", "official-model")
	Shared(home).SetAntigravityBinary(filepath.Join(custom, "custom-agy"))
	check(c.Runtime().Antigravity, "2.0.0", "custom-model")
	Shared(home).SetAntigravityBinary(filepath.Join(custom, "missing-agy"))
	if r := c.Runtime().Antigravity; r.Installed || r.LoggedIn || len(r.Models) != 0 {
		t.Fatalf("missing override retained previous binary's evidence: %+v", r)
	}
	Shared(home).SetAntigravityBinary(filepath.Join(custom, "custom-agy"))

	// Keep the old composite check in flight after its native probe completes.
	c.Invalidate()
	c.codexMu.Lock()
	locked := true
	defer func() {
		if locked {
			c.codexMu.Unlock()
		}
	}()
	done := make(chan Result, 1)
	go func() { done <- c.Runtime() }()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		c.antigravityMu.Lock()
		ready := c.antigravity != nil
		c.antigravityMu.Unlock()
		if ready {
			break
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("native probe did not complete")
		}
	}
	Shared(home).SetAntigravityBinary("")
	c.codexMu.Unlock()
	locked = false
	select {
	case r := <-done:
		check(r.Antigravity, "1.0.0", "official-model")
	case <-time.After(5 * time.Second):
		t.Fatal("invalidated Runtime did not complete")
	}
	check(c.Runtime().Antigravity, "1.0.0", "official-model")
}

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
