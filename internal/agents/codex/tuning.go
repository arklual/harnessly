package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

// Defaults a Codex chat of harnessly starts with. They are `-c key=value` flags, so each is added only where
// the person's own config.toml says nothing about the key: a value the person wrote stays theirs.
//
//   - model_context_window: Codex runs a model on the window its catalog calls `context_window` (272k, or 500k for the
//     newest), while the same catalog lists `max_context_window` (872k) as what the model takes. The chat is started on
//     the model's own maximum. Only on a ChatGPT sign-in: an API key is billed per token and past 200k some models cost
//     double, so a key keeps the catalog's window.
//   - model_auto_compact_token_limit: compaction at 80% of that window, so the long window is used before the history
//     is summarised, and what is summarised still leaves room for the work after it.
//   - project_doc_max_bytes: the AGENTS.md chain is cut at 32 KiB by default; a repository with a long one lost its
//     tail without a word.
//   - background_terminal_max_timeout: the longest a command left in the background may be waited for (5 min by default:
//     a build or a test run is often longer).
//   - the MCP servers harnessly hands to Codex: startup_timeout_sec 30 (10 by default, too short for a server started
//     through npx) and tool_timeout_sec 300 (60 by default, while delegate/wait tools wait for another chat).
const (
	DocMaxBytes          = 65536
	TerminalMaxTimeoutMs = 900000
	MCPStartupSec        = 30
	MCPToolSec           = 300
	// compactPercent is the share of the window at which compaction starts.
	compactPercent = 80
)

// userConfig is the person's config.toml decoded; nil when it is missing or does not read (a file that does not read is
// not guessed at: the launch adds nothing that depends on it).
type userConfig map[string]any

func readUserConfig(path string) userConfig {
	if path == "" {
		return userConfig{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return userConfig{}
		}
		return nil
	}
	var m map[string]any
	if _, err := toml.Decode(string(raw), &m); err != nil {
		return nil
	}
	if m == nil {
		m = map[string]any{}
	}
	return m
}

func (u userConfig) has(table []string, key string) bool {
	cur := map[string]any(u)
	for _, t := range table {
		next, ok := cur[t].(map[string]any)
		if !ok {
			return false
		}
		cur = next
	}
	_, ok := cur[key]
	return ok
}

func (u userConfig) str(key string) string {
	s, _ := u[key].(string)
	return s
}

// catalogModel is a row of the models cache Codex keeps next to its config.
type catalogModel struct {
	Slug       string `json:"slug"`
	Visibility string `json:"visibility"`
	Context    int64  `json:"context_window"`
	MaxContext int64  `json:"max_context_window"`
}

type catalogEntry struct {
	mod    time.Time
	size   int64
	models []catalogModel
}

var catalogCache sync.Map // path → catalogEntry

// readCatalog is the model catalog Codex cached in its folder (models_cache.json), read again when the file changes.
func readCatalog(codexDir string) []catalogModel {
	path := filepath.Join(codexDir, "models_cache.json")
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil
	}
	if v, ok := catalogCache.Load(path); ok {
		if e := v.(catalogEntry); e.mod.Equal(info.ModTime()) && e.size == info.Size() {
			return e.models
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc struct {
		Models []catalogModel `json:"models"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	catalogCache.Store(path, catalogEntry{info.ModTime(), info.Size(), doc.Models})
	return doc.Models
}

// subscription: Codex is signed in with a ChatGPT account (a plan, not a per-token key).
func subscription(codexDir string) bool {
	raw, err := os.ReadFile(filepath.Join(codexDir, "auth.json"))
	if err != nil {
		return false
	}
	var a struct {
		Mode   string `json:"auth_mode"`
		Tokens *struct {
			ID string `json:"id_token"`
		} `json:"tokens"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return false
	}
	return a.Tokens != nil && a.Tokens.ID != "" && a.Mode != "apikey"
}

// TunedWindow is the context window and the compaction point a chat of Codex on model is started with by default, in
// tokens as Codex counts them in its config; zeros when the catalog or the sign-in says to leave the model's own.
// configPath is the person's config.toml: a model_context_window or a model_auto_compact_token_limit written there wins,
// so that value is returned as 0 here (nothing of ours). model "" is the one config.toml or the account chooses: the
// smallest maximum of the catalog's visible models then, which every one of them takes.
func TunedWindow(configPath, model string) (window, compactAt int64) {
	cfg := readUserConfig(configPath)
	if cfg == nil {
		return 0, 0
	}
	dir := filepath.Dir(configPath)
	if !subscription(dir) {
		return 0, 0
	}
	if model == "" {
		model = cfg.str("model")
	}
	models := readCatalog(dir)
	var max, base int64
	for _, m := range models {
		if m.Slug == model {
			max, base = m.MaxContext, m.Context
			break
		}
	}
	if max == 0 {
		for _, m := range models {
			if m.Visibility != "list" || m.MaxContext <= 0 {
				continue
			}
			if max == 0 || m.MaxContext < max {
				max = m.MaxContext
			}
			if base == 0 || m.Context < base {
				base = m.Context
			}
		}
	}
	if max <= base || max <= 0 {
		return 0, 0
	}
	if !cfg.has(nil, "model_context_window") {
		window = max
	}
	if !cfg.has(nil, "model_auto_compact_token_limit") {
		compactAt = max * compactPercent / 100 / 1000 * 1000
	}
	return window, compactAt
}

// TunedArgs are the `-c` flags of the defaults above for a launch on model whose MCP servers are named servers.
// configPath is the person's config.toml (the CODEX_HOME of the sign-in the launch uses). own is false for a launch on a
// vendor's backend (another provider: its window is not the catalog's), which gets only the settings not about a model.
func TunedArgs(configPath, model string, servers []string, own bool) []string {
	cfg := readUserConfig(configPath)
	if cfg == nil {
		return nil
	}
	var args []string
	add := func(table []string, key, lit string) {
		if !cfg.has(table, key) {
			args = append(args, "-c", dotted(table, key)+"="+lit)
		}
	}
	if own {
		window, at := TunedWindow(configPath, model)
		if window > 0 {
			add(nil, "model_context_window", strconv.FormatInt(window, 10))
		}
		if at > 0 {
			add(nil, "model_auto_compact_token_limit", strconv.FormatInt(at, 10))
		}
	}
	add(nil, "project_doc_max_bytes", strconv.Itoa(DocMaxBytes))
	add(nil, "background_terminal_max_timeout", strconv.Itoa(TerminalMaxTimeoutMs))
	seen := map[string]bool{}
	for _, s := range servers {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		t := []string{"mcp_servers", s}
		add(t, "startup_timeout_sec", strconv.Itoa(MCPStartupSec))
		add(t, "tool_timeout_sec", strconv.Itoa(MCPToolSec))
	}
	return args
}

func dotted(table []string, key string) string {
	return string(joinDots(append(slices.Clone(table), key)))
}

func joinDots(parts []string) []byte {
	var b []byte
	for i, p := range parts {
		if i > 0 {
			b = append(b, '.')
		}
		b = append(b, p...)
	}
	return b
}
