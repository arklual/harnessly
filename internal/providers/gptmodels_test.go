package providers

import (
	"os"
	"path/filepath"
	"testing"
)

const codexCacheSample = `{"fetched_at":"2026-11-18T00:00:00Z","models":[
{"slug":"gpt-6.2-sol","display_name":"GPT-6.2-Sol","visibility":"list","default_reasoning_level":"low","context_window":500000,
 "supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"max"},{"effort":"ultra"}]},
{"slug":"gpt-reserve","display_name":"GPT-Reserve","visibility":"hide","context_window":272000},
{"slug":"gpt-6.2-astra","display_name":"GPT-6.2-Astra","visibility":"list","default_reasoning_level":"ultra","context_window":272000,
 "supported_reasoning_levels":[{"effort":"low"},{"effort":"ultra"}]},
{"slug":"gpt-6-luna","display_name":"GPT-6-Luna","visibility":"list","default_reasoning_level":"medium","context_window":272000,
 "supported_reasoning_levels":[{"effort":"medium"}]}]}`

func TestTheGPTPresetFollowsCodexOwnModelList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models_cache.json")
	if err := os.WriteFile(path, []byte(codexCacheSample), 0o600); err != nil {
		t.Fatal(err)
	}
	old := codexCachePath
	codexCachePath = func() string { return path }
	t.Cleanup(func() { codexCachePath = old })

	p, _ := Find("gpt")
	var ids []string
	for _, m := range p.Models {
		ids = append(ids, m.ID)
	}
	if len(ids) != 3 || ids[0] != "gpt-6.2-sol" || ids[1] != "gpt-6.2-astra" || ids[2] != "gpt-6-luna" {
		t.Fatalf("models %v, want the listed ones in Codex's order", ids)
	}
	if p.Models[0].Window != 475_000 || p.Models[0].DefaultEffort != "low" || len(p.Models[0].Efforts) != 3 {
		t.Fatalf("sol %+v", p.Models[0])
	}
	if p.Models[1].DefaultEffort != "" {
		t.Fatalf("a default the profile cannot pass on is dropped: %+v", p.Models[1])
	}
	if p.Small != "gpt-6-luna" || p.Test != "gpt-6-luna" {
		t.Fatalf("small %q test %q", p.Small, p.Test)
	}
}

func TestTheGPTPresetKeepsItsTableWithoutCodexCache(t *testing.T) {
	old := codexCachePath
	codexCachePath = func() string { return filepath.Join(t.TempDir(), "none.json") }
	t.Cleanup(func() { codexCachePath = old })
	p, _ := Find("gpt")
	if p.Models[0].ID != "gpt-6.2-sol" || p.Small != "gpt-6-luna" {
		t.Fatalf("fallback %v small %q", p.Models[0].ID, p.Small)
	}
}

func TestMain(m *testing.M) {
	// The tests never read the machine's own ~/.codex.
	codexCachePath = func() string { return "" }
	os.Exit(m.Run())
}
