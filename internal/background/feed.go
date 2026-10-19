package background

import (
	"encoding/json"
	"sync"
	"time"
)

// Source is where a tab's background work is read from.
type Source struct {
	TmuxSession string
	Transcript  string // "" when the tab has none yet
	Provider    string
}

// Deps are what the feed reads with and publishes to.
type Deps struct {
	ReadTasks func(transcript string) []Task
	// ReadPane is a plain capture of the agent's pane: its footer is the authority on shells and monitors still running.
	ReadPane  func(tmuxSession string) (string, bool)
	Reconcile func(tasks []Task, pane string, ok bool) []Task
	Publish   func(tabID string, tasks []Task)
	Forget    func(transcript string)
}

// Timings of the feed; the zero value is the production one.
type Timings struct {
	// MinInterval: one tab reads its transcript at most this often, however fast the transcript grows.
	MinInterval time.Duration
	// PaneRefresh: the footer is captured when the transcript's tasks changed, after an event, else at most this often.
	PaneRefresh time.Duration
	// Recheck: tabs with work are read again this often although nothing was written — a shell can end, a wakeup come
	// due and an output file go away without a line in the transcript.
	Recheck time.Duration
	// MaxReads: a restart finds every live agent at once; their cold tails are read a few at a time.
	MaxReads int
}

func (t Timings) withDefaults() Timings {
	if t.MinInterval == 0 {
		t.MinInterval = 3 * time.Second
	}
	if t.PaneRefresh == 0 {
		t.PaneRefresh = 15 * time.Second
	}
	if t.Recheck == 0 {
		t.Recheck = 10 * time.Second
	}
	if t.MaxReads == 0 {
		t.MaxReads = 2
	}
	return t
}

type tabFeed struct {
	source        Source
	tasks         []Task
	transcriptKey *string
	pane          string
	paneOK        bool
	paneAt        time.Time
	paneWanted    bool
	lastRunAt     time.Time
	running       bool
	queued        bool
	timer         *time.Timer
}

// Feed keeps the background work of every live agent tab, recomputed when something happened to the tab (a transcript
// write, a hook, a state change) and published only when it changed.
type Feed struct {
	deps    Deps
	timing  Timings
	now     func() time.Time
	mu      sync.Mutex
	tabs    map[string]*tabFeed
	recheck *time.Ticker
	stopRe  chan struct{}
	slots   chan struct{}
}

// NewFeed makes a feed; now is the clock (nil for the real one).
func NewFeed(deps Deps, timing Timings, now func() time.Time) *Feed {
	if now == nil {
		now = time.Now
	}
	timing = timing.withDefaults()
	return &Feed{deps: deps, timing: timing, now: now, tabs: map[string]*tabFeed{}, slots: make(chan struct{}, timing.MaxReads)}
}

// Touch says something happened to the tab. A transcript write alone captures the footer only when the transcript's
// tasks changed; any other event (hook, state change, new tab) captures it too.
func (f *Feed) Touch(tabID string, source Source, event bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tab := f.tabs[tabID]
	if tab == nil {
		tab = &tabFeed{source: source, tasks: []Task{}, paneWanted: true}
		f.tabs[tabID] = tab
	} else {
		if tab.source.Transcript != "" && tab.source.Transcript != source.Transcript && f.deps.Forget != nil {
			f.deps.Forget(tab.source.Transcript)
		}
		tab.source = source
	}
	if event {
		tab.paneWanted = true
	}
	f.schedule(tabID, tab)
}

// Remove: the agent is gone or the tab closed, so its background work is over as far as the page is concerned.
func (f *Feed) Remove(tabID string) {
	f.mu.Lock()
	tab := f.tabs[tabID]
	if tab == nil {
		f.mu.Unlock()
		return
	}
	if tab.timer != nil {
		tab.timer.Stop()
	}
	delete(f.tabs, tabID)
	f.syncRecheck()
	f.mu.Unlock()
	if tab.source.Transcript != "" && f.deps.Forget != nil {
		f.deps.Forget(tab.source.Transcript)
	}
	if len(tab.tasks) > 0 {
		f.deps.Publish(tabID, []Task{})
	}
}

