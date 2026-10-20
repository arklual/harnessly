package status

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/background"
	"github.com/ly-solutions/harnessly/internal/history"
	"github.com/ly-solutions/harnessly/internal/sockets"
)

const (
	pollSmall, pollMedium, pollLarge = 30 * time.Second, 45 * time.Second, 60 * time.Second
	tabsMedium, tabsLarge            = 11, 21
	queueSettle                      = 700 * time.Millisecond
	queueWatchdog                    = 4 * time.Second // a queued message never waits on a missed transition
	compactStale                     = 60 * time.Second
	busyStuck                        = 10 * time.Minute
	hooksRecheck                     = 30 * time.Second
	agentLaunchGrace                 = 5 * time.Second
	healTurnFresh                    = 30 * time.Minute // a transcript quiet longer proves no turn in progress
	processRetryCount                = 3
	launchPoll                       = 700 * time.Millisecond
	launchHookGrace                  = 1500 * time.Millisecond // SessionStart gets this long after the prompt box showed
	pushSettle                       = 1500 * time.Millisecond // Stop and Notification fire before the transcript has the text
	historySaveDelay                 = 500 * time.Millisecond
	turnEndSnapshotDelay             = 400 * time.Millisecond // the agent's last writes land a moment before its stop
)

var launchReadyPolls = []time.Duration{700 * time.Millisecond, 1500 * time.Millisecond, 3 * time.Second, 5 * time.Second, 8 * time.Second}

// Broadcaster is where status messages go: the status sockets.
type Broadcaster interface {
	Broadcast(v any, exclude *sockets.Client)
	Shutdown()
}

// Manager keeps the work state of every tab and tells the status sockets.
type Manager struct {
	d   Deps
	hub Broadcaster

	mu    sync.Mutex
	tabs  map[string]*Entry
	order []string // insertion order, as the old runtime's Map iterated

	scanned       bool
	earlyLaunches []string
	idHeal        []string
	rateLimits    any

	pollMu       sync.Mutex
	polling      bool
	pollAgain    bool
	pollInterval time.Duration
	pollReset    chan time.Duration

	compactTimers map[string]*time.Timer
	persist       chan func()
	watch         *jsonlWatch
	feed          *background.Feed
	ctx           context.Context
}

// New makes a keeper broadcasting on hub (a *sockets.Hub).
func New(d Deps, hub Broadcaster) *Manager {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = func(string, string, ...any) {}
	}
	m := &Manager{d: d, hub: hub, tabs: map[string]*Entry{}, compactTimers: map[string]*time.Timer{},
		persist: make(chan func(), 1024), pollReset: make(chan time.Duration, 1), ctx: context.Background()}
	m.watch = newJSONLWatch(m)
	m.feed = background.NewFeed(background.Deps{
		ReadTasks: func(t string) []background.Task {
			if d.BackgroundTasks == nil {
				return []background.Task{}
			}
			return d.BackgroundTasks(t)
		},
		ReadPane:  d.Tmux.Capture,
		Reconcile: background.ReconcileWithFooter,
		Publish: func(tabID string, tasks []background.Task) {
			m.broadcast(map[string]any{"type": "background:update", "tabId": tabID, "tasks": tasks}, nil)
		},
		Forget: func(t string) {
			if d.ForgetTranscript != nil {
				d.ForgetTranscript(t)
			}
		},
	}, d.BackgroundTimings, nil)
	return m
}

func (m *Manager) now() float64 { return float64(m.d.Now().UnixNano()) / 1e6 }

func (m *Manager) provider(e *Entry) Provider { return m.d.Providers[providerOfPanel(e.PanelType)] }

// Start scans every tab, then keeps them current until ctx ends.
func (m *Manager) Start(ctx context.Context) {
	m.ctx = ctx
	go m.persistLoop(ctx)
	m.scanAll()
	m.mu.Lock()
	m.scanned = true
	early := m.earlyLaunches
	m.earlyLaunches = nil
	heal := m.idHeal
	m.idHeal = nil
	m.mu.Unlock()
	for _, id := range early {
		m.MarkAgentLaunch(id, false)
	}
	go m.pollLoop(ctx)
	for _, id := range heal {
		go m.recoverEntrySession(id)
	}
	if m.d.Queue != nil {
		m.mu.Lock()
		ids := append([]string(nil), m.order...)
		m.mu.Unlock()
		for _, id := range ids {
			m.d.Queue.Schedule(id, queueSettle)
		}
		go m.queueWatchdog(ctx)
	}
	go func() {
		<-ctx.Done()
		m.shutdown()
	}()
}

