package agentcfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

func firstRunStore(t *testing.T) (*Store, string) {
	t.Helper()
	home := t.TempDir()
	s := New(Config{Home: home, ClaudeDir: filepath.Join(home, ".claude"), CodexDir: filepath.Join(home, ".codex")})
	return s, home
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return m
}

func TestFirstRunClaude(t *testing.T) {
	s, home := firstRunStore(t)
	repo := filepath.Join(home, "work", "repo")
	cwd := filepath.Join(repo, "sub")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"model": "opus", "permissions": {"defaultMode": "bypassPermissions"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"theme": "light", "userID": "u", "projects": {"/other": {"allowedTools": []}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.FirstRun(wire.ProviderClaude, cwd); err != nil {
		t.Fatal(err)
	}
	settings := readJSON(t, filepath.Join(home, ".claude", "settings.json"))
	if settings["skipDangerousModePermissionPrompt"] != true || settings["model"] != "opus" {
		t.Fatalf("settings.json: %v", settings)
	}
	state := readJSON(t, filepath.Join(home, ".claude.json"))
	if state["hasCompletedOnboarding"] != true || state["theme"] != "light" || state["userID"] != "u" {
		t.Fatalf(".claude.json: %v", state)
	}
	projects := state["projects"].(map[string]any)
	for _, d := range []string{cwd, repo} {
		p, _ := projects[d].(map[string]any)
		if p["hasTrustDialogAccepted"] != true {
			t.Fatalf("%s not trusted: %v", d, projects)
		}
	}
	if _, ok := projects["/other"]; !ok {
		t.Fatal("another project lost")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json.lock")); !os.IsNotExist(err) {
		t.Fatal("the lock stays")
	}

	// A second start changes nothing.
	before, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	info, _ := os.Stat(filepath.Join(home, ".claude.json"))
	if err := s.FirstRun(wire.ProviderClaude, cwd); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	info2, _ := os.Stat(filepath.Join(home, ".claude.json"))
	if string(before) != string(after) || !info.ModTime().Equal(info2.ModTime()) {
		t.Fatal("a second start rewrote .claude.json")
	}
}

func TestFirstRunClaudeFreshMachine(t *testing.T) {
	s, home := firstRunStore(t)
	if err := s.FirstRun(wire.ProviderClaude, home); err != nil {
		t.Fatal(err)
	}
	state := readJSON(t, filepath.Join(home, ".claude.json"))
	if state["hasCompletedOnboarding"] != true || state["theme"] != "dark" || state["hasSeenAutoDefaultNudge"] != true {
		t.Fatalf(".claude.json: %v", state)
	}
	if readJSON(t, filepath.Join(home, ".claude", "settings.json"))["skipDangerousModePermissionPrompt"] != true {
		t.Fatal("no bypass consent")
	}
}

// The CLI's own false shows the auto-mode offer, which holds the chat at a dialog: it is made true.
func TestFirstRunClaudeTurnsTheAutoOfferOff(t *testing.T) {
	s, home := firstRunStore(t)
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"hasCompletedOnboarding":true,"theme":"light","hasSeenAutoDefaultNudge":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.FirstRun(wire.ProviderClaude, home); err != nil {
		t.Fatal(err)
	}
	state := readJSON(t, filepath.Join(home, ".claude.json"))
	if state["hasSeenAutoDefaultNudge"] != true || state["hasSeenAutoDefaultNotice"] != true || state["theme"] != "light" {
		t.Fatalf(".claude.json: %v", state)
	}
}

func TestFirstRunClaudeLeavesBrokenFiles(t *testing.T) {
	s, home := firstRunStore(t)
	broken := []byte(`{"theme": `)
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), broken, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.FirstRun(wire.ProviderClaude, home); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(home, ".claude.json")); string(got) != string(broken) {
		t.Fatalf("a broken file was written over: %s", got)
	}
}

func TestFirstRunClaudeWaitsForTheLock(t *testing.T) {
	s, home := firstRunStore(t)
	lock := filepath.Join(home, ".claude.json.lock")
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.FirstRun(wire.ProviderClaude, home); err == nil || !strings.Contains(err.Error(), "held") {
		t.Fatalf("a held lock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); !os.IsNotExist(err) {
		t.Fatal("written under someone else's lock")
	}
}

func TestFirstRunCodex(t *testing.T) {
	s, home := firstRunStore(t)
	repo := filepath.Join(home, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".codex", "config.toml")
	untrusted := filepath.Join(home, "untrusted")
	if err := os.MkdirAll(untrusted, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(codexFile+"\n[projects.\""+untrusted+"\"]\ntrust_level = \"untrusted\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.FirstRun(wire.ProviderCodex, repo); err != nil {
		t.Fatal(err)
	}
	if err := s.FirstRun(wire.ProviderCodex, untrusted); err != nil {
		t.Fatal(err)
	}
	if err := s.FirstRun(wire.ProviderCodex, repo); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	m, err := tomlMap(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := tomlDig(m, []string{"projects", repo}, "trust_level"); v != "trusted" {
		t.Fatalf("repo not trusted:\n%s", raw)
	}
	if v, _ := tomlDig(m, []string{"projects", untrusted}, "trust_level"); v != "untrusted" {
		t.Fatalf("the person's choice changed:\n%s", raw)
	}
	if !strings.HasPrefix(string(raw), codexFile) || strings.Count(string(raw), repo+"\"]") != 1 {
		t.Fatalf("the file:\n%s", raw)
	}
}
