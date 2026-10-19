package catalog

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/BurntSushi/toml"
)

// Choice is a model and reasoning effort; nil means the CLI's own default.
type Choice struct {
	Model  *string `json:"model"`
	Effort *string `json:"effort"`
}

// Model is one model an agent CLI can be started with.
type Model struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
	// Efforts the model accepts; empty means the effort flag is not offered.
	Efforts       []string `json:"efforts"`
	DefaultEffort *string  `json:"defaultEffort"`
}

// LaunchOptions: what this machine can start an agent CLI with, the CLI's default and the remembered choice.
type LaunchOptions struct {
	Provider string `json:"provider"`
	// Source: "static" for Claude, "models-cache" for Codex.
	Source string `json:"source"`
	// FetchedAt is when Codex last refreshed its model list.
	FetchedAt  *string  `json:"fetchedAt"`
	Models     []Model  `json:"models"`
	CliDefault Choice   `json:"cliDefault"`
	Warnings   []string `json:"warnings"`
	Saved      *Choice  `json:"saved"`
}

// LaunchConfig is where launch options come from.
type LaunchConfig struct {
	ClaudeDir, CodexDir string
	// RuntimeConfig is the runtime's config file, DefaultsKey its key holding the remembered choice per agent CLI.
	RuntimeConfig, DefaultsKey string
}

// effortOrder: every reasoning level the CLIs know, weakest first.
var effortOrder = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra", "ultracode"}

// claudeEfforts: the levels Claude Code's --effort accepts.
var claudeEfforts = []string{"low", "medium", "high", "xhigh", "max", "ultracode"}

