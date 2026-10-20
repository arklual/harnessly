package timeline

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/sessions"
)

func projectDir(projects, cwd string) string {
	return filepath.Join(projects, sessions.ProjectName(cwd))
}

func readFile(p string) ([]byte, error) { return os.ReadFile(p) }

func readAt(p string, from, n int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, n)
	k, err := f.ReadAt(buf, from)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:k], nil
}

// dirWatch calls fn with the name of every entry of dir that changes, until stopped; nil when dir cannot be watched.
func dirWatch(dir string, fn func(name string)) func() {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil
	}
	if err := w.Add(dir); err != nil {
		_ = w.Close()
		return nil
	}
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				fn(filepath.Base(ev.Name))
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	closed := false
	return func() {
		if !closed {
			closed = true
			close(stop)
			go w.Close()
		}
	}
}

// pendingWatch waits for the transcript of a session that started but has not written one yet.
type pendingWatch struct {
	stopped bool
	dir     func()
	parent  func()
}

func (p *pendingWatch) stop() {
	if p.stopped {
		return
	}
	p.stopped = true
	if p.dir != nil {
		p.dir()
		p.dir = nil
	}
	if p.parent != nil {
		p.parent()
		p.parent = nil
	}
}

func (s *Server) cancelPending(session string) {
	if w := s.pending[session]; w != nil {
		w.stop()
		delete(s.pending, session)
	}
}

// watchForJSONL watches the project folder (or the projects folder, while the project's does not exist) for the
// session's transcript, and moves the tab's sockets onto it once it appears.
func (s *Server) watchForJSONL(session, sessionID, cwd string, p *Provider) {
	s.cancelPending(session)
	dir := projectDir(s.d.ClaudeProjects, cwd)
	name := sessionID + ".jsonl"
	expected := filepath.Join(dir, name)
	w := &pendingWatch{}

	found := func() {
		w.stop()
		if s.pending[session] == w {
			delete(s.pending, session)
		}
		s.setAgentSessionID(session, p, sessionID)
		for _, cn := range s.sessionConns(session) {
			if expected == cn.jsonlPath {
				continue
			}
			if cn.jsonlPath != "" {
				s.unsubscribe(cn.c, cn.jsonlPath)
			}
			cn.jsonlPath = expected
			s.subscribeAndUpdateSummary(cn.c, expected, sessionID, session, p)
		}
	}
	watchDir := func() {
		if w.stopped {
			return
		}
		w.dir = dirWatch(dir, func(changed string) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if !w.stopped && changed == name && exists(expected) {
				found()
			}
		})
	}
	if exists(expected) {
		found()
		return
	}
	if exists(dir) {
		watchDir()
		// The file may have appeared between the check and the watch.
		if !w.stopped && exists(expected) {
			found()
		}
	} else {
		projects := filepath.Dir(dir)
		base := filepath.Base(dir)
		if os.MkdirAll(projects, 0o755) == nil {
			w.parent = dirWatch(projects, func(changed string) {
				s.mu.Lock()
				defer s.mu.Unlock()
				if w.stopped || changed != base || !exists(dir) {
					return
				}
				if w.parent != nil {
					w.parent()
					w.parent = nil
				}
				if exists(expected) {
					found()
					return
				}
				watchDir()
				if !w.stopped && exists(expected) {
					found()
				}
			})
		}
	}
	// Like the old runtime, a watch that found its file at once is still registered, stopped.
	s.pending[session] = w
}

// sessionWatcher follows which session a tab's agent runs, shared by the tab's sockets.
type sessionWatcher struct {
	stop func()
	// codexInfo takes a session a Codex hook named (Codex only).
	codexInfo func(agents.SessionInfo)
}

func (s *Server) detect(p *Provider, pid int) agents.SessionInfo {
	info, err := p.Detect(s.d.Procs.ChildrenOf(pid))
	if err != nil {
		return agents.Info(agents.NotRunning)
	}
	return info
}

// watchSessions starts the provider's watch of the pane's agent; changes reach onSessionChange under the lock.
func (s *Server) watchSessions(session string, pid int, p *Provider) *sessionWatcher {
	if p.ID == agents.Codex {
		return s.watchCodex(session, pid, p)
	}
	return s.watchClaude(session, pid, p)
}

// watchClaude: Claude writes a pid file per process into its sessions folder; a change there is looked at 200 ms
// after the last of a burst, and the process of the last session found is checked every 10 s.
func (s *Server) watchClaude(session string, pid int, p *Provider) *sessionWatcher {
	stopped := false
	currentPID := 0
	var debounce *time.Timer
	var watchStop func()
	var install *time.Ticker
	installDone := make(chan struct{})
	quit := make(chan struct{})

	onDirChange := func(string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if stopped {
			return
		}
		if debounce != nil {
			debounce.Stop()
		}
		debounce = time.AfterFunc(s.d.Timing.SessionDebounce, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if stopped {
				return
			}
			info := s.detect(p, pid)
			if info.PID != 0 {
				currentPID = info.PID
			}
			s.onSessionChange(session, p, info)
		})
	}
	var tryWatch func()
	tryWatch = func() {
		if stopped {
			return
		}
		if stop := dirWatch(p.SessionsDir, onDirChange); stop != nil {
			watchStop = stop
			if install != nil {
				install.Stop()
				close(installDone)
				install = nil
			}
			return
		}
		if install == nil {
			install = time.NewTicker(s.d.Timing.InstallCheck)
			tick := install.C
			go func() {
				for {
					select {
					case <-quit:
						return
					case <-installDone:
						return
					case <-tick:
					}
					s.mu.Lock()
					if !stopped && exists(p.SessionsDir) {
						tryWatch()
						info := s.detect(p, pid)
						if info.PID != 0 {
							currentPID = info.PID
						}
						s.onSessionChange(session, p, info)
					}
					s.mu.Unlock()
				}
			}()
		}
	}
	tryWatch()
	poll := time.NewTicker(s.d.Timing.PIDPoll)
	go func() {
		defer poll.Stop()
		for {
			select {
			case <-quit:
				return
			case <-poll.C:
			}
			s.mu.Lock()
			if !stopped && currentPID != 0 && !s.d.Alive(currentPID) {
				currentPID = 0
				s.onSessionChange(session, p, s.detect(p, pid))
			}
			s.mu.Unlock()
		}
	}()
	return &sessionWatcher{stop: func() {
		if stopped {
			return
		}
		stopped = true
		close(quit)
		if watchStop != nil {
			watchStop()
		}
		if debounce != nil {
			debounce.Stop()
		}
		if install != nil {
			install.Stop()
		}
	}}
}