func (m *Manager) queueWatchdog(ctx context.Context) {
	t := time.NewTicker(queueWatchdog)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, id := range m.d.Queue.Tabs() {
				if !m.d.Queue.Pending(id) {
					m.d.Queue.Schedule(id, 0)
				}
			}
		}
	}
}

func (m *Manager) shutdown() {
	m.feed.Stop()
	m.watch.closeAll()
	m.hub.Shutdown()
}

// persistLoop writes tab changes to the layout one at a time, in the order they were made.
func (m *Manager) persistLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case fn := <-m.persist:
			fn()
		}
	}
}

func (m *Manager) enqueuePersist(fn func()) {
	select {
	case m.persist <- fn:
	default:
		go func() { m.persist <- fn }()
	}
}

// persistToLayout saves the tab's state, dismissal and last event name. Called with the lock held.
func (m *Manager) persistToLayout(e *Entry) {
	session, state, dismissed, event := e.TmuxSession, e.CliState, e.DismissedAt, e.lastEventName()
	m.enqueuePersist(func() {
		if err := m.d.Layouts.UpdateTabCliStatus(session, state, dismissed, event); err != nil {
			m.d.Log("debug", "persist cli state failed", "err", err)
		}
	})
}

func (m *Manager) storeAgentState(session, provider string, sessionID, jsonlPath, summary, lastUser Opt[string]) {
	m.enqueuePersist(func() {
		if err := m.d.Layouts.UpdateTabAgentState(session, provider, sessionID, jsonlPath, summary, lastUser); err != nil {
			m.d.Log("debug", "store agent state failed", "err", err)
		}
	})
}

func (m *Manager) set(tabID string, e *Entry) {
	if _, has := m.tabs[tabID]; !has {
		m.order = append(m.order, tabID)
	}
	m.tabs[tabID] = e
}

func (m *Manager) del(tabID string) {
	if _, has := m.tabs[tabID]; !has {
		return
	}
	delete(m.tabs, tabID)
	for i, id := range m.order {
		if id == tabID {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
}

func (m *Manager) findBySession(session string) (string, *Entry) {
	for _, id := range m.order {
		if e := m.tabs[id]; e.TmuxSession == session {
			return id, e
		}
	}
	return "", nil
}

func (m *Manager) broadcast(v any, exclude *sockets.Client) { m.hub.Broadcast(v, exclude) }

// broadcastUpdate tells every socket a tab's whole entry. Called with the lock held.
func (m *Manager) broadcastUpdate(tabID string, e *Entry, exclude *sockets.Client) {
	m.broadcast(json.RawMessage(e.clientJSON([2]any{"type", "status:update"}, [2]any{"tabId", tabID})), exclude)
}

func (m *Manager) broadcastRemove(tabID string) {
	m.broadcast(map[string]any{"type": "status:update", "tabId": tabID, "cliState": nil, "workspaceId": "", "tabName": ""}, nil)
}

// Tabs is every tab as the page sees it (status:sync).
func (m *Manager) Tabs() any {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := orderedObject{}
	for _, id := range m.order {
		out = append(out, kv{id, json.RawMessage(m.tabs[id].clientJSON())})
	}
	return out
}

// Background is every tab with background work (background:sync).
func (m *Manager) Background() any { return m.feed.Snapshot() }

// RateLimits is the last limits content.
func (m *Manager) RateLimits() any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rateLimits
}

// RateLimitsChanged: the limits file got a new measurement.
func (m *Manager) RateLimitsChanged(data any) {
	m.mu.Lock()
	m.rateLimits = data
	m.mu.Unlock()
	m.broadcast(map[string]any{"type": "rate-limits:update", "data": data}, nil)
}

// Queues is the prompt queue of every tab.
func (m *Manager) Queues() any {
	if m.d.Queue == nil {
		return map[string]any{}
	}
	return m.d.Queue.Snapshot()
}

// History is the session history, clipped for the wire.
func (m *Manager) History() any {
	if m.d.History == nil {
		return []any{}
	}
	entries := m.d.History.All()
	out := make([]history.Entry, len(entries))
	for i, e := range entries {
		out[i] = e.Clip()
	}
	return out
}

// Broadcast sends any message to every status socket (queue and pane-intent updates).
func (m *Manager) Broadcast(v any) { m.broadcast(v, nil) }

type kv struct {
	k string
	v any
}

// orderedObject writes its keys in order.
type orderedObject []kv

func (o orderedObject) MarshalJSON() ([]byte, error) {
	b := []byte{'{'}
	for i, p := range o {
		if i > 0 {
			b = append(b, ',')
		}
		k, _ := json.Marshal(p.k)
		v, err := json.Marshal(p.v)
		if err != nil {
			return nil, err
		}
		b = append(append(append(b, k...), ':'), v...)
	}
	return append(b, '}'), nil
}
