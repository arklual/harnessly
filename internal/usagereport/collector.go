package usagereport

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

const (
	// rescan is how long a built report is served as it is.
	rescan = 60 * time.Second
	// cacheVersion is the facts file's format, the previous runtime's.
	cacheVersion = 2
)

// Config is where the transcripts and the facts file are, and how the report names its days and projects.
type Config struct {
	// ClaudeRoot is Claude Code's projects folder, CodexRoot Codex's sessions folder.
	ClaudeRoot, CodexRoot string
	// CacheFile keeps the facts of every transcript between runs (the previous runtime reads and writes the same file).
	CacheFile string
	// SQLiteFacts keeps the facts in a database beside CacheFile too (HARNESSLY_USAGE_SQLITE=1 does the same); sqlitefacts.go.
	SQLiteFacts bool
	Projects    *Projects
	Clock       Clock
	// Timezone is the zone the report says its days are in.
	Timezone string
	// Machine is the host name the report carries.
	Machine string
	// Workers read transcripts in parallel on a first pass (GOMAXPROCS when 0).
	Workers int
	// Now is the clock (tests move it); nil is the real one.
	Now func() time.Time
}

type cachedFile struct {
	size    float64
	mtimeMs float64
	facts   *Facts
}

// build is a report being built; done closes when it is.
type build struct {
	done   chan struct{}
	report *Report
	err    error
}

// Collector keeps the facts of the transcripts and the latest report. Concurrent callers share one build.
type Collector struct {
	cfg Config

	mu     sync.Mutex
	order  []string
	files  map[string]*cachedFile
	loaded bool
	// mrKept are the merge request keys kept beside the facts file (mrs.go), by transcript.
	mrKept   map[string]mrEntry
	mrDirty  bool
	report   *Report
	builtAt  time.Time
	building *build
	progress Progress
	// refresh serializes passes over the transcripts.
	refresh sync.Mutex
	// beforeBuild runs as a build starts (tests hold it there).
	beforeBuild func()
	// sql is the facts database, when it is on (sqlitefacts.go).
	sql       *factsDB
	sqlFailed bool
}

// NewCollector is a collector with nothing read yet.
func NewCollector(cfg Config) *Collector {
	if cfg.Projects == nil {
		cfg.Projects = &Projects{}
	}
	return &Collector{cfg: cfg, files: map[string]*cachedFile{}}
}

func (c *Collector) now() time.Time {
	if c.cfg.Now != nil {
		return c.cfg.Now()
	}
	return time.Now()
}

func nowMs(t time.Time) float64 { return float64(t.UnixMilli()) }

// Report is the current report for a person. A build that takes longer than wait is not waited for: the answer is
// then the previous report, or an empty one, marked with how far the pass has got.
func (c *Collector) Report(person string, wait time.Duration) (*Report, error) {
	c.mu.Lock()
	if c.report != nil && c.now().Sub(c.builtAt) < rescan {
		r := c.report.With(person, nil)
		c.mu.Unlock()
		return r, nil
	}
	b := c.building
	if b == nil {
		b = &build{done: make(chan struct{})}
		c.building = b
		go c.rebuild(b)
	}
	c.mu.Unlock()
	if wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-b.done:
		case <-t.C:
		}
	}
	select {
	case <-b.done:
		if b.err != nil {
			return nil, b.err
		}
		return b.report.With(person, nil), nil
	default:
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.progress
	if c.report != nil {
		return c.report.With(person, &p), nil
	}
	return Empty(person, c.cfg.Machine, c.cfg.Timezone, nowMs(c.now()), p), nil
}

