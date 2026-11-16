package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// LeanSettings writes, next to a settings file, a copy of it for a lean start (rewritten only when its content changes)
// and gives its path. A delegated chat gets a whole task and needs neither the person's memory index nor every plugin
// they keep for their own chats: a start in the person's profile measured 63 thousand tokens of context, 23 of them the
// memory index (11) and plugins that only bring MCP servers and skills (12). The copy turns Claude Code's
// auto memory off and the person's plugins that have no hooks and no language servers: a plugin with hooks may guard
// what the agent does (a push, a release), so it stays, as do the plugins of the platform itself (--plugin-dir).
func LeanSettings(settingsPath, claudeDir string, deny ...string) (string, error) {
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		return "", err
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		return "", err
	}
	settings["autoMemoryEnabled"] = false
	if len(deny) > 0 {
		perms, _ := settings["permissions"].(map[string]any)
		if perms == nil {
			perms = map[string]any{}
		}
		have, _ := perms["deny"].([]any)
		for _, d := range deny {
			have = append(have, d)
		}
		perms["deny"] = have
		settings["permissions"] = perms
	}
	if off := quietPlugins(claudeDir); len(off) > 0 {
		plugins, _ := settings["enabledPlugins"].(map[string]any)
		if plugins == nil {
			plugins = map[string]any{}
		}
		for _, name := range off {
			if _, set := plugins[name]; !set {
				plugins[name] = false
			}
		}
		settings["enabledPlugins"] = plugins
	}
	body, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", err
	}
	target := strings.TrimSuffix(settingsPath, ".json") + ".lean.json"
	if have, err := os.ReadFile(target); err == nil && string(have) == string(body) {
		return target, nil
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return target, nil
}

