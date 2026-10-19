package catalog

import (
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/text/collate"
)

const (
	quickPromptsWarning  = "Быстрые промпты не прочитались"
	claudePluginsWarning = "Список плагинов Claude не прочитался"
)

// projectDirs: cwd and its parents up to root (inclusive), nearest first; just cwd when it is outside root.
func projectDirs(cwd, root string) []string {
	dirs := []string{filepath.Clean(cwd)}
	top := filepath.Clean(root)
	if !isInside(top, dirs[0]) {
		return dirs
	}
	for dirs[len(dirs)-1] != top {
		last := dirs[len(dirs)-1]
		parent := filepath.Dir(last)
		if parent == last {
			break
		}
		dirs = append(dirs, parent)
	}
	return dirs
}

// settingsFiles: the user's settings, then each project folder's from the top down, the local file after the shared.
func settingsFiles(claudeDir string, dirs []string) []string {
	files := []string{filepath.Join(claudeDir, "settings.json")}
	for i := len(dirs) - 1; i >= 0; i-- {
		files = append(files, filepath.Join(dirs[i], ".claude", "settings.json"), filepath.Join(dirs[i], ".claude", "settings.local.json"))
	}
	return files
}

// claudeWatched: every path whose change can change the catalog.
func claudeWatched(claudeDir, cwd, root string) []string {
	dirs := projectDirs(cwd, root)
	paths := []string{
		filepath.Join(claudeDir, "commands"),
		filepath.Join(claudeDir, "skills"),
		filepath.Join(claudeDir, "plugins", "installed_plugins.json"),
	}
	paths = append(paths, settingsFiles(claudeDir, dirs)...)
	for _, d := range dirs {
		paths = append(paths, filepath.Join(d, ".claude", "commands"), filepath.Join(d, ".claude", "skills"))
	}
	return paths
}

// entriesOf is Object.entries for a parsed JSON value: an object's keys, an array's indexes.
func entriesOf(v any) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		return x
	case []any:
		out := make(map[string]any, len(x))
		for i, e := range x {
			out[itoa(i)] = e
		}
		return out
	}
	return nil
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{byte('0' + i%10)}, b...)
	}
	return string(b)
}

// enabledPlugins: user, then project, then local settings, a later file overriding an earlier one.
func enabledPlugins(claudeDir string, dirs []string) map[string]bool {
	merged := map[string]bool{}
	for _, file := range settingsFiles(claudeDir, dirs) {
		v, _ := readJSON(file)
		settings, _ := v.(map[string]any)
		for key, on := range entriesOf(settings["enabledPlugins"]) {
			if b, ok := on.(bool); ok {
				merged[key] = b
			}
		}
	}
	return merged
}

// pickInstall: the most recently updated install usable from cwd (user-wide, or for a project containing cwd).
// Malformed entries are skipped.
func pickInstall(entries []any, cwd string, col *collate.Collator) (installPath string, ok bool) {
	type usable struct{ path, updated string }
	var list []usable
	for _, e := range entries {
		m, _ := e.(map[string]any)
		path, _ := m["installPath"].(string)
		if path == "" {
			continue
		}
		project, _ := m["projectPath"].(string)
		if m["scope"] != "user" && !(project != "" && isInside(project, cwd)) {
			continue
		}
		updated, _ := m["lastUpdated"].(string)
		list = append(list, usable{path, updated})
	}
	if len(list) == 0 {
		return "", false
	}
	sort.SliceStable(list, func(i, j int) bool { return col.CompareString(list[i].updated, list[j].updated) < 0 })
	return list[len(list)-1].path, true
}

func addFileCommands(b *builder, root, source string, plugin *string, col *collate.Collator) {
	for _, fc := range listMarkdownCommands(root, col) {
		meta, ok := readCommandMeta(fc.file, root)
		if !ok || !meta.userInvocable {
			continue
		}
		name := fc.name
		if plugin != nil && *plugin != "" {
			name = *plugin + ":" + name
		}
		b.add(promptItem(name, "/", source, plugin, meta.description, meta.argumentHint))
	}
}

