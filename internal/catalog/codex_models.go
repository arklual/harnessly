package catalog

import "github.com/ly-solutions/harnessly/internal/locale"

// codexBuiltinModels is the list Codex 0.152 itself offers, for a machine whose ~/.codex/models_cache.json is not
// there (Codex has not run yet), cannot be read or lists nothing: the picker shows these instead of an empty segment.
// Codex refreshes its cache on a run, and the cache wins from then on.
func codexBuiltinModels() []Model {
	row := func(id, label, description, def string, efforts ...string) Model {
		d := def
		return Model{ID: id, Label: label, Hint: id + locale.Pick(" · default ", " · по умолчанию ") + d, Description: description,
			Efforts: efforts, DefaultEffort: &d}
	}
	four := []string{"low", "medium", "high", "xhigh"}
	return []Model{
		row("gpt-5.6-sol", "GPT-5.6-Sol", "Latest frontier agentic coding model.", "low", "low", "medium", "high", "xhigh", "max", "ultra"),
		row("gpt-5.6-terra", "GPT-5.6-Terra", "Balanced agentic coding model for everyday work.", "medium", "low", "medium", "high", "xhigh", "max", "ultra"),
		row("gpt-5.6-luna", "GPT-5.6-Luna", "Fast and affordable agentic coding model.", "medium", "low", "medium", "high", "xhigh", "max"),
		row("gpt-5.5", "GPT-5.5", "Frontier model for complex coding, research, and real-world work.", "medium", four...),
		row("gpt-5.4", "GPT-5.4", "Strong model for everyday coding.", "medium", four...),
		row("gpt-5.4-mini", "GPT-5.4-Mini", "Small, fast, and cost-efficient model for simpler coding tasks.", "medium", four...),
		row("gpt-5.3-codex-spark", "GPT-5.3-Codex-Spark", "Ultra-fast coding model.", "high", four...),
	}
}
