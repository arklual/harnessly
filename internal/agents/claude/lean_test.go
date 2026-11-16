package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
)

// a lean start drops the memory index and the person's plugins that bring no hooks and no language servers.
func TestLeanSettings(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	plugin := func(name string, files map[string]string) string {
		p := filepath.Join(dir, "plugins", name)
		for f, body := range files {
			if err := os.MkdirAll(filepath.Dir(filepath.Join(p, f)), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(p, f), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}
	write := func(path string, v any) {
		raw, _ := json.Marshal(v)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	at := func(p string) []map[string]string { return []map[string]string{{"installPath": p}} }
	write(filepath.Join(claudeDir, "settings.json"), map[string]any{"enabledPlugins": map[string]any{
		"tools@m": true, "mlsp@m": true, "guard@m": true, "lsp@m": true, "manifest-hooks@m": true, "off@m": false, "gone@m": true}})
	write(filepath.Join(claudeDir, "plugins", "installed_plugins.json"), map[string]any{"plugins": map[string]any{
		"tools@m":          at(plugin("tools", map[string]string{".mcp.json": "{}", "skills/a/SKILL.md": "x"})),
		"guard@m":          at(plugin("guard", map[string]string{"hooks/hooks.json": "{}"})),
		"lsp@m":            at(plugin("lsp", map[string]string{".lsp.json": "{}"})),
		"manifest-hooks@m": at(plugin("mh", map[string]string{".claude-plugin/plugin.json": `{"name":"mh","hooks":"./h.json"}`})),
		"off@m":            at(plugin("off", map[string]string{".mcp.json": "{}"})),
		"gone@m":           at(filepath.Join(dir, "plugins", "nowhere")),
		"mlsp@m":           at(plugin("mlsp", map[string]string{"README.md": "x"})),
	}})
	write(filepath.Join(claudeDir, "plugins", "known_marketplaces.json"), map[string]any{"m": map[string]any{"installLocation": filepath.Join(dir, "market")}})
	write(filepath.Join(dir, "market", ".claude-plugin", "marketplace.json"), map[string]any{"plugins": []any{
		map[string]any{"name": "mlsp", "lspServers": map[string]any{}}, map[string]any{"name": "tools"}}})
	base := filepath.Join(dir, "hooks.json")
	write(base, map[string]any{"hooks": map[string]any{"Stop": []any{}}, "enabledPlugins": map[string]any{"tools@m": true}})

	path, err := LeanSettings(base, claudeDir)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["autoMemoryEnabled"] != false || got["hooks"] == nil {
		t.Fatalf("settings: %v", got)
	}
	plugins := got["enabledPlugins"].(map[string]any)
	// The launch's own choice stays; only quiet plugins the person turned on go off.
	if plugins["tools@m"] != true || len(plugins) != 1 {
		t.Fatalf("plugins: %v", plugins)
	}
	if err := os.Remove(filepath.Join(dir, "hooks.json")); err != nil {
		t.Fatal(err)
	}
	write(base, map[string]any{})
	path, _ = LeanSettings(base, claudeDir)
	raw, _ = os.ReadFile(path)
	if !strings.Contains(string(raw), `"tools@m": false`) || strings.Contains(string(raw), "guard@m") || strings.Contains(string(raw), "mlsp@m") || strings.Contains(string(raw), "lsp@m") ||
		strings.Contains(string(raw), "manifest-hooks@m") || strings.Contains(string(raw), "gone@m") || strings.Contains(string(raw), "off@m") {
		t.Fatalf("lean settings: %s", raw)
	}

	// A lean launch reads the copy.
	c := LaunchConfig{HooksPath: base, MCPConfigPath: "/m.json", ClaudeDir: claudeDir}
	flags, err := c.Flags("ws", &agents.Launch{Lean: true}, nil)
	if err != nil || !strings.Contains(flags, "hooks.lean.json") {
		t.Fatalf("flags: %s %v", flags, err)
	}
	if flags, _ := c.Flags("ws", &agents.Launch{}, nil); strings.Contains(flags, "lean") {
		t.Fatalf("not lean: %s", flags)
	}
}

// without the memory index a lean chat keeps the person's standing rules: the 🔴 lines and the access section.
func TestMemoryDigest(t *testing.T) {
	index := "## People\n- [Who](user.md) — the person\n- [🔴 Never push the bank's kit to GitHub](f.md) — private GitLab only\n" +
		"## Access and tools\n- [Short replies](r.md) — 2–4 lines\n- [Full list](index.md)\n## UI\n- [No inline styles](s.md)\n" +
		"- [🔴 Heavy work on the build machine](h.md) — always\n"
	got := MemoryDigest(index)
	for _, want := range []string{"🔴 Never push the bank's kit to GitHub — private GitLab only", "🔴 Heavy work on the build machine", "Short replies — 2–4 lines"} {
		if !strings.Contains(got, want) {
			t.Fatalf("digest lacks %q:\n%s", want, got)
		}
	}
	for _, not := range []string{"Who", "Full list", "inline", "f.md"} {
		if strings.Contains(got, not) {
			t.Fatalf("digest has %q:\n%s", not, got)
		}
	}
	if strings.Index(got, "Heavy work") > strings.Index(got, "Short replies") {
		t.Fatal("the 🔴 rules go first")
	}
	long := strings.Repeat("- [🔴 rule](x.md) "+strings.Repeat("x", 200)+"\n", 100)
	if d := MemoryDigest(strings.ReplaceAll(long, "rule", "rule ")); len(d) > LeanMemoryMax+300 {
		t.Fatalf("digest of %d bytes", len(d))
	}
	if MemoryDigest("## A\n- [plain](p.md)\n") != "" {
		t.Fatal("nothing to keep")
	}
}

// A lean launch adds the digest of the memory index to its prompt and denies the platform's notices.
func TestLeanLaunchKeepsTheRules(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	mem := filepath.Join(dir, "mem")
	for path, body := range map[string]string{
		filepath.Join(claudeDir, "settings.json"): `{"autoMemoryDirectory":"` + mem + `"}`,
		filepath.Join(mem, "MEMORY.md"):           "## Rules\n- [🔴 Never on GitHub](g.md) — private only\n",
		filepath.Join(dir, "hooks.json"):          `{}`,
		filepath.Join(dir, "mcp.json"):            `{"mcpServers":{"platform":{}}}`,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c := LaunchConfig{HooksPath: filepath.Join(dir, "hooks.json"), MCPConfigPath: filepath.Join(dir, "mcp.json"), ClaudeDir: claudeDir, JoinDir: filepath.Join(dir, "join")}
	flags, err := c.Flags("", &agents.Launch{Lean: true}, nil)
	if err != nil || !strings.Contains(flags, "--append-system-prompt-file") {
		t.Fatalf("flags: %s %v", flags, err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "lean-memory.md")); !strings.Contains(string(got), "🔴 Never on GitHub — private only") {
		t.Fatalf("digest: %s", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "hooks.lean.json")); !strings.Contains(string(got), "mcp__platform__notice_ask") {
		t.Fatalf("lean settings: %s", got)
	}
}
