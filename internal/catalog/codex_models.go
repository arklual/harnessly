package catalog

import "github.com/ly-solutions/harnessly/internal/locale"

// codexBuiltinModels is the list Codex 0.169 itself offers, for a machine whose ~/.codex/models_cache.json is not
// there (Codex has not run yet), cannot be read or lists nothing: the picker shows these instead of an empty segment.
// Codex refreshes its cache on a run, and the cache wins from then on.
func codexBuiltinModels() []Model {
	row := func(id, label, description, def string, efforts ...string) Model {
		d := def
		return Model{ID: id, Label: label, Hint: id + locale.Pick(" · default ", " · по умолчанию ") + d, Description: description,
			Efforts: efforts, DefaultEffort: &d}
	}
	six := []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	return []Model{
		row("gpt-6.2-sol", "GPT-6.2-Sol", "Latest frontier agentic coding model.", "low", six...),
		row("gpt-6.2-astra", "GPT-6.2-Astra", "Most capable model for the hardest tasks.", "low", six...),
		row("gpt-6.1-terra", "GPT-6.1 Terra", "Balanced agentic coding model for everyday work.", "low", six...),
		row("gpt-6.1-sol", "GPT-6.1-Sol", "Frontier agentic coding model.", "low", six...),
		row("gpt-6-luna", "GPT-6-Luna", "Fast and affordable agentic coding model.", "medium", "low", "medium", "high", "xhigh", "max"),
	}
}
