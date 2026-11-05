package status

import (
	"encoding/json"
	"os"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/screen"
)

// MarkAgentLaunch: a launch or resume line was typed into the tab. reset clears the conversation the tab showed.
func (m *Manager) MarkAgentLaunch(tabID string, reset bool) {
	if m.d.AgentLaunched != nil {
		m.d.AgentLaunched(tabID)
	}
	m.mu.Lock()
	e := m.tabs[tabID]
	if e == nil {
		// Before the first scan the launch waits for it; after, a tab not known yet is looked up by a poll now and
		// marked then (the old runtime dropped it).
		if !m.scanned {
			m.earlyLaunches = append(m.earlyLaunches, tabID)
			m.mu.Unlock()
			return
		}
		m.mu.Unlock()
		m.Poll()
		m.mu.Lock()
		if e = m.tabs[tabID]; e == nil {
			m.mu.Unlock()
			return
		}
	}
	now := m.now()
	e.LastResumeOrStartedAt = &now
	if reset {
		e.AgentSessionID, e.AgentSummary, e.LastUserMessage, e.LastAssistantMessage = Null[string](), Null[string](), Null[string](), Null[string]()
		e.JSONLPath = ""
		e.CurrentAction = Null[agents.CurrentAction]()
		e.PermissionRequest = Null[json.RawMessage]()
		m.watch.stop(tabID)
		m.persistToLayout(e)
		if p := m.provider(e); p != nil {
			m.storeAgentState(e.TmuxSession, p.ID(), Null[string](), Null[string](), Null[string](), Null[string]())
		}
		m.broadcastUpdate(tabID, e, nil)
	}
	for _, d := range launchReadyPolls {
		time.AfterFunc(d, m.Poll)
	}
	if e.AgentProviderID.Value() != agents.Codex && e.PanelType.Value() != agents.CodexPanel {
		m.beginLaunch(tabID, e)
	}
	m.mu.Unlock()
}

// beginLaunch: from the launch line until the prompt box takes input, what is sent waits in the queue. Called with the
// lock held.
func (m *Manager) beginLaunch(tabID string, e *Entry) {
	started := m.now()
	e.LaunchingSince = &started
	e.LaunchFailed = false
	m.broadcastUpdate(tabID, e, nil)
	go m.watchLaunch(tabID, started)
}

// launchEnded is called with the lock held.
func (m *Manager) launchEnded(tabID string, e *Entry) {
	e.LaunchingSince, e.LaunchFailed = nil, false
	m.broadcastUpdate(tabID, e, nil)
	if m.d.Queue != nil {
		m.d.Queue.Schedule(tabID, queueSettle)
	}
}

// LaunchFailed: a launch the queue asked for cannot come up (nothing to resume); the composer shows it.
func (m *Manager) LaunchFailed(tabID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.tabs[tabID]; e != nil {
		now := m.now()
		e.LaunchingSince, e.LaunchFailed = &now, true
		m.broadcastUpdate(tabID, e, nil)
	}
}

// watchLaunch waits for the agent's prompt box. SessionStart ends a launch by itself; this stands in when that hook is
// late or lost (a loaded machine misses its 3 s limit), and calls a launch failed once its process is gone, never came,
// or never drew its prompt box.
func (m *Manager) watchLaunch(tabID string, started float64) {
	seenRunning := false
	var readySince *float64
	for {
		time.Sleep(launchPoll)
		m.mu.Lock()
		e := m.tabs[tabID]
		if e == nil || e.LaunchingSince == nil || *e.LaunchingSince != started {
			m.mu.Unlock()
			return
		}
		p, session := m.provider(e), e.TmuxSession
		m.mu.Unlock()
		running := false
		if pid := m.d.Tmux.PanePID(session); p != nil && pid > 0 {
			running = p.Running(pid, m.d.Procs.Children(pid))
		}
		seenRunning = seenRunning || running
		ready := false
		if running {
			if pane, ok := m.d.Tmux.Capture(session); ok && pane != "" {
				ready = screen.IsLaunchedPromptBoxReady(pane)
				if !ready {
					m.acceptTrust(tabID, session, pane)
				}
			}
		}
		now := m.now()
		phase := launchPhase(launchProbe{elapsed: time.Duration((now - started) * float64(time.Millisecond)), running: running, seenRunning: seenRunning, promptOK: ready})
		m.mu.Lock()
		if e.LaunchingSince == nil || *e.LaunchingSince != started || m.tabs[tabID] != e {
			m.mu.Unlock()
			return
		}
		switch phase {
		case "failed":
			e.LaunchFailed = true
			m.broadcastUpdate(tabID, e, nil)
			m.mu.Unlock()
			m.d.Log("warn", "agent launch did not come up", "tabId", tabID, "running", running, "seenRunning", seenRunning)
			return
		case "ready":
			if readySince == nil {
				readySince = &now
			}
			if now-*readySince >= ms(launchHookGrace) {
				m.mu.Unlock()
				m.updateFromHook(session, EventSessionStart, "")
				return
			}
		default:
			readySince = nil
		}
		m.mu.Unlock()
	}
}

