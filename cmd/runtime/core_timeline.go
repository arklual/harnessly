package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/claude"
	"github.com/ly-solutions/harnessly/internal/agents/codex"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/machine"
	"github.com/ly-solutions/harnessly/internal/preflight"
	"github.com/ly-solutions/harnessly/internal/proc"
	"github.com/ly-solutions/harnessly/internal/state"
	"github.com/ly-solutions/harnessly/internal/timeline"
	"github.com/ly-solutions/harnessly/internal/tmux"
	"github.com/ly-solutions/harnessly/internal/transcript"
)

// The timeline slice ("timeline"): the conversation socket /api/timeline, the statusline sink and the routes that read
// transcripts. It needs the core: the keeper it tells about sessions and the layout store it binds tabs in.

// timelineOn is whether the timeline slice runs here now: switched on, and the core it needs running.
func (c *core) timelineOn() bool {
	return c.sliceOn != nil && c.sliceOn("timeline") && c.manager() != nil
}

// broadcastSessionStats sends a session's statusline figures to the timeline sockets that show its transcript.
func (c *core) broadcastSessionStats(stats *jsvalue.Object) {
	if c.timeline != nil && stats != nil {
		c.timeline.BroadcastSessionStats(stats)
	}
}

// timelineConfig is what the deployment names for the timeline.
type timelineConfig struct {
	// AppBase is the prefix the pages reach the runtime's routes under (an attached image's address).
	AppBase    string
	UploadsDir string
}

func timelineConfigFromEnv(dataDir string) timelineConfig {
	return timelineConfig{AppBase: env("RUNTIME_APP_BASE_PATH", "/agents"), UploadsDir: env("RUNTIME_UPLOADS_DIR", filepath.Join(dataDir, "uploads"))}
}

func (c *core) newTimeline(cfg timelineConfig) *timeline.Server {
	home := c.cfg.Home
	checks := preflight.New(home)
	claudeDet := claude.NewDetector(home)
	claudeDet.Installed = func() bool { return checks.Runtime().Claude.Installed }
	codexDet := codex.NewDetector(home)
	codexDet.Installed = func() bool { return checks.Runtime().Codex.Installed }
	projects := claudeDet.ProjectsDir()
	providers := map[string]*timeline.Provider{
		agents.ClaudePanel: {
			ID: agents.Claude, DisplayName: "Claude Code",
			Detect:         claudeDet.Detect,
			Running:        claudeDet.IsRunning,
			ParsePaneTitle: claude.ParsePaneTitle,
			SessionIDFromPath: func(p string) string {
				if p == "" {
					return ""
				}
				return jscompat.Basename(p, ".jsonl")
			},
			ValidSessionID: agents.IsUUID,
			FindSession:    func(id string) string { return claude.FindTranscript(projects, id) },
			SessionsDir:    filepath.Join(claudeDet.Dir, "sessions"),
		},
		agents.CodexPanel: {
			ID: agents.Codex, DisplayName: "Codex",
			Detect:            func(children []int) (agents.SessionInfo, error) { return codexDet.Detect(children, false), nil },
			Running:           codexDet.IsRunning,
			ParsePaneTitle:    codex.ParsePaneTitle,
			SessionIDFromPath: codex.SessionIDFromPath,
			ValidSessionID:    agents.IsUUID,
			FindSession: func(id string) string {
				if m, ok := codex.FindSessionByID(codexDet.SessionsRoot(), id, time.Now(), codex.ScanDays); ok {
					return m.JSONLPath
				}
				return ""
			},
		},
	}
	return timeline.New(timeline.Deps{
		Tmux:           timelineTmux{c.cfg.Tmux},
		Procs:          proc.System(),
		Providers:      providers,
		Keeper:         c.timelineKeeper,
		Layout:         timelineLayout{c},
		ResumeLine:     c.timelineResumeLine,
		ClaudeProjects: projects,
		CodexSessions:  codexDet.SessionsRoot(),
		StatsDir:       filepath.Join(c.cfg.DataDir, "session-stats"),
		Codex:          transcript.CodexOptions{ImageURL: transcript.NewCodexImageURL(cfg.UploadsDir, cfg.AppBase)},
		Log: func(level, msg string, kv ...any) {
			fields := make([]agent.F, 0, len(kv)/2)
			for i := 0; i+1 < len(kv); i += 2 {
				k, _ := kv[i].(string)
				fields = append(fields, agent.F{K: k, V: kv[i+1]})
			}
			agent.Log(level, msg, fields...)
		},
	})
}