// watchCodex: a new Codex session is named by its hooks; the process is checked every 10 s, and looked for while none
// is known.
func (s *Server) watchCodex(session string, pid int, p *Provider) *sessionWatcher {
	stopped := false
	currentPID := 0
	var currentID, currentPath string
	quit := make(chan struct{})
	remember := func(info agents.SessionInfo) {
		if info.PID != 0 {
			currentPID = info.PID
		}
		currentID, currentPath = info.SessionID, info.JSONLPath
	}
	poll := time.NewTicker(s.d.Timing.PIDPoll)
	go func() {
		defer poll.Stop()
		for {
			select {
			case <-quit:
				return
			case <-poll.C:
			}
			s.mu.Lock()
			if !stopped {
				switch {
				case currentPID == 0:
					info := s.detect(p, pid)
					remember(info)
					if info.SessionID != "" || info.JSONLPath != "" {
						s.onSessionChange(session, p, info)
					}
				case !s.d.Alive(currentPID):
					currentPID = 0
					info := s.detect(p, pid)
					remember(info)
					s.onSessionChange(session, p, info)
				case currentID == "" && currentPath == "":
					info := s.detect(p, pid)
					if info.SessionID != currentID || info.JSONLPath != currentPath {
						remember(info)
						s.onSessionChange(session, p, info)
					}
				}
			}
			s.mu.Unlock()
		}
	}()
	return &sessionWatcher{
		stop: func() {
			if !stopped {
				stopped = true
				close(quit)
			}
		},
		codexInfo: func(info agents.SessionInfo) {
			if stopped {
				return
			}
			remember(info)
			s.onSessionChange(session, p, info)
		},
	}
}

// onSessionChange moves the tab's sockets with the agent: a new transcript, the agent gone, a new session not yet on
// disk.
func (s *Server) onSessionChange(session string, p *Provider, info agents.SessionInfo) {
	conns := s.sessionConns(session)
	for _, cn := range conns {
		if info.JSONLPath != "" && info.JSONLPath != cn.jsonlPath {
			s.cancelPending(session)
			if cn.jsonlPath != "" {
				s.unsubscribe(cn.c, cn.jsonlPath)
			}
			cn.jsonlPath = info.JSONLPath
			if info.SessionID != "" {
				s.setAgentSessionID(session, p, info.SessionID)
			}
			sendJSON(cn.c, sessionChanged(info.SessionID, "new-session-started"))
			s.subscribeAndUpdateSummary(cn.c, info.JSONLPath, info.SessionID, session, p)
		} else if info.JSONLPath == "" && info.Status == agents.NotRunning {
			s.cancelPending(session)
			// A Claude conversation outlives its process (exit, a kill): the next prompt resumes the same session, so
			// the transcript stays subscribed and the tab keeps its session id.
			if p.ID == agents.Claude && cn.jsonlPath != "" {
				if _, _, tabID, ok := s.d.Tmux.ParseSessionName(session); ok {
					s.effect(func() {
						if k := s.keeper(); k != nil {
							k.MarkAgentGone(tabID, false)
						}
					})
				}
				sendJSON(cn.c, sessionChanged(basenameJSONL(cn.jsonlPath), "agent-exited"))
				continue
			}
			if cn.jsonlPath != "" {
				s.unsubscribe(cn.c, cn.jsonlPath)
				cn.jsonlPath = ""
			}
			s.setAgentSessionID(session, p, nil)
			s.effect(func() { s.d.Layout.AgentSummary(session, p.ID, nil) })
			sendJSON(cn.c, sessionChanged("", "session-ended"))
		}
	}
	if info.Status == agents.Running && info.JSONLPath == "" {
		if info.SessionID != "" {
			s.setAgentSessionID(session, p, info.SessionID)
		}
		for _, cn := range conns {
			if cn.jsonlPath != "" {
				if info.SessionID != "" && basenameJSONL(cn.jsonlPath) != info.SessionID {
					s.unsubscribe(cn.c, cn.jsonlPath)
					cn.jsonlPath = ""
				} else {
					continue
				}
			}
			if s.pending[session] != nil {
				continue
			}
			sendJSON(cn.c, sessionChanged(info.SessionID, "session-waiting"))
		}
		if info.SessionID != "" && info.Cwd != "" {
			active := false
			for _, cn := range conns {
				if cn.jsonlPath != "" {
					active = true
				}
			}
			if !active && len(conns) > 0 {
				s.cancelPending(session)
				s.watchForJSONL(session, info.SessionID, info.Cwd, p)
			}
		}
	}
}

func basenameJSONL(p string) string {
	base := filepath.Base(p)
	if len(base) > len(".jsonl") && base[len(base)-len(".jsonl"):] == ".jsonl" {
		return base[:len(base)-len(".jsonl")]
	}
	return base
}
