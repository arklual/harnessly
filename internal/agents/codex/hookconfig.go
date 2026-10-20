package codex

import (
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

// HookConfig builds the `-c hooks.<Event>=[…]` flags of a Codex launch: the runtime's hook script first, then the
// person's own valid entries for that event from their config.toml (read only, re-read when it changes). Codex gets
// its hooks per launch; no file of the person's is written.
type HookConfig struct {
	// Script is the hook script Codex runs for every event.
	Script string
	// UserConfig is the person's ~/.codex/config.toml.
	UserConfig string

	mu    sync.Mutex
	cache *userHooks
}

type userHooks struct {
	mtime      time.Time
	byEvent    map[string][]any
	parseError bool
}

// tomlValue is a decoded TOML table whose keys keep their file order.
type tomlTable struct {
	keys []string
	m    map[string]any
}

// ordered rebuilds decoded TOML values with each table's keys in the order they first appear at that path.
func ordered(v any, path string, order map[string]int) any {
	switch x := v.(type) {
	case map[string]any:
		t := tomlTable{m: map[string]any{}}
		for k, e := range x {
			t.keys = append(t.keys, k)
			t.m[k] = ordered(e, path+"\x00"+k, order)
		}
		sort.SliceStable(t.keys, func(i, j int) bool {
			oi, iok := order[path+"\x00"+t.keys[i]]
			oj, jok := order[path+"\x00"+t.keys[j]]
			if iok != jok {
				return iok
			}
			if oi != oj {
				return oi < oj
			}
			return t.keys[i] < t.keys[j]
		})
		return t
	case []map[string]any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = ordered(e, path, order)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = ordered(e, path, order)
		}
		return out
	}
	return v
}

func isHookCommand(v any) bool {
	t, ok := v.(tomlTable)
	if !ok {
		return false
	}
	_, isStr := t.m["command"].(string)
	return t.m["type"] == "command" && isStr
}

func isHookEntry(v any) bool {
	t, ok := v.(tomlTable)
	if !ok {
		return false
	}
	hooks, ok := t.m["hooks"].([]any)
	if !ok {
		return false
	}
	if m, present := t.m["matcher"]; present {
		if _, isStr := m.(string); !isStr {
			return false
		}
	}
	for _, h := range hooks {
		if !isHookCommand(h) {
			return false
		}
	}
	return true
}

func (h *HookConfig) userEntries() (*userHooks, error) {
	st, err := os.Stat(h.UserConfig)
	if err != nil {
		h.cache = nil
		return &userHooks{byEvent: map[string][]any{}}, nil
	}
	if h.cache != nil && h.cache.mtime.Equal(st.ModTime()) {
		return h.cache, nil
	}
	raw, err := os.ReadFile(h.UserConfig)
	if err != nil {
		return nil, err
	}
	u := &userHooks{mtime: st.ModTime(), byEvent: map[string][]any{}}
	var doc map[string]any
	md, err := toml.Decode(string(raw), &doc)
	if err != nil {
		u.parseError = true
	} else {
		order := map[string]int{}
		for i, k := range md.Keys() {
			p := strings.Join(k, "\x00")
			if _, seen := order["\x00"+p]; !seen {
				order["\x00"+p] = i
			}
		}
		if hooks, ok := ordered(doc, "", order).(tomlTable).m["hooks"].(tomlTable); ok {
			for _, ev := range HookEvents {
				list, ok := hooks.m[ev].([]any)
				if !ok {
					continue
				}
				var valid []any
				for _, e := range list {
					if isHookEntry(e) {
						valid = append(valid, e)
					}
				}
				if len(valid) > 0 {
					u.byEvent[ev] = valid
				}
			}
		}
	}
	h.cache = u
	return u, nil
}

// tomlInline writes a value as an inline TOML value the way the previous runtime did (strings as JSON strings).
func tomlInline(v any) string {
	switch x := v.(type) {
	case nil:
		return `""`
	case string:
		return string(nodejson.Marshal(x))
	case bool:
		return strconv.FormatBool(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return "0"
		}
		return agents.JSString(x)
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = tomlInline(e)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case tomlTable:
		parts := make([]string, len(x.keys))
		for i, k := range x.keys {
			parts[i] = k + "=" + tomlInline(x.m[k])
		}
		return "{" + strings.Join(parts, ",") + "}"
	case time.Time:
		return "{}"
	}
	return `""`
}

// Flags are `-c hooks.<Event>=[ours, theirs…]` for every event; parseError reports a config.toml that could not be
// read as TOML (only the runtime's hook applies then, and the person should be told once).
func (h *HookConfig) Flags() (args []string, parseError bool, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	u, err := h.userEntries()
	if err != nil {
		return nil, false, err
	}
	ours := tomlTable{keys: []string{"matcher", "hooks"}, m: map[string]any{
		"matcher": ".*",
		"hooks":   []any{tomlTable{keys: []string{"type", "command"}, m: map[string]any{"type": "command", "command": h.Script}}},
	}}
	for _, ev := range HookEvents {
		entries := append([]any{ours}, u.byEvent[ev]...)
		args = append(args, "-c", "hooks."+ev+"="+tomlInline(entries))
	}
	return args, u.parseError, nil
}
