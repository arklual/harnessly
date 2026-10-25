package main

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/claude"
	"github.com/ly-solutions/harnessly/internal/agents/codex"
	"github.com/ly-solutions/harnessly/internal/background"
	"github.com/ly-solutions/harnessly/internal/coreapi"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/history"
	"github.com/ly-solutions/harnessly/internal/hooksetup"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/launch"
	"github.com/ly-solutions/harnessly/internal/limits"
	"github.com/ly-solutions/harnessly/internal/nodejson"
	"github.com/ly-solutions/harnessly/internal/paneintents"
	"github.com/ly-solutions/harnessly/internal/panel"
	"github.com/ly-solutions/harnessly/internal/prompts"
	"github.com/ly-solutions/harnessly/internal/push"
	"github.com/ly-solutions/harnessly/internal/sleep"
	"github.com/ly-solutions/harnessly/internal/sockets"
	"github.com/ly-solutions/harnessly/internal/state"
	"github.com/ly-solutions/harnessly/internal/status"
	"github.com/ly-solutions/harnessly/internal/telemetry"
	"github.com/ly-solutions/harnessly/internal/terminal"
	"github.com/ly-solutions/harnessly/internal/timeline"
	"github.com/ly-solutions/harnessly/internal/tmux"
	"github.com/ly-solutions/harnessly/internal/vault"
)

// The runtime core (slice "core"): the tab keeper, the layout and workspace stores, the status and sync sockets and
// what feeds them. It runs only while the slice is on: two keepers of one machine's tabs would fight over its files.

type coreConfig struct {
	DataDir, Home string
	Tmux          *tmux.Tmux
	Term          *terminal.Server
	MetaKey       string
	AgentHeader   string
	CodexbarBin   string
	CodexHome     string
	Prompts       promptConfig
	Hooks         hookConfig
	// Agents answers availability and launch lines and starts conversations; nil for none.
	Agents *coreAgents
	Launch launchConfig
	// Push is the process's web push sender and subscription store, shared with the machines slice's push routes.
	Push     *push.Sender
	Timeline timelineConfig
	// Standalone: no old runtime runs beside the core. What it was told (the workspace prompts, the corrected states)
	// is the core's own then: WorkspacePrompts writes the prompt files, Telemetry reports the states.
	Standalone       bool
	WorkspacePrompts hooksetup.Prompts
	Telemetry        *telemetry.Queue
}

type syncBroadcaster struct{ s *sockets.SyncServer }

func (b syncBroadcaster) Layout(wsID string) {
	b.s.Broadcast(map[string]any{"type": "layout", "workspaceId": wsID})
}
func (b syncBroadcaster) Workspaces() { b.s.Broadcast(map[string]any{"type": "workspace"}) }

// reconciler hands layout changes to whichever keeper runs.
type reconciler struct{ c *core }

func (r reconciler) ReconcileWorkspaceTabs(wsID string, tabIDs []string) {
	if m := r.c.manager(); m != nil {
		m.ReconcileWorkspaceTabs(wsID, tabIDs)
	}
}
func (r reconciler) RemoveWorkspaceTabs(wsID string) {
	if m := r.c.manager(); m != nil {
		m.RemoveWorkspaceTabs(wsID)
	}
}

