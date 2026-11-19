package codex

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
)

// every Codex chat starts with memories on, unless the person's config.toml or the machine says otherwise.
func TestCodexChatsStartWithMemoriesOn(t *testing.T) {
	t.Setenv(MemoriesEnv, "")
	cfg := filepath.Join(t.TempDir(), "config.toml")
	c := LaunchConfig{Hooks: &HookConfig{UserConfig: cfg}}
	on := []string{"-c", "features.memories=true"}

	if got := c.memoryArgs(agents.CodexProfile{}); !slices.Equal(got, on) {
		t.Fatalf("no config: %v", got)
	}
	if err := os.WriteFile(cfg, []byte("model = \"gpt-5\"\n[features]\nfast_mode = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := c.memoryArgs(agents.CodexProfile{}); !slices.Equal(got, on) {
		t.Fatalf("a config without the key: %v", got)
	}
	for _, body := range []string{"[features]\nmemories = false\n", "[features]\nmemories = true\n", "features.memories = false\n"} {
		if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := c.memoryArgs(agents.CodexProfile{}); got != nil {
			t.Fatalf("the person's own value is left alone (%q): %v", body, got)
		}
	}
	_ = os.Remove(cfg)
	if got := c.memoryArgs(agents.CodexProfile{Home: "/x"}); got != nil {
		t.Fatalf("a vendor's profile: %v", got)
	}
	t.Setenv(MemoriesEnv, "off")
	if got := c.memoryArgs(agents.CodexProfile{}); got != nil {
		t.Fatalf("the machine's switch: %v", got)
	}
	// Without hooks configured (a launch that has no person's config to read) the default still holds.
	t.Setenv(MemoriesEnv, "")
	if got := (LaunchConfig{}).memoryArgs(agents.CodexProfile{}); !slices.Equal(got, on) {
		t.Fatalf("no hooks: %v", got)
	}
}