// TabIDs are the tabs the feed follows.
func (f *Feed) TabIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]string, 0, len(f.tabs))
	for id := range f.tabs {
		ids = append(ids, id)
	}
	return ids
}

// SourceOf is where a followed tab is read from.
func (f *Feed) SourceOf(tabID string) (Source, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if tab := f.tabs[tabID]; tab != nil {
		return tab.source, true
	}
	return Source{}, false
}

// Snapshot is every tab with background work, for a client that just connected.
func (f *Feed) Snapshot() map[string][]Task {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string][]Task{}
	for id, tab := range f.tabs {
		if len(tab.tasks) > 0 {
			out[id] = tab.tasks
		}
	}
	return out
}

// Stop forgets every tab.
func (f *Feed) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, tab := range f.tabs {
		if tab.timer != nil {
			tab.timer.Stop()
		}
	}
	f.tabs = map[string]*tabFeed{}
	f.syncRecheck()
}

func (f *Feed) schedule(tabID string, tab *tabFeed) {
	if tab.running {
		tab.queued = true
		return
	}
	if tab.timer != nil {
		return
	}
	wait := max(0, tab.lastRunAt.Add(f.timing.MinInterval).Sub(f.now()))
	tab.timer = time.AfterFunc(wait, func() {
		f.mu.Lock()
		tab.timer = nil
		f.mu.Unlock()
		f.run(tabID, tab)
	})
}

func key(tasks []Task) string {
	raw, _ := json.Marshal(tasks)
	return string(raw)
}

func (f *Feed) run(tabID string, tab *tabFeed) {
	f.mu.Lock()
	if f.tabs[tabID] != tab {
		f.mu.Unlock()
		return
	}
	tab.running = true
	tab.lastRunAt = f.now()
	src := tab.source
	f.mu.Unlock()

	f.slots <- struct{}{}
	tasks := []Task{}
	if src.Transcript != "" && src.Provider != "codex" {
		tasks = f.deps.ReadTasks(src.Transcript)
	}
	k := key(tasks)
	f.mu.Lock()
	now := f.now()
	capture := tab.paneWanted || tab.transcriptKey == nil || k != *tab.transcriptKey || now.Sub(tab.paneAt) >= f.timing.PaneRefresh
	if capture {
		tab.paneWanted, tab.paneAt = false, now
	}
	f.mu.Unlock()
	if capture {
		pane, ok := f.deps.ReadPane(src.TmuxSession)
		f.mu.Lock()
		tab.pane, tab.paneOK = pane, ok
		f.mu.Unlock()
	}
	<-f.slots

	f.mu.Lock()
	tab.transcriptKey = &k
	next := f.deps.Reconcile(tasks, tab.pane, tab.paneOK)
	publish := f.tabs[tabID] == tab && key(tab.tasks) != key(next)
	if publish {
		tab.tasks = next
	}
	tab.running = false
	if tab.queued && f.tabs[tabID] == tab {
		tab.queued = false
		f.schedule(tabID, tab)
	}
	f.syncRecheck()
	f.mu.Unlock()
	if publish {
		f.deps.Publish(tabID, next)
	}
}

// syncRecheck runs the recheck only while some tab has work. Called with the lock held.
func (f *Feed) syncRecheck() {
	busy := false
	for _, tab := range f.tabs {
		if len(tab.tasks) > 0 {
			busy = true
			break
		}
	}
	if busy && f.recheck == nil {
		f.recheck = time.NewTicker(f.timing.Recheck)
		stop, ticks := make(chan struct{}), f.recheck.C
		f.stopRe = stop
		go func() {
			for {
				select {
				case <-stop:
					return
				case <-ticks:
					f.mu.Lock()
					for id, tab := range f.tabs {
						if len(tab.tasks) > 0 {
							tab.paneWanted = true
							f.schedule(id, tab)
						}
					}
					f.mu.Unlock()
				}
			}
		}()
	} else if !busy && f.recheck != nil {
		f.recheck.Stop()
		close(f.stopRe)
		f.recheck, f.stopRe = nil, nil
	}
}

// ReconcileWithFooter is the production Reconcile: the transcript list trimmed and padded by the footer.
func ReconcileWithFooter(tasks []Task, pane string, ok bool) []Task {
	if !ok {
		return tasks
	}
	return Reconcile(tasks, ParseFooter(pane))
}