type core struct {
	cfg       coreConfig
	store     *state.Store
	statusHub *sockets.Hub
	sync      *sockets.SyncServer
	history   *history.Store
	limits    *limits.Cache
	bg        *background.Reader
	claude    *claude.Detector
	// intents are what agents asked their right panes; panel and vault carry commands to the person's pages.
	intents *paneintents.Store
	panel   *panel.Bridge
	vault   *vault.Bridge
	agents  *coreAgents
	// launcher starts and resumes conversations, inbox delivers document comments (slice "launch").
	launcher *launch.Starter
	inbox    *launch.Inbox
	// api answers the hook and agent routes; push sends the web push notifications (nil when off).
	api  *coreapi.Config
	push *push.Sender
	// resumeAtStart: the core was on when the process started (set once in main).
	resumeAtStart atomic.Bool
	// sliceOn tells whether a slice is switched on (the gate's; set once in main).
	sliceOn func(slice string) bool
	// timeline holds the conversation sockets of the timeline slice.
	timeline *timeline.Server
	// reload types plugin and skill reloads into idle Claude tabs (standalone only; nil otherwise).
	reload *pluginReloader
	// v2 is the running v2 (conversations' actors and their protocol) while the core runs, nil otherwise; v2d is what it
	// is built from (set by main before the core starts).
	v2  atomic.Pointer[v2Live]
	v2d *deps

	mu      sync.Mutex
	m       *status.Manager
	prompts *prompts.Service
	sleeper *sleep.Sleeper
	cancel  context.CancelFunc
}

func newCore(cfg coreConfig) *core {
	c := &core{cfg: cfg, statusHub: sockets.NewHub(), sync: &sockets.SyncServer{Hub: sockets.NewHub()},
		history: &history.Store{File: filepath.Join(cfg.DataDir, "session-history.json")},
		limits:  &limits.Cache{File: filepath.Join(cfg.DataDir, "rate-limits.json")},
		bg:      &background.Reader{UID: os.Getuid()},
		claude:  claude.NewDetector(cfg.Home)}
	c.store = state.New(state.Config{DataDir: cfg.DataDir, Tmux: state.FromTmux(cfg.Tmux), Broadcast: syncBroadcaster{c.sync},
		Reconciler: reconciler{c}, Prompts: layoutPrompts{c}, AgentTokenHeader: cfg.AgentHeader, MetaKey: cfg.MetaKey, Home: cfg.Home})
	publish := func(e *jsvalue.Object) { c.sync.Broadcast(e) }
	c.panel = panel.NewBridge(publish, c.sync.Hub.Count)
	c.vault = vault.NewBridge(publish, c.sync.Hub.Count)
	c.agents = cfg.Agents
	c.newLauncher()
	c.timeline = c.newTimeline(cfg.Timeline)
	c.push = newPusher(cfg.DataDir, cfg.Push, cfg.Hooks)
	if cfg.Standalone {
		c.reload = newPluginReloader(c)
	}
	c.api = c.newHookAPI()
	c.intents = paneintents.New(filepath.Join(cfg.DataDir, "pane-intents.json"), nil, true)
	// Every change of a conversation's pane intents goes to the status sockets too: the machine agent relays them into
	// the cluster stream.
	c.intents.Subscribe(func(tabID string, intents []*jsvalue.Object) {
		if m := c.manager(); m != nil {
			list := make([]any, len(intents))
			for i, it := range intents {
				list[i] = it
			}
			m.Broadcast(jsvalue.NewObject("type", "pane-intents:update", "tabId", tabID, "intents", list))
		}
	})
	return c
}

// layoutPrompts writes the agents' per-workspace prompt files, from the deployment's templates.
type layoutPrompts struct{ c *core }

func (p layoutPrompts) WriteWorkspacePrompts(ws *jsvalue.Object) {
	p.c.writeWorkspacePrompts(ws.String("id"))
}

func (c *core) writeWorkspacePrompts(ids ...string) {
	if c.cfg.WorkspacePrompts == nil {
		return
	}
	for _, id := range ids {
		if err := c.cfg.WorkspacePrompts.Write(c.cfg.DataDir, id); err != nil {
			agent.Log("warn", "core: workspace prompts not written", agent.F{K: "workspace", V: id}, agent.F{K: "err", V: err.Error()})
		}
	}
}