func (c *Collector) rebuild(b *build) {
	defer func() {
		if p := recover(); p != nil {
			b.err = fmt.Errorf("usage report: %v", p)
		}
		c.mu.Lock()
		if b.err == nil {
			c.report, c.builtAt = b.report, c.now()
		}
		c.building = nil
		c.mu.Unlock()
		close(b.done)
	}()
	if c.beforeBuild != nil {
		c.beforeBuild()
	}
	started := time.Now()
	c.Refresh()
	b.report, b.err = Build(c.Facts(), "", c.cfg.Machine, c.cfg.Timezone, c.cfg.Clock, nowMs(c.now()))
	if b.err == nil {
		jsonlog.Log("info", "usage report built", jsonlog.F{K: "ms", V: time.Since(started).Milliseconds()}, jsonlog.F{K: "files", V: c.FileCount()})
	}
}

// Facts are the facts of every transcript known, in the order they became known.
func (c *Collector) Facts() []*Facts {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*Facts, 0, len(c.order))
	for _, f := range c.order {
		out = append(out, c.files[f].facts)
	}
	return out
}

// Entry is the facts of one transcript with where they came from.
type Entry struct {
	Path string
	// MtimeMs is the transcript's modification time when its facts were read, epoch ms.
	MtimeMs float64
	Facts   *Facts
}

// Entries are the transcripts known with their facts, in the order they became known. The agent reads them to tell the
// hub of calls and sessions; a subagent's file is told apart by its path.
func (c *Collector) Entries() []Entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Entry, 0, len(c.order))
	for _, f := range c.order {
		e := c.files[f]
		out = append(out, Entry{Path: f, MtimeMs: e.mtimeMs, Facts: e.facts})
	}
	return out
}

// FileCount is how many transcripts have facts.
func (c *Collector) FileCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.order)
}

type transcriptFile struct {
	path, provider string
}

// walk lists the regular files ending in .jsonl under root, down to maxDepth folders below it, a level at a time.
func walk(root string, maxDepth int) []string {
	var out []string
	level := []string{root}
	for depth := 0; len(level) > 0; depth++ {
		var next []string
		for _, dir := range level {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				full := filepath.Join(dir, e.Name())
				switch {
				case e.IsDir():
					if depth < maxDepth {
						next = append(next, full)
					}
				case e.Type().IsRegular() && strings.HasSuffix(full, ".jsonl"):
					out = append(out, full)
				}
			}
		}
		level = next
	}
	return out
}

func (c *Collector) list() []transcriptFile {
	var out []transcriptFile
	// projects/<dir>/<session>.jsonl and projects/<dir>/<session>/subagents/agent-*.jsonl
	for _, f := range walk(c.cfg.ClaudeRoot, 3) {
		out = append(out, transcriptFile{f, "claude"})
	}
	for _, f := range walk(c.cfg.CodexRoot, 4) {
		out = append(out, transcriptFile{f, "codex"})
	}
	return out
}

// loadCache reads the facts file once; a missing, unreadable or other-version file is a first run.
func (c *Collector) loadCache() {
	if c.loaded || c.cfg.CacheFile == "" {
		c.loaded = true
		return
	}
	c.loaded = true
	var order []string
	var files map[string]*cachedFile
	if db := c.sqliteOn(); db != nil {
		order, files = db.load()
	}
	if len(order) == 0 { // the database is off or still empty: the JSON file is the state, as before
		order, files = readFacts(c.cfg.CacheFile)
	}
	for _, name := range order {
		if _, ok := c.files[name]; !ok {
			c.order = append(c.order, name)
		}
		c.files[name] = files[name]
	}
	c.mrKept = readMRs(c.cfg.CacheFile)
	if c.mrKept == nil {
		c.mrKept = map[string]mrEntry{}
	}
	applyMRs(c.files, c.mrKept)
}