var modelID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}(\[1m\])?$`)

func isKnownEffort(v string) bool { return slices.Contains(effortOrder, v) }

// claudeModels: the aliases Claude Code takes; Haiku takes --effort silently and ignores it.
func claudeModels() []Model {
	all := func() []string { return append([]string{}, claudeEfforts...) }
	return []Model{
		{ID: "fable", Label: "Fable", Hint: "псевдоним: последняя модель Fable", Efforts: all()},
		{ID: "opus", Label: "Opus", Hint: "псевдоним: последняя модель Opus", Efforts: all()},
		{ID: "sonnet", Label: "Sonnet", Hint: "псевдоним: последняя модель Sonnet", Efforts: all()},
		{ID: "haiku", Label: "Haiku", Hint: "без выбора глубины", Efforts: []string{}},
	}
}

// stringOrNil: a non-blank string, trimmed.
func stringOrNil(v any) *string {
	if s, ok := v.(string); ok {
		if t := trim(s); t != "" {
			return &t
		}
	}
	return nil
}

func knownEffortOrNil(v any) *string {
	if e := stringOrNil(v); e != nil && isKnownEffort(*e) {
		return e
	}
	return nil
}

type fileState int

const (
	fileOK fileState = iota
	fileMissing
	fileBroken
)

// readParsed: missing when the path is not a file, broken when it cannot be read or parsed.
func readParsed[T any](p string, parse func(string) (T, error)) (T, fileState) {
	var zero T
	st, err := os.Stat(p)
	if err != nil || !st.Mode().IsRegular() {
		return zero, fileMissing
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return zero, fileBroken
	}
	v, err := parse(decodeUTF8(raw))
	if err != nil {
		return zero, fileBroken
	}
	return v, fileOK
}

func parseJSONText(raw string) (any, error) {
	var v any
	err := json.Unmarshal([]byte(raw), &v)
	return v, err
}

type codexModels struct {
	fetchedAt *string
	models    []Model
}

// parseCodexModels reads Codex's models_cache.json: listed models only, levels per model.
func parseCodexModels(raw string) (codexModels, error) {
	v, err := parseJSONText(raw)
	if err != nil {
		return codexModels{}, err
	}
	data, _ := v.(map[string]any)
	items, isArr := data["models"].([]any)
	if data == nil || !isArr {
		return codexModels{}, errors.New("models_cache.json has no models")
	}
	out := codexModels{models: []Model{}}
	for _, it := range items {
		m, _ := it.(map[string]any)
		if m == nil || m["visibility"] != "list" {
			continue
		}
		slug, _ := m["slug"].(string)
		if _, isStr := m["slug"].(string); !isStr || !modelID.MatchString(slug) ||
			slices.ContainsFunc(out.models, func(x Model) bool { return x.ID == slug }) {
			continue
		}
		efforts := []string{}
		levels, _ := m["supported_reasoning_levels"].([]any)
		for _, l := range levels {
			lm, _ := l.(map[string]any)
			if e, ok := lm["effort"].(string); ok && isKnownEffort(e) {
				efforts = append(efforts, e)
			}
		}
		var def *string
		if d, ok := m["default_reasoning_level"].(string); ok && slices.Contains(efforts, d) {
			def = &d
		}
		label := slug
		if name, ok := m["display_name"].(string); ok && trim(name) != "" {
			label = trim(name)
		}
		hint := slug
		if def != nil {
			hint = slug + " · по умолчанию " + *def
		}
		out.models = append(out.models, Model{ID: slug, Label: label, Hint: hint, Efforts: efforts, DefaultEffort: def})
	}
	if f, ok := data["fetched_at"].(string); ok {
		out.fetchedAt = &f
	}
	return out, nil
}

// parseCodexDefault: top-level model and model_reasoning_effort of Codex's config.toml.
func parseCodexDefault(raw string) (Choice, error) {
	var doc map[string]any
	if _, err := toml.Decode(raw, &doc); err != nil {
		return Choice{}, err
	}
	return Choice{Model: stringOrNil(doc["model"]), Effort: knownEffortOrNil(doc["model_reasoning_effort"])}, nil
}

func (c LaunchConfig) saved(provider string) *Choice {
	v, _ := readJSON(c.RuntimeConfig)
	cfg, _ := v.(map[string]any)
	defaults, _ := cfg[c.DefaultsKey].(map[string]any)
	s, ok := defaults[provider].(map[string]any)
	if !ok {
		return nil
	}
	return &Choice{Model: stringOrNil(s["model"]), Effort: stringOrNil(s["effort"])}
}

// Launch is what this machine can start an agent CLI with, and what it remembers.
func (c LaunchConfig) Launch(provider string) LaunchOptions {
	warnings := []string{}
	var out LaunchOptions
	if provider == "claude" {
		settings, state := readParsed(filepath.Join(c.ClaudeDir, "settings.json"), parseJSONText)
		if state == fileBroken {
			warnings = append(warnings, "~/.claude/settings.json не читается — значение «Авто» неизвестно")
		}
		var def Choice
		if s, ok := settings.(map[string]any); ok && state == fileOK {
			def = Choice{Model: stringOrNil(s["model"]), Effort: knownEffortOrNil(s["effortLevel"])}
		}
		out = LaunchOptions{Provider: "claude", Source: "static", Models: claudeModels(), CliDefault: def}
	} else {
		models, state := readParsed(filepath.Join(c.CodexDir, "models_cache.json"), parseCodexModels)
		switch {
		case state == fileMissing:
			warnings = append(warnings, "~/.codex/models_cache.json не найден: Codex создаёт его при первом запуске")
		case state == fileBroken:
			warnings = append(warnings, "~/.codex/models_cache.json не читается")
		case len(models.models) == 0:
			warnings = append(warnings, "В ~/.codex/models_cache.json нет доступных моделей")
		}
		def, cfgState := readParsed(filepath.Join(c.CodexDir, "config.toml"), parseCodexDefault)
		if cfgState == fileBroken {
			warnings = append(warnings, "~/.codex/config.toml не читается — значение «Авто» неизвестно")
		}
		out = LaunchOptions{Provider: "codex", Source: "models-cache", FetchedAt: models.fetchedAt, Models: models.models, CliDefault: def}
		if out.Models == nil {
			out.Models = []Model{}
		}
	}
	out.Warnings = warnings
	out.Saved = c.saved(provider)
	return out
}
