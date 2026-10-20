package usagereport

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/jscompat"
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
	Projects  *Projects
	Clock     Clock
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

	mu       sync.Mutex
	order    []string
	files    map[string]*cachedFile
	loaded   bool
	report   *Report
	builtAt  time.Time
	building *build
	progress Progress
	// refresh serializes passes over the transcripts.
	refresh sync.Mutex
	// beforeBuild runs as a build starts (tests hold it there).
	beforeBuild func()
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
		agent.Log("info", "usage report built", agent.F{K: "ms", V: time.Since(started).Milliseconds()}, agent.F{K: "files", V: c.FileCount()})
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
	raw, err := os.ReadFile(c.cfg.CacheFile)
	if err != nil {
		return
	}
	if !valid(raw) {
		return
	}
	top := objectOf(raw[skipWS(raw, 0):])
	if top == nil || top.num("version") != cacheVersion {
		return
	}
	for _, m := range objectOf(top.raw("files")) {
		name := decodeString(append(append([]byte{'"'}, m.name...), '"'))
		e := objectOf(m.value)
		facts := objectOf(e.raw("facts"))
		if facts == nil {
			continue
		}
		if _, ok := c.files[name]; !ok {
			c.order = append(c.order, name)
		}
		c.files[name] = &cachedFile{size: e.num("size"), mtimeMs: e.num("mtimeMs"), facts: factsOf(facts)}
	}
}

func (c *Collector) saveCache() {
	if c.cfg.CacheFile == "" {
		return
	}
	var w jw
	w.WriteString(`{"version":2,"files":{`)
	c.mu.Lock()
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
		agent.Log("warn", "usage cache write failed", agent.F{K: "error", V: err.Error()})
		return
	} else {
		agent.Log("warn", "usage cache write failed", agent.F{K: "error", V: err.Error()})
	}
}

type staleFile struct {
	transcriptFile
	size, mtimeMs float64
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
		stale = append(stale, staleFile{t, size, mtime})
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
					agent.Log("warn", "transcript parse failed", agent.F{K: "error", V: err.Error()}, agent.F{K: "file", V: s.path})
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
		c.files[s.path] = &cachedFile{size: s.size, mtimeMs: s.mtimeMs, facts: results[i]}
		dirty = true
	}
	c.mu.Unlock()
	if dirty {
		c.saveCache()
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