// readFacts reads a facts file: the transcripts in the file's order and their facts. A missing, unreadable or
// other-version file has none.
func readFacts(path string) ([]string, map[string]*cachedFile) {
	raw, err := os.ReadFile(path)
	if err != nil || !valid(raw) {
		return nil, nil
	}
	top := objectOf(raw[skipWS(raw, 0):])
	if top == nil || top.num("version") != cacheVersion {
		return nil, nil
	}
	var order []string
	files := map[string]*cachedFile{}
	for _, m := range objectOf(top.raw("files")) {
		name := decodeString(append(append([]byte{'"'}, m.name...), '"'))
		e := objectOf(m.value)
		facts := objectOf(e.raw("facts"))
		if facts == nil {
			continue
		}
		if _, ok := files[name]; !ok {
			order = append(order, name)
		}
		files[name] = &cachedFile{size: e.num("size"), mtimeMs: e.num("mtimeMs"), facts: factsOf(facts)}
	}
	return order, files
}

func (c *Collector) saveCache() {
	if c.cfg.CacheFile == "" {
		return
	}
	var w jw
	w.WriteString(`{"version":2,"files":{`)
	c.mu.Lock()
	if db := c.sqliteOn(); db != nil {
		err := db.save(c.order, c.files)
		recent := !db.jsonAt.IsZero() && c.now().Sub(db.jsonAt) < jsonSnapshotEvery
		if err == nil && recent {
			c.mu.Unlock()
			return
		}
		if err != nil {
			jsonlog.Log("warn", "usage facts database write failed", jsonlog.F{K: "error", V: err.Error()})
		}
		db.jsonAt = c.now()
	}
	for i, name := range c.order {
		f := c.files[name]
		if i > 0 {
			w.WriteByte(',')
		}
		w.key(name)
		w.WriteString(`{"size":`)
		w.num(f.size)
		w.WriteString(`,"mtimeMs":`)
		w.num(f.mtimeMs)
		w.WriteString(`,"facts":`)
		f.facts.write(&w)
		w.WriteByte('}')
	}
	c.mu.Unlock()
	w.WriteString(`}}`)
	if err := os.MkdirAll(filepath.Dir(c.cfg.CacheFile), 0o755); err == nil {
		tmp := fmt.Sprintf("%s.%d.tmp", c.cfg.CacheFile, os.Getpid())
		if err = os.WriteFile(tmp, w.Bytes(), 0o600); err == nil {
			err = os.Rename(tmp, c.cfg.CacheFile)
		}
		if err == nil {
			return
		}
		jsonlog.Log("warn", "usage cache write failed", jsonlog.F{K: "error", V: err.Error()})
		return
	} else {
		jsonlog.Log("warn", "usage cache write failed", jsonlog.F{K: "error", V: err.Error()})
	}
}

type staleFile struct {
	transcriptFile
	size, mtimeMs float64
	// backfill: the facts are known and only the merge requests are looked for; the facts file is not written for it.
	backfill bool
}

