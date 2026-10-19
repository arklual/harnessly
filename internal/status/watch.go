package status

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"harnessly/internal/agents"
)

const jsonlDebounce = 100 * time.Millisecond

// jsonlWatch follows the transcripts of the tabs. One inotify watcher serves them all; each tab's change is debounced.
type jsonlWatch struct {
	m       *Manager
	mu      sync.Mutex
	w       *fsnotify.Watcher
	byTab   map[string]string          // tab → transcript
	byPath  map[string]map[string]bool // transcript → tabs
	debounc map[string]*time.Timer
}

func newJSONLWatch(m *Manager) *jsonlWatch {
	return &jsonlWatch{m: m, byTab: map[string]string{}, byPath: map[string]map[string]bool{}, debounc: map[string]*time.Timer{}}
}

func (j *jsonlWatch) ensure() bool {
	if j.w != nil {
		return true
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return false
	}
	j.w = w
	go j.loop(w)
	return true
}

func (j *jsonlWatch) loop(w *fsnotify.Watcher) {
	for {
		select {
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Chmod) == 0 {
				if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
					j.dropPath(filepath.Clean(ev.Name))
				}
				continue
			}
			j.changed(filepath.Clean(ev.Name))
		case _, ok := <-w.Errors:
			if !ok {
				return
			}
		}
	}
}

// start follows a tab's transcript; a tab already following it is left alone.
func (j *jsonlWatch) start(tabID, path string) {
	path = filepath.Clean(path)
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.byTab[tabID] == path {
		return
	}
	j.stopLocked(tabID)
	if !j.ensure() {
		return
	}
	if len(j.byPath[path]) == 0 {
		if j.w.Add(path) != nil {
			return // the file may not exist yet
		}
		j.byPath[path] = map[string]bool{}
	}
	j.byPath[path][tabID] = true
	j.byTab[tabID] = path
}

func (j *jsonlWatch) stop(tabID string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.stopLocked(tabID)
}

func (j *jsonlWatch) stopLocked(tabID string) {
	path, ok := j.byTab[tabID]
	if !ok {
		return
	}
	delete(j.byTab, tabID)
	if t := j.debounc[tabID]; t != nil {
		t.Stop()
		delete(j.debounc, tabID)
	}
	delete(j.byPath[path], tabID)
	if len(j.byPath[path]) == 0 {
		delete(j.byPath, path)
		if j.w != nil {
			_ = j.w.Remove(path)
		}
	}
}

// dropPath: a transcript replaced or removed stops being followed, like a failed watch before.
func (j *jsonlWatch) dropPath(path string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for tabID := range j.byPath[path] {
		j.stopLocked(tabID)
	}
}

func (j *jsonlWatch) closeAll() {
	j.mu.Lock()
	defer j.mu.Unlock()
	for tabID := range j.byTab {
		j.stopLocked(tabID)
	}
}

func (j *jsonlWatch) changed(path string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for tabID := range j.byPath[path] {
		if t := j.debounc[tabID]; t != nil {
			t.Stop()
		}
		j.debounc[tabID] = time.AfterFunc(jsonlDebounce, func() {
			j.mu.Lock()
			delete(j.debounc, tabID)
			j.mu.Unlock()
			j.m.transcriptChanged(tabID, path)
		})
	}
}

// transcriptChanged reads what the transcript now says. It is the only witness of a turn started without a prompt
// hook (a finished background task, a monitor event, a message from another session), of a menu answered, and of an
// interrupt.
func (m *Manager) transcriptChanged(tabID, path string) {
	m.mu.Lock()
	e := m.tabs[tabID]
	if e == nil || e.CliState == Inactive || e.CliState == Cancelled {
		m.watch.stop(tabID)
		m.mu.Unlock()
		return
	}
	p := m.provider(e)
	if p == nil {
		m.mu.Unlock()
		return
	}
	m.touchBackground(tabID, e, false)
	m.mu.Unlock()
	s, err := p.Snapshot(path, false)
	if err != nil {
		return
	}
	m.mu.Lock()
	session := e.TmuxSession
	state, lastAt := e.CliState, e.lastEventAt()
	codex := e.AgentProviderID.Value() == agents.Codex || e.PanelType.Value() == agents.CodexPanel
	m.mu.Unlock()
	if !s.Idle && (state == Idle || state == ReadyForReview) && s.LastEntryTs != nil && *s.LastEntryTs > lastAt+1000 {
		m.updateFromHook(session, EventPromptSubmit, "")
	}
	m.mu.Lock()
	if inputAnsweredByTranscript(e.CliState, s.LastEntryTs, e.lastEventAt()) && m.tabs[tabID] == e {
		m.applyCliState(tabID, e, Busy, applyOpts{})
		m.persistToLayout(e)
		m.broadcastUpdate(tabID, e, nil)
	}
	m.mu.Unlock()
	if codex && m.d.CodexRateLimits != nil {
		go m.d.CodexRateLimits(path)
	}
	m.mu.Lock()
	interrupt := s.Interrupted && e.CliState == Busy && s.LastEntryTs != nil && *s.LastEntryTs > e.LastInterruptTs && *s.LastEntryTs > e.lastEventAt()
	if interrupt {
		e.LastInterruptTs = *s.LastEntryTs
	}
	m.mu.Unlock()
	if interrupt {
		m.updateFromHook(session, EventInterrupt, "")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.applySnapshot(e, s) && m.tabs[tabID] == e {
		m.broadcastUpdate(tabID, e, nil)
	}
}
