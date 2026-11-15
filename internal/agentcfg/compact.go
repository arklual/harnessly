package agentcfg

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The auto-compact settings a chat's agent runs with, for the context ring: Claude's autoCompactWindow (the window it
// counts its compaction from, below the model's own) and Codex's model_auto_compact_token_limit (the tokens it compacts
// at). The files are read again only when their modification time or size changes: the ring asks every few seconds.

// Claude Code takes an autoCompactWindow from 100k to 1M tokens; a value outside is refused and the model's window used.
const (
	compactWindowMin = 100_000
	compactWindowMax = 1_000_000
)

// claudeCompactEnv takes precedence over every settings file.
const claudeCompactEnv = "CLAUDE_CODE_AUTO_COMPACT_WINDOW"

// Compact is what a chat's agent is set to compact at; zero fields: nothing is set.
type Compact struct {
	// Window is the window Claude counts its compaction from (autoCompactWindow), in tokens.
	Window int64
	// At is the tokens Codex compacts at (model_auto_compact_token_limit).
	At int64
}

type cachedFile struct {
	mod  time.Time
	size int64
	obj  *jsvalue.Object // Claude's settings.json
	toml map[string]any  // Codex's config.toml
}

// fileCache keeps parsed settings files by path, valid while the file's mtime and size stay.
type fileCache struct {
	mu    sync.Mutex
	files map[string]cachedFile
}

func (c *fileCache) get(path string, parse func(raw []byte) cachedFile) cachedFile {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return cachedFile{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if f, ok := c.files[path]; ok && f.mod.Equal(info.ModTime()) && f.size == info.Size() {
		return f
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return cachedFile{}
	}
	f := parse(raw)
	f.mod, f.size = info.ModTime(), info.Size()
	if c.files == nil {
		c.files = map[string]cachedFile{}
	}
	c.files[path] = f
	return f
}

// CompactOf is what an agent of the machine compacts at in a chat of the folder cwd on the model.
func (s *Store) CompactOf(p wire.Provider, cwd, model string) Compact {
	switch p {
	case wire.ProviderClaude:
		return Compact{Window: s.claudeCompactWindow(cwd, model)}
	case wire.ProviderCodex:
		return Compact{At: s.codexCompactAt()}
	}
	return Compact{}
}

// claudeCompactWindow follows Claude Code: the environment first, then the settings of the user, the project and the
// project's local file, a later one over an earlier; in one file modelSettings.<model>.autoCompactWindow replaces the
// top-level key for that model. "auto" (or nothing) is the model's own window: 0.
func (s *Store) claudeCompactWindow(cwd, model string) int64 {
	if v := strings.TrimSpace(s.cfg.Getenv(claudeCompactEnv)); v != "" {
		if n, ok := compactTokens(v); ok {
			return n
		}
	}
	files := []string{filepath.Join(s.cfg.ClaudeDir, "settings.json")}
	if cwd != "" {
		files = append(files, filepath.Join(cwd, ".claude", "settings.json"), filepath.Join(cwd, ".claude", "settings.local.json"))
	}
	var window int64
	for _, path := range files {
		obj := s.compactFiles.get(path, func(raw []byte) cachedFile {
			o, err := jsonObject(raw)
			if err != nil {
				return cachedFile{}
			}
			return cachedFile{obj: o}
		}).obj
		if obj == nil {
			continue
		}
		v, ok := modelCompact(obj, model)
		if !ok {
			v, ok = obj.Get("autoCompactWindow")
		}
		if !ok {
			continue
		}
		if n, valid := compactTokens(v); valid {
			window = n
		}
	}
	return window
}

// modelCompact is modelSettings.<key>.autoCompactWindow of the file for the model: the key is the canonical name, which
// also matches the model's dated and [1m] spellings.
func modelCompact(obj *jsvalue.Object, model string) (any, bool) {
	ms, ok := obj.Get("modelSettings")
	if !ok {
		return nil, false
	}
	mo, ok := ms.(*jsvalue.Object)
	if !ok || model == "" {
		return nil, false
	}
	bare := strings.TrimSuffix(model, "[1m]")
	for _, k := range mo.Keys() {
		if k != bare && !strings.HasPrefix(bare, k+"-") {
			continue
		}
		if o, ok := mo.Get(k); ok {
			if per, ok := o.(*jsvalue.Object); ok {
				if v, ok := per.Get("autoCompactWindow"); ok {
					return v, true
				}
			}
		}
	}
	return nil, false
}

// compactTokens reads an autoCompactWindow value: a number of tokens, or a text such as "400000", "400k", "1m" or bare
// thousands ("400"). "auto" is valid and means the model's window (0). Anything else, or out of range, is not valid.
func compactTokens(v any) (int64, bool) {
	var n float64
	switch x := v.(type) {
	case float64:
		n = x
	case int64:
		n = float64(x)
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		if s == "auto" {
			return 0, true
		}
		mult := 1.0
		switch {
		case strings.HasSuffix(s, "k"):
			mult, s = 1_000, strings.TrimSuffix(s, "k")
		case strings.HasSuffix(s, "m"):
			mult, s = 1_000_000, strings.TrimSuffix(s, "m")
		}
		f, err := strconv.ParseFloat(strings.ReplaceAll(s, "_", ""), 64)
		if err != nil {
			return 0, false
		}
		if mult == 1 && f > 0 && f <= compactWindowMax/1000 {
			mult = 1_000
		}
		n = f * mult
	default:
		return 0, false
	}
	t := int64(n)
	if t < compactWindowMin || t > compactWindowMax {
		return 0, false
	}
	return t, true
}

// codexCompactAt is model_auto_compact_token_limit of Codex's config.toml; 0 when it is not set.
func (s *Store) codexCompactAt() int64 {
	path := filepath.Join(s.cfg.CodexDir, "config.toml")
	m := s.compactFiles.get(path, func(raw []byte) cachedFile {
		t, err := tomlMap(string(raw))
		if err != nil {
			return cachedFile{}
		}
		return cachedFile{toml: t}
	}).toml
	v, ok := tomlDig(m, nil, "model_auto_compact_token_limit")
	if !ok {
		return 0
	}
	if n, ok := v.(int64); ok && n > 0 {
		return n
	}
	return 0
}