func addSkills(b *builder, root, source string, plugin *string, col *collate.Collator) {
	for _, fc := range listSkillFiles(root, col) {
		meta, ok := readCommandMeta(fc.file, root)
		if !ok || !meta.userInvocable {
			continue
		}
		own := meta.name
		if own == "" {
			own = fc.name
		}
		name, src := own, source
		if plugin != nil && *plugin != "" {
			name, src = *plugin+":"+own, "plugin"
		}
		b.add(promptItem(name, "/", src, plugin, meta.description, meta.argumentHint))
	}
}

// addQuickPrompts: enabled quick prompts that are a single slash command. An entry of a shape the previous runtime
// failed on stops the list there, with a warning.
func addQuickPrompts(b *builder, prompts []map[string]any) {
	for _, q := range prompts {
		prompt, ok := q["prompt"].(string)
		if !ok {
			b.warn(quickPromptsWarning)
			return
		}
		line := trim(prompt)
		if !truthy(q["enabled"]) || !isSlashWord(line) {
			continue
		}
		var desc string
		switch n := q["name"].(type) {
		case nil:
		case string:
			desc = n
		default:
			b.warn(quickPromptsWarning)
			return
		}
		b.add(promptItem(line[1:], "/", "quick", nil, desc, nil))
	}
}

// isSlashWord: "/" and one word with no whitespace.
func isSlashWord(line string) bool {
	return len(line) >= 2 && line[0] == '/' && !strings.ContainsFunc(line[1:], isSpace)
}

// scanClaude: Claude Code's commands for a folder — curated built-ins, quick prompts, project and user commands,
// skills, and the commands and skills of plugins both installed and enabled. Only names and descriptions are kept.
func scanClaude(claudeDir, cwd, root string, prompts []map[string]any) *builder {
	col := newCollator()
	b := newBuilder()
	dirs := projectDirs(cwd, root)
	for _, row := range claudeBuiltins {
		b.add(builtinItem(row))
	}
	addQuickPrompts(b, prompts)
	for _, d := range dirs {
		addFileCommands(b, filepath.Join(d, ".claude", "commands"), "project", nil, col)
	}
	addFileCommands(b, filepath.Join(claudeDir, "commands"), "user", nil, col)
	for _, d := range dirs {
		addSkills(b, filepath.Join(d, ".claude", "skills"), "project", nil, col)
	}
	addSkills(b, filepath.Join(claudeDir, "skills"), "skill", nil, col)

	installedFile := filepath.Join(claudeDir, "plugins", "installed_plugins.json")
	installed, ok := readJSON(installedFile)
	if (!ok || installed == nil) && mtimeMs(installedFile) > 0 {
		b.warn(claudePluginsWarning)
	}
	enabled := enabledPlugins(claudeDir, dirs)
	inst, _ := installed.(map[string]any)
	all := entriesOf(inst["plugins"])
	keys := make([]string, 0, len(all))
	for k := range all {
		if enabled[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys) // a fixed order first, so equal-collating keys keep one
	sort.SliceStable(keys, func(i, j int) bool { return col.CompareString(keys[i], keys[j]) < 0 })
	for _, key := range keys {
		entries, isArr := all[key].([]any)
		if !isArr {
			continue
		}
		installPath, ok := pickInstall(entries, dirs[0], col)
		if !ok {
			continue
		}
		manifest, _ := readJSON(filepath.Join(installPath, ".claude-plugin", "plugin.json"))
		m, _ := manifest.(map[string]any)
		name, _ := m["name"].(string)
		plugin := trim(name)
		if plugin == "" {
			plugin, _, _ = strings.Cut(key, "@")
		}
		addFileCommands(b, filepath.Join(installPath, "commands"), "plugin", &plugin, col)
		addSkills(b, filepath.Join(installPath, "skills"), "plugin", &plugin, col)
	}
	return b
}
