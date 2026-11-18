package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// The models of the ChatGPT subscription follow what Codex itself lists (~/.codex/models_cache.json, refreshed by every
// run of Codex), so a new model (GPT-6.2 Astra) is offered the day OpenAI ships it, not when this file is edited
// The table of the preset stays the fallback for a machine where Codex has not run yet.

// codexCachePath is where Codex keeps its model list; a var for the tests.
var codexCachePath = func() string {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		home = filepath.Join(h, ".codex")
	}
	return filepath.Join(home, "models_cache.json")
}

var gptCache struct {
	sync.Mutex
	path   string
	mod    time.Time
	models []Model
}

type codexCacheModel struct {
	Slug       string `json:"slug"`
	Name       string `json:"display_name"`
	Visibility string `json:"visibility"`
	Default    string `json:"default_reasoning_level"`
	Window     int    `json:"context_window"`
	Levels     []struct {
		Effort string `json:"effort"`
	} `json:"supported_reasoning_levels"`
}

// parseGPTModels: the listed models of Codex's cache in its own order (the newest first), each with the efforts the
// profile can pass on and the 95% of the window Codex itself fills. Nil when the file lists none.
func parseGPTModels(raw []byte) []Model {
	var doc struct {
		Models []codexCacheModel `json:"models"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	var out []Model
	for _, m := range doc.Models {
		if m.Visibility != "list" || !strings.HasPrefix(m.Slug, "gpt-") ||
			slices.ContainsFunc(out, func(x Model) bool { return x.ID == m.Slug }) {
			continue
		}
		mod := Model{ID: m.Slug, Label: strings.TrimSpace(m.Name), Window: m.Window * 95 / 100}
		if mod.Label == "" {
			mod.Label = m.Slug
		}
		if mod.Window <= 0 {
			mod.Window = 258_400
		}
		for _, l := range m.Levels {
			if e := wire.Effort(l.Effort); slices.Contains(gptEfforts, e) {
				mod.Efforts = append(mod.Efforts, e)
			}
		}
		if d := wire.Effort(m.Default); slices.Contains(mod.Efforts, d) {
			mod.DefaultEffort = d
		}
		if known, ok := fallbackGPT(m.Slug); ok {
			mod.Hint = known.Hint
		}
		out = append(out, mod)
	}
	return out
}

func fallbackGPT(id string) (Model, bool) {
	for _, p := range presets {
		if p.ID == "gpt" {
			return p.ModelOf(id)
		}
	}
	return Model{}, false
}

// codexGPTModels is the cache's list, read again only when the file changes.
func codexGPTModels() []Model {
	path := codexCachePath()
	if path == "" {
		return nil
	}
	st, err := os.Stat(path)
	gptCache.Lock()
	defer gptCache.Unlock()
	if err != nil {
		gptCache.path, gptCache.models = "", nil
		return nil
	}
	if gptCache.path != path || !gptCache.mod.Equal(st.ModTime()) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		gptCache.path, gptCache.mod, gptCache.models = path, st.ModTime(), parseGPTModels(raw)
	}
	return gptCache.models
}

// withCodexModels puts the cache's list into the gpt preset, its small and test model the cheapest one (Luna) when
// the list has it, else the last.
func withCodexModels(p Preset, models []Model) Preset {
	if len(models) == 0 {
		return p
	}
	p.Models = slices.Clone(models)
	has := func(id string) bool { _, ok := p.ModelOf(id); return ok }
	if !has(p.Small) || !has(p.Test) {
		small := p.Models[len(p.Models)-1].ID
		for _, m := range p.Models {
			if strings.Contains(m.ID, "luna") {
				small = m.ID
				break
			}
		}
		if !has(p.Small) {
			p.Small = small
		}
		if !has(p.Test) {
			p.Test = small
		}
	}
	return p
}
