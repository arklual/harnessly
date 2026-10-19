package catalog

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/text/collate"
)

const codexConfigWarning = "~/.codex/config.toml не разобрался — навыки плагинов Codex не показаны"

var pluginKey = regexp.MustCompile(`^([A-Za-z0-9._-]+)@([A-Za-z0-9._-]+)$`)

func codexWatched(codexDir string) []string {
	return []string{
		filepath.Join(codexDir, "config.toml"),
		filepath.Join(codexDir, "prompts"),
		filepath.Join(codexDir, "skills"),
		filepath.Join(codexDir, "skills", ".system"),
		filepath.Join(codexDir, "plugins", "cache"),
	}
}

type codexPlugin struct{ plugin, marketplace string }

// enabledCodexPlugins: the `[plugins."name@marketplace"] enabled = true` tables of config.toml, in file order.
func enabledCodexPlugins(text string) ([]codexPlugin, error) {
	var doc map[string]any
	md, err := toml.Decode(text, &doc)
	if err != nil {
		return nil, err
	}
	table, _ := doc["plugins"].(map[string]any)
	var out []codexPlugin
	seen := map[string]bool{}
	for _, key := range md.Keys() {
		if len(key) < 2 || key[0] != "plugins" || seen[key[1]] {
			continue
		}
		seen[key[1]] = true
		value, _ := table[key[1]].(map[string]any)
		if on, _ := value["enabled"].(bool); !on {
			continue
		}
		if m := pluginKey.FindStringSubmatch(key[1]); m != nil {
			out = append(out, codexPlugin{m[1], m[2]})
		}
	}
	// Keys the metadata did not list (none expected) follow in a fixed order.
	var rest []string
	for k := range table {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		value, _ := table[k].(map[string]any)
		if on, _ := value["enabled"].(bool); on {
			if m := pluginKey.FindStringSubmatch(k); m != nil {
				out = append(out, codexPlugin{m[1], m[2]})
			}
		}
	}
	return out, nil
}

// latestVersionDir: the installed version folder of a cached plugin, the most recently written one.
func latestVersionDir(pluginDir string, col *collate.Collator) (string, bool) {
	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		return "", false
	}
	type dated struct {
		name string
		at   float64
	}
	var dirs []dated
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, dated{e.Name(), mtimeMs(filepath.Join(pluginDir, e.Name()))})
		}
	}
	if len(dirs) == 0 {
		return "", false
	}
	sort.SliceStable(dirs, func(i, j int) bool {
		if dirs[i].at != dirs[j].at {
			return dirs[i].at < dirs[j].at
		}
		return col.CompareString(dirs[i].name, dirs[j].name) < 0
	})
	return filepath.Join(pluginDir, dirs[len(dirs)-1].name), true
}

// scanCodex: Codex CLI's commands — curated built-ins, custom prompts ("/prompts:<name>"), and "$" skills: the
// user's (with ".system") and those of plugins enabled in config.toml, named "$<plugin>:<skill>" as Codex inserts them.
func scanCodex(codexDir string) *builder {
	col := newCollator()
	b := newBuilder()
	for _, row := range codexBuiltins {
		b.add(builtinItem(row))
	}

	promptsDir := filepath.Join(codexDir, "prompts")
	for _, fc := range listMarkdownCommands(promptsDir, col) {
		if strings.Contains(fc.name, ":") {
			continue
		}
		meta, ok := readCommandMeta(fc.file, promptsDir)
		if !ok {
			continue
		}
		b.add(promptItem("prompts:"+fc.name, "/", "user", nil, meta.description, meta.argumentHint))
	}

	skillsDir := filepath.Join(codexDir, "skills")
	for _, fc := range listSkillFiles(skillsDir, col, ".system") {
		meta, ok := readCommandMeta(fc.file, skillsDir)
		if !ok || !meta.userInvocable {
			continue
		}
		name := meta.name
		if name == "" {
			name = fc.name
		}
		b.add(promptItem(name, "$", "skill", nil, meta.description, nil))
	}

	var plugins []codexPlugin
	if raw, err := os.ReadFile(filepath.Join(codexDir, "config.toml")); err == nil {
		if plugins, err = enabledCodexPlugins(decodeUTF8(raw)); err != nil {
			b.warn(codexConfigWarning)
		}
	}
	cacheDir := filepath.Join(codexDir, "plugins", "cache")
	for _, p := range plugins {
		versionDir, ok := latestVersionDir(filepath.Join(cacheDir, p.marketplace, p.plugin), col)
		if !ok {
			continue
		}
		pluginSkills := filepath.Join(versionDir, "skills")
		for _, fc := range listSkillFiles(pluginSkills, col) {
			meta, ok := readCommandMeta(fc.file, pluginSkills)
			if !ok || !meta.userInvocable {
				continue
			}
			name := meta.name
			if name == "" {
				name = fc.name
			}
			plugin := p.plugin
			b.add(promptItem(plugin+":"+name, "$", "plugin", &plugin, meta.description, nil))
		}
	}
	return b
}
