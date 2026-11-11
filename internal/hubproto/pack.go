package hubproto

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

// An environment pack (docs/hub/packs.md) is a versioned folder of a git repository that an organization puts
// in its environment as one item: tools for the PATH of the chats, hooks of Claude Code, MCP servers, skills and the
// rules of the safe the pack needs. The folder holds harnessly/pack.json. The machine's setup installs it as a Claude
// Code plugin of its own, so a new version of the pack reaches the machines without a new version of harnessly.

const (
	// EnvKindPack is an environment pack in a repository.
	EnvKindPack = "pack"
	// FeatureEnvPacks is in `hello` when the machine's runtime installs the pack kind; the hub sends it only to such a
	// machine.
	FeatureEnvPacks = "environment.packs"

	// PackManifestFile is the manifest in a pack's folder.
	PackManifestFile = "harnessly/pack.json"
	// PacksDir is where, under the link's folder, the packs are installed: <PacksDir>/<id> is a Claude Code plugin.
	PacksDir = "environment/packs"
	// PackSafeFile is the pack's safe rules next to its plugin files, for whoever wants to read them.
	PackSafeFile = "harnessly-safe.json"
	// PackMarkFile says which version of which source a pack folder holds.
	PackMarkFile = "harnessly-pack.json"
	// PackPluginPrefix names the plugin of a pack: pack-<id>.
	PackPluginPrefix = "pack-"

	PackMaxFiles = 2000
	PackMaxBytes = 16 << 20
)

// PackManifest is harnessly/pack.json.
type PackManifest struct {
	// Schema is the version of this file's format; 1.
	Schema int `json:"schema"`
	// ID names the pack on the machine (a folder and the plugin pack-<id>).
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Version is the pack's own (semver); the hub shows it, the machine reports it.
	Version string `json:"version"`
	// Tools are programs put on the PATH of the chats' shell: Claude Code adds the plugin's bin/ to it.
	Tools []PackTool `json:"tools,omitempty"`
	// Hooks are commands Claude Code runs on its events.
	Hooks []PackHook `json:"hooks,omitempty"`
	// MCP is a .mcp.json of the pack's folder; its servers arrive with the plugin.
	MCP string `json:"mcp,omitempty"`
	// Skills is a folder of skills (folder/<name>/SKILL.md).
	Skills string `json:"skills,omitempty"`
	// Safe says what the pack asks of the person's safe.
	Safe *PackSafe `json:"safe,omitempty"`
}

// PackTool is a program of the pack: Path in the folder, put on the PATH as Name.
type PackTool struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// PackHook is one command on a Claude Code event.
type PackHook struct {
	ID    string `json:"id"`
	Event string `json:"event"`
	// Matcher is Claude Code's matcher of tool names (PreToolUse, PostToolUse); empty for all.
	Matcher string `json:"matcher,omitempty"`
	// Command runs through the shell; ${PACK} is the pack's installed folder. A pack's own script is the usual command.
	Command string `json:"command"`
	// Timeout in seconds; 0 is 10.
	Timeout int `json:"timeout,omitempty"`
}

// PackSafe are the rules of the safe a pack brings. The values never come with the pack: the person puts them in the
// safe themselves (the setup opens the ask), and the pack only names them.
type PackSafe struct {
	// Secrets are the secrets the pack's tools and hooks read from the safe.
	Secrets []PackSecret `json:"secrets,omitempty"`
	// Never are the secrets no chat may be given in its conversation, whatever else says (the pack's rule for the
	// reference prefixes and the names the guard hooks hold to).
	Never []string `json:"never,omitempty"`
}

// PackSecret is a secret the pack needs.
type PackSecret struct {
	Name     string `json:"name"`
	Why      string `json:"why"`
	Optional bool   `json:"optional,omitempty"`
}