func (c *core) timelineKeeper() timeline.Keeper {
	if m := c.manager(); m != nil {
		return m
	}
	return nil
}

// timelineResumeLine is the line that resumes a session in a tab, with the launch choice the tab remembers.
func (c *core) timelineResumeLine(provider, sessionID, session string) (string, error) {
	if c.agents == nil {
		return "", errors.New("Error during resume")
	}
	wsID, _, _, _ := c.cfg.Tmux.ParseSessionName(session)
	var launch *agents.Launch
	for _, t := range c.store.WorkspaceTabs() {
		if t.Tab.String("sessionName") == session {
			launch = launchOfTab(c.store.TabLaunch(t.Tab))
			break
		}
	}
	return c.agents.ResumeLine(provider, sessionID, wsID, launch)
}

func launchOfTab(o *jsvalue.Object) *agents.Launch {
	if o == nil {
		return nil
	}
	l := &agents.Launch{}
	if v, _ := o.Get("model"); v != nil {
		if s, ok := v.(string); ok {
			l.Model = &s
		}
	}
	if v, _ := o.Get("effort"); v != nil {
		if s, ok := v.(string); ok {
			l.Effort = &s
		}
	}
	return l
}

// timelineTmux is tmux as the timeline asks it.
type timelineTmux struct{ *tmux.Tmux }

func (t timelineTmux) SessionCwd(s string) string { return machine.SessionDir(t.Tmux, s) }

// timelineLayout writes what the timeline learns about a tab's agent into the layout store.
type timelineLayout struct{ c *core }

func (l timelineLayout) AgentState(session, provider string, sessionID, jsonlPath, summary any) {
	_ = l.c.store.UpdateTabAgentState(session, state.Provider(provider), state.AgentState{
		SessionID: state.Value(sessionID), JsonlPath: state.Value(jsonlPath), Summary: state.Value(summary)})
}

func (l timelineLayout) AgentSessionID(session, provider string, sessionID any) {
	_ = l.c.store.UpdateTabAgentSessionID(session, state.Provider(provider), sessionID)
}

func (l timelineLayout) AgentSummary(session, provider string, summary any) {
	_ = l.c.store.UpdateTabAgentSummary(session, state.Provider(provider), summary)
}

func (l timelineLayout) LastUserMessage(session, message string) {
	_ = l.c.store.UpdateTabLastUserMessage(session, message)
}

// Recover is the session a stored Claude tab stands for, written back to it; the session is shown even when the
// write fails.
func (l timelineLayout) Recover(session string) (string, string, bool) {
	wsID, _, _, ok := l.c.cfg.Tmux.ParseSessionName(session)
	if !ok {
		return "", "", false
	}
	layouts := coreLayouts{store: l.c.store, claude: l.c.claude, history: l.c.history}
	tabs, ok := layouts.Tabs(wsID)
	if !ok {
		return "", "", false
	}
	var tab *agents.TabAgent
	for i := range tabs {
		if tabs[i].SessionName == session {
			tab = &tabs[i].Agent
			break
		}
	}
	if tab == nil {
		return "", "", false
	}
	found := agents.RecoverTabSession(*tab, claude.Tabs{}, agents.RecoveryDeps{
		Exists:         func(p string) bool { _, err := os.Stat(p); return err == nil },
		FindTranscript: func(id string) string { return claude.FindTranscript(l.c.claude.ProjectsDir(), id) },
		History: func() ([]agents.HistoryEntry, error) {
			var out []agents.HistoryEntry
			for _, e := range l.c.history.All() {
				out = append(out, agents.HistoryEntry{TabID: e.TabID, ProviderID: e.ProviderID, AgentSessionID: e.AgentSessionID, CompletedAt: float64(e.CompletedAt)})
			}
			return out, nil
		},
	})
	if found == nil {
		return "", "", false
	}
	_ = l.c.store.StoreRecoveredAgentSession(session, state.Claude, found.SessionID, found.JSONLPath)
	return found.SessionID, found.JSONLPath, true
}

// timelineRoutes: the socket, for a page session, while the slice and the core run.
func (c *core) timelineSocketRoutes(sessionOK func(*http.Request) bool) []gate.Route {
	return []gate.Route{{
		Slice: "timeline",
		Match: func(r *http.Request) bool {
			return isUpgrade(r) && pathIs(r, "/api/timeline") && c.manager() != nil
		},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !sessionOK(r) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			c.timeline.ServeHTTP(w, r)
		}),
	}}
}