// quietPlugins are the plugins the person turned on in their settings that bring neither hooks nor language servers:
// only MCP servers, skills, commands or agents, which a delegated chat does without. A plugin whose folder cannot be
// found or read is kept.
func quietPlugins(claudeDir string) []string {
	if claudeDir == "" {
		return nil
	}
	var user struct {
		EnabledPlugins map[string]any `json:"enabledPlugins"`
	}
	if raw, err := os.ReadFile(filepath.Join(claudeDir, "settings.json")); err != nil || json.Unmarshal(raw, &user) != nil {
		return nil
	}
	var installed struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if raw, err := os.ReadFile(filepath.Join(claudeDir, "plugins", "installed_plugins.json")); err != nil || json.Unmarshal(raw, &installed) != nil {
		return nil
	}
	var known map[string]struct {
		InstallLocation string `json:"installLocation"`
	}
	if raw, err := os.ReadFile(filepath.Join(claudeDir, "plugins", "known_marketplaces.json")); err == nil {
		_ = json.Unmarshal(raw, &known)
	}
	entries := map[string]map[string]map[string]json.RawMessage{} // marketplace → plugin → its entry
	var out []string
	for name, on := range user.EnabledPlugins {
		if on != true {
			continue
		}
		at := installed.Plugins[name]
		if len(at) == 0 || at[0].InstallPath == "" {
			continue
		}
		// A marketplace may declare a plugin's hooks or language servers in its own entry (strict: false).
		plugin, market, _ := strings.Cut(name, "@")
		if _, ok := entries[market]; !ok {
			entries[market] = marketEntries(known[market].InstallLocation)
		}
		entry, listed := entries[market][plugin]
		_, hooks := entry["hooks"]
		_, lsp := entry["lspServers"]
		if listed && (hooks || lsp) {
			continue
		}
		if quiet(at[0].InstallPath) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// marketEntries are the plugin entries of a marketplace's folder by name; none when it cannot be read.
func marketEntries(dir string) map[string]map[string]json.RawMessage {
	out := map[string]map[string]json.RawMessage{}
	if dir == "" {
		return out
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "marketplace.json"))
	if err != nil {
		return out
	}
	var m struct {
		Plugins []map[string]json.RawMessage `json:"plugins"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return out
	}
	for _, p := range m.Plugins {
		var name string
		if json.Unmarshal(p["name"], &name) == nil && name != "" {
			out[name] = p
		}
	}
	return out
}

// quiet tells a plugin folder without hooks and language servers; false when it cannot tell.
func quiet(dir string) bool {
	if _, err := os.Stat(dir); err != nil {
		return false
	}
	for _, f := range []string{filepath.Join("hooks", "hooks.json"), ".lsp.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return false
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if os.IsNotExist(err) {
		return true // no manifest: nothing declared beyond the folders looked at
	}
	if err != nil {
		return false
	}
	var manifest map[string]json.RawMessage
	if json.Unmarshal(raw, &manifest) != nil {
		return false
	}
	_, hooks := manifest["hooks"]
	_, lsp := manifest["lspServers"]
	return !hooks && !lsp
}

// LeanMemoryMax is how long the memory digest of a lean start may be, in bytes: about 1.5 thousand tokens.
const LeanMemoryMax = 4500

var (
	mdLink     = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	rulesTitle = regexp.MustCompile(`(?i)^(доступ|access|rules|правила)`)
)

// MemoryDigest is what a lean start keeps of the person's memory index (MEMORY.md) instead of the whole of it: the
// lines marked 🔴 (rules the person holds every chat to), then the lines of a section about access and tools, links
// reduced to their titles, up to LeanMemoryMax. Built from the index at every start, so a new 🔴 rule goes in by itself.
// "" when the index has none of them.
func MemoryDigest(index string) string {
	var red, rules []string
	section := ""
	for _, line := range strings.Split(index, "\n") {
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		text := mdLink.ReplaceAllString(strings.TrimSpace(line), "$1")
		switch {
		case strings.Contains(line, "🔴"):
			red = append(red, text)
		case rulesTitle.MatchString(section) && !strings.Contains(strings.ToLower(text), "полный список") && !strings.Contains(strings.ToLower(text), "full list"):
			rules = append(rules, text)
		}
	}
	var out []string
	size := 0
	seen := map[string]bool{}
	for _, l := range append(red, rules...) {
		if seen[l] {
			continue
		}
		if size+len(l)+1 > LeanMemoryMax {
			break
		}
		seen[l] = true
		out = append(out, l)
		size += len(l) + 1
	}
	if len(out) == 0 {
		return ""
	}
	return "# The person's standing rules\n\nThis chat starts without the person's memory; these lines of it hold in every chat " +
		"they run. Keep to them.\n\n" + strings.Join(out, "\n")
}

// memoryIndex is the person's memory index: MEMORY.md in the autoMemoryDirectory of Claude Code's settings; "" when
// it names none or the file cannot be read.
func memoryIndex(claudeDir, home string) string {
	var user struct {
		Dir string `json:"autoMemoryDirectory"`
	}
	if raw, err := os.ReadFile(filepath.Join(claudeDir, "settings.json")); err != nil || json.Unmarshal(raw, &user) != nil || user.Dir == "" {
		return ""
	}
	dir := user.Dir
	if rest, ok := strings.CutPrefix(dir, "~/"); ok {
		dir = filepath.Join(home, rest)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
	if err != nil {
		return ""
	}
	return string(raw)
}

// LeanMemoryFile writes the digest of the person's memory index beside a settings file (rewritten only when it
// changes) and gives its path, "" when there is nothing to keep.
func LeanMemoryFile(settingsPath, claudeDir string) string {
	digest := MemoryDigest(memoryIndex(claudeDir, filepath.Dir(claudeDir)))
	if digest == "" {
		return ""
	}
	target := filepath.Join(filepath.Dir(settingsPath), "lean-memory.md")
	if have, err := os.ReadFile(target); err == nil && string(have) == digest+"\n" {
		return target
	}
	tmp := target + ".tmp"
	if os.WriteFile(tmp, []byte(digest+"\n"), 0o600) != nil || os.Rename(tmp, target) != nil {
		_ = os.Remove(tmp)
		return ""
	}
	return target
}

// leanDeny are the tools of the platform's own MCP servers (in the --mcp-config file at mcpPath) a delegated chat goes
// without: the smart notice at the top of the person's screen is the leading chat's to ask through.
func leanDeny(mcpPath string) []string {
	raw, err := os.ReadFile(mcpPath)
	if err != nil {
		return nil
	}
	var cfg struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Servers))
	for n := range cfg.Servers {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []string
	for _, n := range names {
		for _, t := range []string{"notice_ask", "notice_show", "notice_update", "notice_close"} {
			out = append(out, "mcp__"+n+"__"+t)
		}
	}
	return out
}
