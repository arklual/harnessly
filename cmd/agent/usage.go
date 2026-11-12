package main

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/usagefeed"
	"github.com/ly-solutions/harnessly/internal/usagereport"
)

const (
	// usageWorkers is how many transcripts the agent reads at once when it has to read them itself.
	usageWorkers = 2
	// ownFacts is the agent's own facts file, kept only while no runtime keeps one.
	ownFacts = "usage-agent-v2.json"
	// factsSuffix ends the name of a runtime's facts file (RUNTIME_USAGE_CACHE_FILE, usage-v2.json by default).
	factsSuffix = "usage-v2.json"
	// storedMaxAge: a runtime's facts file is used while the runtime passed over the transcripts this recently.
	storedMaxAge = 10 * time.Minute
)

// usageFeed writes the spend facts the hub asks for. The facts come from the facts file the machine's runtime keeps,
// so the transcripts are parsed once per machine; while no runtime keeps one, the agent reads the transcripts itself.
// It lives here, not in internal/agent: the usage report imports the agent's package.
func usageFeed(d agent.UsageDeps) (agent.UsageFeed, error) {
	claudeDir := d.Getenv("claude", "CLAUDE_CONFIG_DIR")
	if claudeDir == "" {
		claudeDir = filepath.Join(d.Home, ".claude")
	}
	codexDir := d.Getenv("codex", "CODEX_HOME")
	if codexDir == "" {
		codexDir = filepath.Join(d.Home, ".codex")
	}
	statsDir := filepath.Join(d.DataDir, "stats")
	runtimeDir := d.RuntimeDir
	if runtimeDir == "" {
		runtimeDir = d.DataDir
	}
	src := &usageSource{
		statsDir: filepath.Join(runtimeDir, "stats"),
		named:    os.Getenv("AGENT_USAGE_FACTS_FILE"),
		stored:   map[string]*usagereport.Stored{},
		own: func() *usagereport.Collector {
			return usagereport.NewCollector(usagereport.Config{
				ClaudeRoot: filepath.Join(claudeDir, "projects"),
				CodexRoot:  filepath.Join(codexDir, "sessions"),
				// The agent's own facts file: the runtime names projects by its own settings and the two must not
				// overwrite each other's reading.
				CacheFile: filepath.Join(statsDir, ownFacts),
				Projects:  &usagereport.Projects{},
				Timezone:  "UTC",
				Machine:   d.Hostname,
				// Two readers: the agent lives under a small memory cap, and a transcript's big lines are garbage the
				// collector has to catch up with; the hub does not need the facts within seconds.
				Workers: usageWorkers,
			})
		},
		now: time.Now,
	}
	local := usagefeed.Local{Home: d.Home, Getenv: d.Getenv}
	return usagefeed.New(usagefeed.Config{
		Outbox:    d.Box,
		Source:    src,
		StatePath: usagefeed.StatePath(d.Dir),
		Identity:  local.Identity,
		// counts of the skills a session was offered and called; off until the fleet flag turns it on.
		SkillMetrics: os.Getenv("AGENT_USAGE_SKILL_METRICS") == "on",
	})
}

// usageSource is the runtime's facts file while a runtime keeps it current, the agent's own reading otherwise. Only
// one of the two is held at a time. The feed calls it from one pass at a time.
type usageSource struct {
	// statsDir is the runtime's stats folder, where its facts file is.
	statsDir string
	// named is the runtime's facts file the deployment names (AGENT_USAGE_FACTS_FILE); empty: found in statsDir.
	named  string
	stored map[string]*usagereport.Stored
	own    func() *usagereport.Collector
	now    func() time.Time

	col *usagereport.Collector
	cur usagefeed.Source
}

// candidates are the runtime facts files that may be read, the named one alone when there is one.
func (u *usageSource) candidates() []string {
	if u.named != "" {
		if filepath.IsAbs(u.named) {
			return []string{u.named}
		}
		return []string{filepath.Join(u.statsDir, u.named)}
	}
	entries, err := os.ReadDir(u.statsDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.Type().IsRegular() && strings.HasSuffix(name, factsSuffix) && name != ownFacts {
			out = append(out, filepath.Join(u.statsDir, name))
		}
	}
	sort.Strings(out)
	return out
}

// alive is the runtime facts file kept most recently, nil when none is kept now.
func (u *usageSource) alive() *usagereport.Stored {
	var best *usagereport.Stored
	var bestAt time.Time
	now := u.now()
	for _, path := range u.candidates() {
		st, err := os.Stat(path + usagereport.AliveSuffix)
		if err != nil || now.Sub(st.ModTime()) > storedMaxAge {
			continue
		}
		if best == nil || st.ModTime().After(bestAt) {
			s := u.stored[path]
			if s == nil {
				s = &usagereport.Stored{Path: path}
				u.stored[path] = s
			}
			best, bestAt = s, st.ModTime()
		}
	}
	return best
}

func (u *usageSource) Refresh() {
	if s := u.alive(); s != nil {
		if u.col != nil {
			agent.Log("info", "usage facts: reading the runtime's facts file", agent.F{K: "file", V: s.Path})
			u.col = nil
			debug.FreeOSMemory()
		}
		for p, o := range u.stored {
			if o != s {
				o.Forget()
				delete(u.stored, p)
			}
		}
		u.cur = s
		s.Refresh()
		return
	}
	for p, o := range u.stored {
		o.Forget()
		delete(u.stored, p)
	}
	if u.col == nil {
		agent.Log("info", "usage facts: no runtime keeps a facts file; reading the transcripts")
		u.col = u.own()
	}
	u.cur = u.col
	u.col.Refresh()
}

func (u *usageSource) Entries() []usagereport.Entry {
	if u.cur == nil {
		return nil
	}
	return u.cur.Entries()
}