var (
	// PackIDRe is a pack's id; it names a folder and a plugin.
	PackIDRe      = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	packToolRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	packHookIDRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)
	packVersionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$`)
)

// PackHookEvents are the events of Claude Code a pack may hook.
var PackHookEvents = []string{"PreToolUse", "PostToolUse", "UserPromptSubmit", "Stop", "SubagentStop", "SessionStart", "Notification"}

// ParsePack reads and checks a manifest. The checks are strict: whatever is not understood is refused, so that an
// unchecked word never reaches a machine.
func ParsePack(raw []byte) (PackManifest, error) {
	var m PackManifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, fmt.Errorf("pack.json не разобран: %w", err)
	}
	if m.Schema != 1 {
		return m, fmt.Errorf("pack.json: schema должен быть 1")
	}
	if !PackIDRe.MatchString(m.ID) {
		return m, fmt.Errorf("pack.json: id — строчные буквы, цифры и дефис, не длиннее 32")
	}
	if strings.TrimSpace(m.Name) == "" || len(m.Name) > 80 {
		return m, fmt.Errorf("pack.json: нужно name не длиннее 80 знаков")
	}
	if !packVersionRe.MatchString(m.Version) {
		return m, fmt.Errorf("pack.json: version — вида 1.2.3")
	}
	seen := map[string]bool{}
	for _, t := range m.Tools {
		if !packToolRe.MatchString(t.Name) || seen[t.Name] {
			return m, fmt.Errorf("pack.json: имя инструмента %q неверно или повторено", t.Name)
		}
		seen[t.Name] = true
		if err := PackRelPath(t.Path); err != nil {
			return m, fmt.Errorf("pack.json: инструмент %s: %w", t.Name, err)
		}
	}
	ids := map[string]bool{}
	for _, h := range m.Hooks {
		switch {
		case !packHookIDRe.MatchString(h.ID) || ids[h.ID]:
			return m, fmt.Errorf("pack.json: id хука %q неверно или повторено", h.ID)
		case !slices.Contains(PackHookEvents, h.Event):
			return m, fmt.Errorf("pack.json: хук %s: событие %q неизвестно", h.ID, h.Event)
		case strings.TrimSpace(h.Command) == "" || len(h.Command) > 1024:
			return m, fmt.Errorf("pack.json: хук %s: нужна команда не длиннее 1024 знаков", h.ID)
		case h.Timeout < 0 || h.Timeout > 600:
			return m, fmt.Errorf("pack.json: хук %s: timeout от 0 до 600 секунд", h.ID)
		case len(h.Matcher) > 200:
			return m, fmt.Errorf("pack.json: хук %s: matcher слишком длинный", h.ID)
		}
		ids[h.ID] = true
	}
	for _, p := range []string{m.MCP, m.Skills} {
		if p != "" {
			if err := PackRelPath(p); err != nil {
				return m, fmt.Errorf("pack.json: %w", err)
			}
		}
	}
	if m.Safe != nil {
		names := map[string]bool{}
		for _, s := range m.Safe.Secrets {
			if !SafeNameRe.MatchString(s.Name) || names[s.Name] || strings.TrimSpace(s.Why) == "" || len(s.Why) > 200 {
				return m, fmt.Errorf("pack.json: секрет %q: имя сейфа, без повторов, с причиной why до 200 знаков", s.Name)
			}
			names[s.Name] = true
		}
		for _, n := range m.Safe.Never {
			if !SafeNameRe.MatchString(n) {
				return m, fmt.Errorf("pack.json: safe.never: %q — не имя секрета сейфа", n)
			}
		}
		if len(m.Safe.Secrets) > 32 || len(m.Safe.Never) > 64 {
			return m, fmt.Errorf("pack.json: слишком много правил сейфа")
		}
	}
	if len(m.Tools) > 64 || len(m.Hooks) > 32 {
		return m, fmt.Errorf("pack.json: слишком много инструментов или хуков")
	}
	return m, nil
}

// PackRelPath says that p is a path inside the pack's folder, written forward-slashed and without «..».
func PackRelPath(p string) error {
	c := path.Clean(p)
	if p == "" || strings.Contains(p, "\\") || path.IsAbs(p) || c == "." || c == ".." || strings.HasPrefix(c, "../") || c != p {
		return fmt.Errorf("путь %q — внутри папки пакета, без «..» и лишних слэшей", p)
	}
	return nil
}