// Refresh brings the facts up to date with the transcripts on disk: only files whose size or modification time changed
// are read again, in parallel.
func (c *Collector) Refresh() {
	c.refresh.Lock()
	defer c.refresh.Unlock()
	c.mu.Lock()
	c.loadCache()
	c.mu.Unlock()
	transcripts := c.list()
	cutoffMs := nowMs(c.now()) - WindowDays*daySec*1000
	present := make(map[string]bool, len(transcripts))
	for _, t := range transcripts {
		present[t.path] = true
	}
	dirty := false
	c.mu.Lock()
	kept := c.order[:0:0]
	for _, name := range c.order {
		if present[name] {
			kept = append(kept, name)
		} else {
			delete(c.files, name)
			delete(c.mrKept, name)
			c.mrDirty = true
			dirty = true
		}
	}
	c.order = kept
	var stale []staleFile
	for _, t := range transcripts {
		st, err := os.Stat(t.path)
		if err != nil {
			continue
		}
		mtime := jscompat.TimeMs(st.ModTime())
		if mtime < cutoffMs {
			continue
		}
		size := float64(st.Size())
		if f := c.files[t.path]; f != nil && f.size == size && f.mtimeMs == mtime {
			continue
		}
		stale = append(stale, staleFile{transcriptFile: t, size: size, mtimeMs: mtime})
	}
	// Transcripts known from before merge requests were looked for are read again a few at a time, newest first.
	if c.cfg.CacheFile != "" {
		var old []staleFile
		inStale := make(map[string]bool, len(stale))
		for _, s := range stale {
			inStale[s.path] = true
		}
		toolCutoffMs := nowMs(c.now()) - toolBackfillDays*daySec*1000
		for _, t := range transcripts {
			f := c.files[t.path]
			if f == nil || f.facts == nil || inStale[t.path] || f.mtimeMs < cutoffMs {
				continue
			}
			if f.facts.MRKeys != nil && (f.facts.ToolCalls != nil || f.mtimeMs < toolCutoffMs) {
				continue
			}
			old = append(old, staleFile{transcriptFile: t, size: f.size, mtimeMs: f.mtimeMs, backfill: true})
		}
		sort.Slice(old, func(i, j int) bool { return old[i].mtimeMs > old[j].mtimeMs })
		if len(old) > backfillPerPass {
			old = old[:backfillPerPass]
		}
		stale = append(stale, old...)
	}
	c.progress = Progress{Total: len(stale)}
	c.mu.Unlock()

	results := make([]*Facts, len(stale))
	workers := c.cfg.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	var next sync.Mutex
	at := 0
	var wg sync.WaitGroup
	for w := 0; w < workers && w < len(stale); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				next.Lock()
				i := at
				at++
				next.Unlock()
				if i >= len(stale) {
					return
				}
				s := stale[i]
				var f *Facts
				var err error
				if s.provider == "claude" {
					f, err = c.cfg.Projects.ParseClaude(s.path)
				} else {
					f, err = c.cfg.Projects.ParseCodex(s.path)
				}
				if err != nil {
					jsonlog.Log("warn", "transcript parse failed", jsonlog.F{K: "error", V: err.Error()}, jsonlog.F{K: "file", V: s.path})
				}
				results[i] = f
				c.mu.Lock()
				c.progress.Done++
				c.mu.Unlock()
			}
		}()
	}
	wg.Wait()
	c.mu.Lock()
	for i, s := range stale {
		if results[i] == nil {
			continue
		}
		if _, ok := c.files[s.path]; !ok {
			c.order = append(c.order, s.path)
		}
		// The calls are kept summed, as the side file holds them: the same facts whether parsed now or read back.
		sums := sumTools(results[i].ToolCalls)
		results[i].ToolCalls = callsOf(sums)
		if s.backfill {
			// Only the merge requests and the tool calls are taken; the facts already known stay as they were.
			if old := c.files[s.path]; old != nil && old.facts != nil {
				old.facts.MRKeys = results[i].MRKeys
				old.facts.ToolCalls = results[i].ToolCalls
				old.facts.SkillsListed = results[i].SkillsListed
				results[i] = old.facts
			}
		} else {
			c.files[s.path] = &cachedFile{size: s.size, mtimeMs: s.mtimeMs, facts: results[i]}
		}
		if c.cfg.CacheFile != "" {
			if results[i].MRKeys == nil {
				results[i].MRKeys = []string{}
			}
			c.mrKept[s.path] = mrEntry{Size: s.size, MtimeMs: s.mtimeMs, Keys: results[i].MRKeys, Tools: sums, Listed: results[i].SkillsListed}
			c.mrDirty = true
		}
		if !s.backfill {
			dirty = true
		}
	}
	mrDirty := c.mrDirty
	c.mrDirty = false
	c.mu.Unlock()
	if dirty {
		c.saveCache()
	}
	if mrDirty {
		c.mu.Lock()
		kept := make(map[string]mrEntry, len(c.mrKept))
		for k, v := range c.mrKept {
			kept[k] = v
		}
		c.mu.Unlock()
		writeMRs(c.cfg.CacheFile, kept)
	}
}

// DefaultOwner is the person a report is for when the session names none: the user at this host.
func DefaultOwner(host string) string {
	name := ""
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	return name + "@" + host
}