// reportMismatch is a tab state the keeper corrected: logged and reported.
func (c *core) reportMismatch(x status.Mismatch) {
	nullable := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	actual := x.Actual
	if actual == "" {
		actual = "?"
	}
	agent.Log("info", "status mismatch: "+x.Kind+" "+x.Shown+"→"+actual, agent.F{K: "kind", V: x.Kind}, agent.F{K: "shown", V: x.Shown},
		agent.F{K: "actual", V: nullable(x.Actual)}, agent.F{K: "provider", V: nullable(x.Provider)}, agent.F{K: "durationMs", V: x.DurationMs},
		agent.F{K: "unhooked", V: x.Unhooked})
	if !c.cfg.Telemetry.Enabled() {
		return
	}
	var duration any
	if x.DurationMs != nil {
		duration = math.Max(0, math.Round(*x.DurationMs))
	}
	c.cfg.Telemetry.Push(telemetry.Event{Name: env("RUNTIME_TELEMETRY_MISMATCH_EVENT", "platform_status_mismatch"), At: time.Now(),
		Properties: jsvalue.NewObject("kind", x.Kind, "shown", x.Shown, "actual", nullable(x.Actual), "provider", nullable(x.Provider),
			"duration_ms", duration, "unhooked", x.Unhooked)})
}

func (c *core) manager() *status.Manager {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m
}

// promptService is the prompt queue and delivery while the core runs, else nil.
func (c *core) promptService() *prompts.Service {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prompts
}

