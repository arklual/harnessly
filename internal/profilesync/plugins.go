package profilesync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// The plugins of the profile (design-sync's internal/claude, carried over): the settings sync, the plugins they enable
// are installed and kept current on each machine with the official CLI, and the open Claude tabs reload them
// (pluginreload reads the mark Mark leaves).

// Reconciler installs and updates one person's plugins with the official CLI (`claude plugin …`), as that person.
type Reconciler struct {
	Home   string
	Claude string // the CLI; "" = `claude` on PATH
	Log    *slog.Logger

	mu       sync.Mutex
	lastHead map[string]string
}

func (r *Reconciler) bin() string {
	if r.Claude != "" {
		return r.Claude
	}
	return "claude"
}

func (r *Reconciler) run(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.bin(), args...)
	cmd.Dir = r.Home
	// Plugin commands touch neither the subscription nor a session; keep the CLI from updating itself meanwhile.
	cmd.Env = append(os.Environ(), "HOME="+r.Home, "DISABLE_AUTOUPDATER=1", "CI=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("claude %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(tail(out, 300)))
	}
	return out, nil
}

func tail(b []byte, n int) string {
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return string(b)
}

// enabled is what settings.json enables.
func (r *Reconciler) enabled() ([]string, error) {
	b, err := os.ReadFile(filepath.Join(r.Home, ".claude", "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil // a profile with no settings enables no plugins
	}
	if err != nil {
		return nil, err
	}
	var s struct {
		EnabledPlugins map[string]bool `json:"enabledPlugins"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	var out []string
	for id, on := range s.EnabledPlugins {
		if on && strings.Contains(id, "@") {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (r *Reconciler) marketplaceHeads() map[string]string {
	heads := map[string]string{}
	dir := filepath.Join(r.Home, ".claude", "plugins", "marketplaces")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out, err := exec.Command("git", "-C", filepath.Join(dir, e.Name()), "rev-parse", "HEAD").Output()
		if err == nil {
			heads[e.Name()] = strings.TrimSpace(string(out))
		}
	}
	return heads
}

type installed struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Enabled bool   `json:"enabled"`
}

// Reconcile refreshes the marketplaces, installs enabled plugins that are missing, updates enabled plugins of every
// marketplace that moved, and reports whether anything changed.
func (r *Reconciler) Reconcile(ctx context.Context) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	want, err := r.enabled()
	if err != nil {
		return false, err
	}
	before := r.lastHead
	if before == nil {
		before = r.marketplaceHeads()
	}
	if _, err := r.run(ctx, 3*time.Minute, "plugin", "marketplace", "update"); err != nil {
		r.Log.Warn("marketplace update", "err", err)
	}
	after := r.marketplaceHeads()
	r.lastHead = after
	out, err := r.run(ctx, 2*time.Minute, "plugin", "list", "--json")
	if err != nil {
		return false, err
	}
	var list []installed
	if err := json.Unmarshal(out, &list); err != nil {
		return false, fmt.Errorf("plugin list: %w", err)
	}
	have := map[string]installed{}
	for _, p := range list {
		have[p.ID] = p
	}
	changed := false
	moved := 0
	for m, h := range after {
		if before[m] != h {
			moved++
		}
	}
	defer func() {
		r.Log.Info("plugins checked", "enabled", len(want), "installed", len(list), "marketplaces_moved", moved, "changed", changed)
	}()
	for _, id := range want {
		mkt := id[strings.LastIndex(id, "@")+1:]
		p, ok := have[id]
		switch {
		case !ok:
			if _, err := r.run(ctx, 5*time.Minute, "plugin", "install", id); err != nil {
				r.Log.Warn("plugin install", "plugin", id, "err", err)
				continue
			}
			r.Log.Info("plugin installed", "plugin", id)
			changed = true
		case before[mkt] != after[mkt]:
			if _, err := r.run(ctx, 5*time.Minute, "plugin", "update", id); err != nil {
				r.Log.Warn("plugin update", "plugin", id, "err", err)
				continue
			}
			if q, err := r.run(ctx, time.Minute, "plugin", "list", "--json"); err == nil {
				var now []installed
				if json.Unmarshal(q, &now) == nil {
					for _, n := range now {
						if n.ID == id && n.Version != p.Version {
							r.Log.Info("plugin updated", "plugin", id, "from", p.Version, "to", n.Version)
							changed = true
						}
					}
				}
			}
		}
	}
	return changed, nil
}

// Mark tells the platform that plugins or skills changed: it reloads them in idle Claude tabs. The file
// is claude-reload.json in the profile's state folder (dir, absolute), `{"plugins": ms, "skills": ms}`, written
// atomically.
func Mark(dir string, plugins, skills bool) error {
	if !plugins && !skills {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file := filepath.Join(dir, "claude-reload.json")
	cur := map[string]int64{}
	if b, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(b, &cur)
	}
	now := time.Now().UnixMilli()
	if plugins {
		cur["plugins"] = now
	}
	if skills {
		cur["skills"] = now
	}
	b, _ := json.Marshal(cur)
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}