// resolveRetries are the pauses between looks for the transcript of a tab that started working without one. A new
// Claude creates its transcript only after the hook of the first prompt has fired: one look at that moment finds
// nothing, and without more the chat waited for the next poll of the tabs (30-60 s) to show an answer that was there.
// Past the last one the poll takes over.
var resolveRetries = []time.Duration{
	200 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 3 * time.Second, 5 * time.Second,
	8 * time.Second, 10 * time.Second,
}

// resolveTranscript finds the transcript of a tab that started working without one: from the session id its layout
// keeps, else from the agent running in the pane; looked for again while it is not on disk yet.
func (m *Manager) resolveTranscript(tabID, session string) {
	for i := 0; ; i++ {
		e, transcript, done := m.lookForTranscript(tabID, session)
		if done {
			return
		}
		if transcript != "" {
			m.setResolvedTranscript(tabID, e, transcript)
			return
		}
		if i >= len(resolveRetries) {
			return
		}
		time.Sleep(resolveRetries[i])
	}
}

// lookForTranscript is one look; done when the tab is gone or has its transcript already.
func (m *Manager) lookForTranscript(tabID, session string) (e *Entry, transcript string, done bool) {
	m.mu.Lock()
	e = m.tabs[tabID]
	if e == nil || e.JSONLPath != "" {
		m.mu.Unlock()
		return e, "", true
	}
	m.mu.Unlock()
	if wsID, _, _, ok := m.d.Tmux.ParseSessionName(session); ok {
		if tabs, ok := m.d.Layouts.Tabs(wsID); ok {
			for _, t := range tabs {
				if t.SessionName != session {
					continue
				}
				if p := m.d.Providers[providerOfPanel(t.PanelType)]; p != nil {
					if id := p.ReadSessionID(t.Agent); id != "" {
						if p.ID() == agents.Codex {
							transcript = p.FindSession(id)
						} else if cwd := m.d.Tmux.SessionCwd(session); cwd != "" {
							if c := p.TranscriptIn(cwd, id); c != "" {
								if _, err := os.Stat(c); err == nil {
									transcript = c
								}
							}
						}
					}
				}
				if t.LastUserMessage.Has() {
					m.mu.Lock()
					if !optEq(e.LastUserMessage, t.LastUserMessage) {
						e.LastUserMessage = t.LastUserMessage
						m.broadcastUpdate(tabID, e, nil)
					}
					m.mu.Unlock()
				}
				break
			}
		}
	}
	if transcript == "" {
		if pid := m.d.Tmux.PanePID(session); pid > 0 {
			children := m.d.Procs.Children(pid)
			for _, id := range []string{agents.Claude, agents.Codex} {
				if p := m.d.Providers[id]; p != nil {
					if info := p.Detect(pid, children); info.Status == agents.Running && info.JSONLPath != "" {
						transcript = info.JSONLPath
						break
					}
				}
			}
		}
	}
	return e, transcript, false
}

func (m *Manager) setResolvedTranscript(tabID string, e *Entry, transcript string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tabs[tabID] != e {
		return
	}
	e.JSONLPath = transcript
	if p := m.provider(e); p != nil {
		e.AgentProviderID = Some(p.ID())
		if id := p.SessionIDFromPath(transcript); id != "" {
			e.AgentSessionID = Some(id)
		}
	}
	if e.CliState == Busy || e.CliState == NeedsInput {
		m.watch.start(tabID, transcript)
	}
}