// start runs the core: the stores cross-check the layouts against tmux, the keeper scans every tab, and the limits,
// background work and idle sleep start.
func (c *core) start() {
	c.mu.Lock()
	if c.m != nil {
		c.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.mu.Unlock()
	if err := c.store.Init(); err != nil {
		agent.Log("warn", "core: layout init failed", agent.F{K: "err", V: err.Error()})
	}
	if c.cfg.Standalone {
		var ids []string
		for _, ws := range (coreLayouts{store: c.store}).Workspaces() {
			ids = append(ids, ws.ID)
		}
		c.writeWorkspacePrompts(ids...)
	}
	providers := map[string]status.Provider{
		agents.Claude: claudeProvider{det: c.claude, snaps: claude.NewSnapshotReader(), hooks: filepath.Join(c.cfg.DataDir, "hooks.json")},
		agents.Codex:  codexProvider{det: codex.NewDetector(c.cfg.Home), snaps: codex.NewSnapshotReader()},
	}
	wiring := &promptWiring{c: c, providers: providers}
	wiring.resume = c.resumeForQueue
	svc := c.newPrompts(wiring)
	var m *status.Manager
	sleeper := c.newSleeper(func() *status.Manager { return m })
	var pusher status.Pusher
	if c.push != nil {
		pusher = c.push
	}
	m = status.New(status.Deps{
		Layouts:    coreLayouts{store: c.store, claude: c.claude, history: c.history},
		Tmux:       coreTmux{Tmux: c.cfg.Tmux, term: c.cfg.Term},
		Procs:      coreProcs{},
		Providers:  providers,
		Queue:      queueAdapter{svc},
		TabRemoved: tabRemoved(svc.TabRemoved, sleeper.ForgetTab),
		History:    c.history,
		Push:       pusher,
		PromptEnter: func(session string, _ float64) (float64, bool) {
			ms, ok := svc.EnterClock().Take(session)
			return float64(ms), ok
		},
		TurnEnd:          c.turnEnd,
		AgentLaunched:    c.reload.launched,
		AskFolderTrust:   env("RUNTIME_FOLDER_TRUST", "") == "ask",
		Mismatch:         c.reportMismatch,
		BackgroundTasks:  c.bg.Read,
		ForgetTranscript: c.bg.Forget,
		CodexRateLimits: func(t string) {
			if d := limits.CodexTranscriptLimits(t, float64(time.Now().UnixMicro())/1e6); d != nil {
				_, _ = c.limits.Write("codex", d)
			}
		},
		Log: coreLog,
	}, c.statusHub)
	wiring.m = m
	c.mu.Lock()
	c.m, c.prompts, c.sleeper = m, svc, sleeper
	c.mu.Unlock()
	m.Start(ctx)
	if c.v2d != nil && v2Enabled() {
		v := newV2Live(*c.v2d, c)
		v.start(ctx)
		c.v2.Store(v)
	}
	go c.runLimits(ctx, m)
	go sleeper.Run(ctx, 0, sleepReport)
	if c.reload != nil {
		go c.reload.run(ctx, m, svc)
	}
	agent.Log("info", "core started")
	// The old runtime resumes the agents that ran before a restart only while the core is off; the core was on when
	// this process started, so the restart is this one's to finish.
	if c.resumeAtStart.CompareAndSwap(true, false) && c.agents != nil {
		c.agents.AutoResume()
	}
}

func (c *core) runLimits(ctx context.Context, m *status.Manager) {
	hist := &limits.History{File: filepath.Join(c.cfg.DataDir, "limits-history.jsonl")}
	now := func() float64 { return float64(time.Now().UnixMicro()) / 1e6 }
	_ = limits.RotateHistory(hist.File, now())
	go limits.Watch(ctx, c.limits.File, func(data map[string]any) {
		m.RateLimitsChanged(data)
		_ = hist.Record(data, now())
	})
	(&limits.Pollers{Cache: c.limits, CodexbarBin: limits.CodexbarBin(c.cfg.CodexbarBin), CodexHome: c.cfg.CodexHome,
		Warn: func(msg string, err error) { agent.Log("warn", "core: "+msg, agent.F{K: "err", V: err.Error()}) }}).Run(ctx)
}

// stop ends the core; its sockets close and the machine agent dials again, to the old runtime.
func (c *core) stop() {
	c.mu.Lock()
	cancel, svc := c.cancel, c.prompts
	c.m, c.cancel, c.prompts, c.sleeper = nil, nil, nil, nil
	c.mu.Unlock()
	if v := c.v2.Swap(nil); v != nil {
		v.stop()
	}
	if svc != nil {
		svc.Stop()
	}
	if cancel != nil {
		cancel()
		c.sync.Hub.Shutdown()
		c.timeline.Shutdown()
		agent.Log("info", "core stopped")
	}
}

// statusSource is the status socket's view: the keeper, plus the pane intents.
type statusSource struct{ c *core }

func (s statusSource) m() *status.Manager { return s.c.manager() }
func (s statusSource) RateLimits() any {
	if m := s.m(); m != nil {
		return m.RateLimits()
	}
	return nil
}
func (s statusSource) Tabs() any {
	if m := s.m(); m != nil {
		return m.Tabs()
	}
	return map[string]any{}
}
func (s statusSource) Background() any {
	if m := s.m(); m != nil {
		return m.Background()
	}
	return map[string]any{}
}
func (s statusSource) Queues() any {
	if m := s.m(); m != nil {
		return m.Queues()
	}
	return map[string]any{}
}
func (s statusSource) Intents() any { return s.c.intents.Snapshot() }
func (s statusSource) History() any {
	if m := s.m(); m != nil {
		return m.History()
	}
	return []any{}
}
func (s statusSource) Dismiss(tabID string, exclude *sockets.Client) {
	if m := s.m(); m != nil {
		m.Dismiss(tabID, exclude)
	}
}
func (s statusSource) Ack(tabID string, seq float64) {
	if m := s.m(); m != nil {
		m.Ack(tabID, seq)
	}
}
func (s statusSource) IntentsApplied(tabID string, ids []string, device string) {
	s.c.intents.MarkApplied(tabID, ids, device)
}
func (s statusSource) Warn(msg string) { agent.Log("warn", "status socket: "+msg) }

// routes are the core's: the two sockets (slice "core") and the internal routes the old runtime calls while it still
// runs the timeline, agent start and the rest.
func (c *core) routes(socketAuth func(*http.Request) bool, cliToken func(*http.Request) bool) []gate.Route {
	statusSrv := &sockets.StatusServer{Hub: c.statusHub, Source: statusSource{c}}
	upgrade := func(path string) func(*http.Request) bool {
		return func(r *http.Request) bool { return isUpgrade(r) && pathIs(r, path) }
	}
	guard := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !socketAuth(r) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			h.ServeHTTP(w, r)
		})
	}
	internal := func(path string, h func(w http.ResponseWriter, r *http.Request, m *status.Manager)) gate.Route {
		return gate.Route{Match: func(r *http.Request) bool { return r.URL.Path == "/api/internal/core/"+path },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !cliToken(r) {
					writeCoreJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
					return
				}
				m := c.manager()
				if m == nil {
					writeCoreJSON(w, http.StatusConflict, map[string]string{"error": "the Go core is off"})
					return
				}
				h(w, r, m)
			})}
	}
	return []gate.Route{
		{Slice: "core", Match: upgrade("/api/status"), Handler: guard(statusSrv)},
		{Slice: "core", Match: upgrade("/api/sync"), Handler: guard(c.sync)},
		internal("tabs", c.handleTabs),
		internal("layout", c.handleLayoutOp),
		internal("agent-launch", func(w http.ResponseWriter, r *http.Request, m *status.Manager) {
			b := readCoreBody(r)
			m.MarkAgentLaunch(b.String("tabId"), boolField(b, "resetAgentSession"))
			w.WriteHeader(http.StatusNoContent)
		}),
		internal("agent-gone", func(w http.ResponseWriter, r *http.Request, m *status.Manager) {
			b := readCoreBody(r)
			m.MarkAgentGone(b.String("tabId"), boolField(b, "endLaunch"))
			w.WriteHeader(http.StatusNoContent)
		}),
		internal("last-user-message", func(w http.ResponseWriter, r *http.Request, m *status.Manager) {
			b := readCoreBody(r)
			m.NotifyLastUserMessage(b.String("session"), b.String("message"))
			w.WriteHeader(http.StatusNoContent)
		}),
		internal("register-tab", func(w http.ResponseWriter, r *http.Request, m *status.Manager) {
			b := readCoreBody(r)
			if e := b.Object("entry"); e != nil {
				m.RegisterTab(b.String("tabId"), entryFromJSON(e))
			}
			w.WriteHeader(http.StatusNoContent)
		}),
	}
}

func writeCoreJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(nodejson.Marshal(v))
}

func readCoreBody(r *http.Request) *jsvalue.Object {
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	v, err := jsvalue.Parse(raw)
	if o, ok := v.(*jsvalue.Object); err == nil && ok {
		return o
	}
	return jsvalue.NewObject()
}

// handleTabs answers the old runtime's synchronous reads: every tab with what the page sees and what the agent
// side keeps.
func (c *core) handleTabs(w http.ResponseWriter, _ *http.Request, m *status.Manager) {
	sources := map[string]status.AgentSource{}
	for _, s := range m.AgentSources() {
		sources[s.TabID] = s
	}
	var clients map[string]json.RawMessage
	raw, _ := json.Marshal(m.Tabs())
	_ = json.Unmarshal(raw, &clients)
	var order []string
	if v, err := jsvalue.Parse(raw); err == nil {
		if o, ok := v.(*jsvalue.Object); ok {
			order = o.Keys()
		}
	}
	tabs := []any{}
	for _, id := range order {
		var client map[string]any
		_ = json.Unmarshal(clients[id], &client)
		info, _ := m.Tab(id)
		s := sources[id]
		nullable := func(v string) any {
			if v == "" {
				return nil
			}
			return v
		}
		tabs = append(tabs, map[string]any{"tabId": id, "tmuxSession": info.TmuxSession, "jsonlPath": nullable(info.Transcript),
			"provider": nullable(info.Provider), "panelType": nullable(info.PanelType), "cliState": info.CliState, "eventSeq": s.EventSeq,
			"lastEventAt": s.LastEventAt, "lastEventName": nullable(s.LastEventName), "lastStartedAt": s.LastStartedAt,
			"agentSessionId": nullable(info.AgentSessionID), "client": client})
	}
	writeCoreJSON(w, http.StatusOK, map[string]any{"tabs": tabs})
}

