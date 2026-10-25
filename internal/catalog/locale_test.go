package catalog

import (
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

var cyrillic = regexp.MustCompile(`[А-Яа-яЁё]`)

func TestEnglishBuiltins(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	for _, provider := range []string{"claude", "codex"} {
		rows := claudeBuiltins
		if provider == "codex" {
			rows = codexBuiltins
		}
		for _, row := range rows {
			it := builtinItem(row)
			if it.Description == "" || cyrillic.MatchString(it.Description) {
				t.Errorf("%s /%s: description %q", provider, row.name, it.Description)
			}
			if it.Note != nil && cyrillic.MatchString(*it.Note) {
				t.Errorf("%s /%s: note %q", provider, row.name, *it.Note)
			}
			if it.ArgumentHint != nil && cyrillic.MatchString(*it.ArgumentHint) {
				t.Errorf("%s /%s: hint %q", provider, row.name, *it.ArgumentHint)
			}
		}
	}
	it, ok := Builtin("claude", "compact")
	if !ok || it.Description != "Compact the conversation history to free up context" || it.ArgumentHint == nil || *it.ArgumentHint != "[instructions]" || it.SourceLabel != "built-in" {
		t.Errorf("claude compact: %+v", it)
	}
	if it, ok := Builtin("claude", "logout"); !ok || it.Note == nil || *it.Note != "Sign-in on the machine is managed centrally" {
		t.Errorf("claude logout: %+v", it)
	}
	if it, ok := Builtin("codex", "delete"); !ok || !it.Danger || *it.Note != "The Codex conversation will be deleted for good and cannot be restored." {
		t.Errorf("codex delete: %+v", it)
	}
	if it, ok := Builtin("codex", "mcp"); !ok || *it.ArgumentHint != "[verbose]" {
		t.Errorf("codex mcp: %+v", it)
	}
	if it, ok := Builtin("claude", "theme"); !ok || !it.Hidden || *it.Note != "Works only in the terminal: this command has no web interface" {
		t.Errorf("terminal row: %+v", it)
	}
}

func TestBuiltinsKeepTheirRussianTexts(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "ru")
	it, ok := Builtin("claude", "compact")
	if !ok || it.Description != "Сжать историю диалога, чтобы освободить контекст" || *it.ArgumentHint != "[инструкция]" || it.SourceLabel != "встроенная" {
		t.Errorf("claude compact: %+v", it)
	}
}

func TestEnglishWarningsAndFolderErrors(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	base := t.TempDir()
	root := filepath.Join(base, "root")
	write(t, filepath.Join(root, "a/.keep"), "")
	c := New(Config{ClaudeDir: filepath.Join(base, "c"), CodexDir: filepath.Join(base, "x"), WorkspaceRoot: root,
		QuickPromptsFile: filepath.Join(base, "q.json"), WorkspacesFile: filepath.Join(base, "ws.json")}, encode)
	if _, _, e := c.ResolveCwd("relative/path"); e != "The folder is outside the workspace" {
		t.Errorf("outside: %q", e)
	}
	if _, _, e := c.ResolveCwd(filepath.Join(root, "missing")); e != "Folder not found" {
		t.Errorf("missing: %q", e)
	}

	cfg := LaunchConfig{ClaudeDir: filepath.Join(base, "c"), CodexDir: filepath.Join(base, "x"), RuntimeConfig: filepath.Join(base, "config.json"), DefaultsKey: "defaults"}
	write(t, filepath.Join(base, "c/settings.json"), `{`)
	if got := cfg.Launch("claude"); !slices.Equal(got.Warnings, []string{`~/.claude/settings.json cannot be read: the value of "Auto" is unknown`}) {
		t.Errorf("claude warnings: %q", got.Warnings)
	}
	if got := cfg.Launch("claude"); got.Models[0].Hint != "alias: the latest Fable model" {
		t.Errorf("claude model hint: %q", got.Models[0].Hint)
	}
	if got := cfg.Launch("codex"); !slices.Equal(got.Warnings, []string{"~/.codex/models_cache.json not found (Codex creates it on its first run): the built-in model list is shown"}) {
		t.Errorf("codex warnings: %q", got.Warnings)
	}
	write(t, filepath.Join(base, "x/models_cache.json"), `{"models":[{"slug":"gpt-5","visibility":"list","default_reasoning_level":"high","supported_reasoning_levels":[{"effort":"high"}]}]}`)
	if got := cfg.Launch("codex"); got.Models[0].Hint != "gpt-5 · default high" {
		t.Errorf("codex model hint: %q", got.Models[0].Hint)
	}
	if got := scanCodex(filepath.Join(base, "x")); len(got.warnings) > 0 && cyrillic.MatchString(got.warnings[0]) {
		t.Errorf("codex scan: %q", got.warnings)
	}
}