// handleLayoutOp is one layout or workspace operation of the old runtime, so the layout files keep one writer.
func (c *core) handleLayoutOp(w http.ResponseWriter, r *http.Request, _ *status.Manager) {
	b := readCoreBody(r)
	session := b.String("session")
	field := func(k string) *any {
		v, has := b.Get(k)
		if !has {
			return nil
		}
		return &v
	}
	var result any
	var err error
	switch b.String("op") {
	case "agentState":
		err = c.store.UpdateTabAgentState(session, state.Provider(b.String("provider")),
			state.AgentState{SessionID: field("sessionId"), JsonlPath: field("jsonlPath"), Summary: field("summary"), LastUserMessage: field("lastUserMessage")})
	case "recoveredSession":
		err = c.store.StoreRecoveredAgentSession(session, state.Provider(b.String("provider")), b.String("sessionId"), b.String("jsonlPath"))
	case "lastUserMessage":
		v, _ := b.Get("message")
		err = c.store.UpdateTabLastUserMessage(session, v)
	case "cliStatus":
		event, _ := b.Get("lastEventName")
		err = c.store.UpdateTabCliStatus(session, b.String("cliState"), field("dismissedAt"), event)
	case "meta":
		patch := b.Object("patch")
		if patch == nil {
			patch = jsvalue.NewObject()
		}
		var meta *jsvalue.Object
		if meta, err = c.store.UpdateTabMeta(session, patch); err == nil && meta != nil {
			result = meta
		}
	case "reconcileCwd":
		err = c.store.ReconcileTabCwd(session)
	case "getLayout":
		result, err = c.store.GetLayout(b.String("workspaceId"), b.String("defaultCwd"))
	case "addTab":
		var tab *jsvalue.Object
		tab, err = c.store.AddTab(b.String("workspaceId"), b.String("paneId"), state.TabOptions{Name: b.String("name"), Cwd: b.String("cwd"),
			PanelType: b.String("panelType"), Command: b.String("command")})
		if err == nil && tab != nil {
			result = tab
		}
	case "createWorkspace":
		result, err = c.store.CreateWorkspace(b.String("directory"), b.String("name"), b.String("panelType"))
	default:
		writeCoreJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown operation"})
		return
	}
	if err != nil {
		status := http.StatusInternalServerError
		if se, ok := err.(*state.Error); ok {
			status = se.Status
		}
		writeCoreJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeCoreJSON(w, http.StatusOK, map[string]any{"result": jsonValue(result)})
}

// jsonValue writes a jsvalue document as JavaScript would.
func jsonValue(v any) any {
	if o, ok := v.(*jsvalue.Object); ok {
		return json.RawMessage(jsvalue.Stringify(o, ""))
	}
	return v
}

// entryFromJSON reads an entry a route of the old runtime built (its client fields plus the tmux session).
func entryFromJSON(o *jsvalue.Object) status.Entry {
	str := func(k string) status.Opt[string] {
		v, has := o.Get(k)
		if !has {
			return status.Opt[string]{}
		}
		if s, ok := v.(string); ok {
			return status.Some(s)
		}
		return status.Null[string]()
	}
	e := status.Entry{CliState: o.String("cliState"), WorkspaceID: o.String("workspaceId"), TabName: o.String("tabName"),
		TmuxSession: o.String("tmuxSession"), PanelType: str("panelType"), AgentProviderID: str("agentProviderId"),
		AgentSessionID: str("agentSessionId"), LastUserMessage: str("lastUserMessage"), JSONLPath: o.String("jsonlPath")}
	if v, has := o.Get("eventSeq"); has {
		if f, ok := v.(float64); ok {
			e.EventSeq = int(f)
		}
	}
	if e.CliState == "" {
		e.CliState = status.Inactive
	}
	return e
}

func boolField(o *jsvalue.Object, k string) bool {
	v, _ := o.Get(k)
	b, _ := v.(bool)
	return b
}
