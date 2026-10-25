package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/catalog"
	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/delegation"
	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/launch"
	"github.com/ly-solutions/harnessly/internal/limits"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/machine"
	"github.com/ly-solutions/harnessly/internal/outbox"
	"github.com/ly-solutions/harnessly/internal/preflight"
	"github.com/ly-solutions/harnessly/internal/prompts"
	"github.com/ly-solutions/harnessly/internal/sessionmeta"
	"github.com/ly-solutions/harnessly/internal/shots"
	"github.com/ly-solutions/harnessly/internal/state"
	"github.com/ly-solutions/harnessly/internal/voice"
	"github.com/ly-solutions/harnessly/internal/wire"
	"github.com/ly-solutions/harnessly/internal/workflows"
)

// v2Features are what a live runtime announces: what its conversations do.
var v2Features = []wire.Feature{wire.FeatureQueue, wire.FeatureSteer, wire.FeatureQuestions, wire.FeaturePermissions, wire.FeatureBackground, wire.FeatureModel}

// v2Live is the v2 of a running core: the conversations' actors (internal/convo) on the machine's tmux and processes,
// served over the socket, the commands and the pages of internal/wire. It lives and dies with the core, beside the
// keeper: the keeper still reports the tabs to the Legacy pages, the actors serve v2, and one pane is typed into by
// one of them at a time (typist's lock).
//
// Nothing of what the pages wait for goes through the keeper's lock or through a tmux call: the socket, the commands
// and the pages answer from the actors and the hub; the sync below runs on a goroutine of its own.
type v2Live struct {
	c        *core
	d        deps
	hub      *wire.Hub
	rt       *convo.Runtime
	srv      *wire.Server
	machine  *liveMachine
	tm       *v2Tmux
	starter  *v2Launcher
	terms    *v2Terminals // nil when the core has no terminal server
	designly *v2Designly  // nil when the core cannot start agents
	// delegation is the agents' delegation of chats to other agents (v2_delegate.go).
	delegation *delegation.Service

	ready    atomic.Bool
	kick     chan struct{}
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	attached map[string]attachedAs // only the sync loop touches it
	heads    launch.Headlines      // the transcripts' titles, as Legacy reads them
}

type attachedAs struct{ transcript, agentSession string }

func newV2Live(d deps, c *core) *v2Live {
	v := &v2Live{c: c, d: d, kick: make(chan struct{}, 1), attached: map[string]attachedAs{}}
	version := env("AGENT_VERSION", agent.BuildVersion)
	features := slices.Clone(v2Features)
	if c.cfg.Term != nil && c.cfg.Tmux != nil {
		features = append(features, wire.FeatureTerminal)
	}
	if c.launcher != nil && c.agents != nil {
		features = append(features, wire.FeatureDesignly)
	}
	// The Machine tool reads what the machine slice looks at the machine with; it is up a moment after the start.
	features = append(features, wire.FeatureMachine)
	// The Apps screen says for each app whether this machine reads it, so it is always offered.
	features = append(features, wire.FeatureApps, wire.FeatureAppsHub)
	// The Settings screen reads the agent CLIs' files of this machine, which are there to read or to make.
	features = append(features, wire.FeatureSettings)
	// The composers' drafts are kept here for every device of the person (draft.set, drafts.json).
	features = append(features, wire.FeatureDrafts)
	// The queue's messages can be put in another order (queue.move).
	features = append(features, wire.FeatureQueueMove)
	// A chat's terminal can be stopped (terminal.stop), and idle ones stop by themselves (runtime-settings): tmux's.
	if c.cfg.Tmux != nil {
		features = append(features, wire.FeatureTerminalStop)
		// A rewind stops the terminal before it forks the transcript, and the next message resumes the new session.
		features = append(features, wire.FeatureRewind)
	}
	// A conversation can be forked, merged back and handed to another agent: the agents are started here.
	if c.launcher != nil && c.agents != nil {
		features = append(features, wire.FeatureFork, wire.FeatureHandoff)
	}
	// A chat held on its usage limit goes on at the reset, and one a restart cut short goes on (runtime-settings).
	features = append(features, wire.FeatureAutoContinue)
	// Dictation is announced when this machine is set up for it at start: ffmpeg and the recognition service's socket.
	var dictation *voice.Server
	if dv := newVoice(); dv.Available() {
		dictation = dv
		features = append(features, wire.FeatureVoice)
	}
	v.machine = newLiveMachine(v2Machine, version, features, d.DataDir, func() int { return v.rt.Busy() }, c.preflight, c.v2Models)
	v.hub = wire.NewHub(v.machine)
	v.tm = &v2Tmux{tm: c.cfg.Tmux}
	v.starter = &v2Launcher{v: v}
	uploads := &wire.Uploads{Dir: filepath.Join(d.DataDir, "v2", "uploads"), Limits: wire.DefaultLimits}
	ses := newV2Sessions(d.Home, func() map[string]string { return v.rt.AttachedSessions() })
	publisher := autoOpenPublisher{hub: v.hub, watch: v.newPaneWatcher()}
	// The terminals are stopped by tmux and /proc; without tmux there is nothing to stop.
	runtimeSettings := &wire.FileRuntimeSettings{Path: filepath.Join(d.DataDir, "v2", "runtime-settings.json")}
	var terminals convo.Terminals
	if c.cfg.Tmux != nil {
		terminals = newV2Stopper(v, c.cfg.Tmux)
	}
	v.rt = convo.New(convo.Deps{AgentSession: ses.Find, MachineID: v2Machine, Publisher: publisher, Machine: v.tm, Launcher: v.starter,
		Uploads: v2Uploads{uploads}, Refs: v2Refs{v}, Store: convo.NewStore(filepath.Join(d.DataDir, "v2")), Log: v2Log{}, Limits: wire.DefaultLimits, Models: v.machine.modelsOf, Background: v2Background{reader: c.bg, flows: &workflows.Reader{}}, Meta: &sessionmeta.Reader{}, Shots: &shots.Reader{}, PictureRoots: []string{shots.ScratchRoot(), uploadsDir(d), uploads.Dir}, Titler: newV2Titler(os.Getenv("RUNTIME_SIDE_CHAT_CONFIG")).Title, Closer: v2Closer{v}, DefaultCwd: d.WorkspaceRoot,
		Terminals: terminals, IdleStop: func() time.Duration { return runtimeSettings.RuntimeSettings().IdleStop.After() },
		Rewinder: v2Rewinder{c},
		AutoContinue: func() (bool, bool) {
			s := runtimeSettings.RuntimeSettings()
			return s.LimitContinue != wire.Off, s.RestartContinue != wire.Off
		},
		LimitReset: func(p wire.Provider, now time.Time) wire.Ms {
			return wire.Ms(limits.ExhaustedReset(c.limits.Read()[string(p)], float64(now.Unix())) * 1000)
		}})
	v.delegation = newDelegation(v, runtimeSettings)
	v.srv = v2Server(d, v.hub, v.rt, v.machine, features, false)
	v.srv.Uploads = uploads
	if terminals != nil {
		v.srv.RuntimeSettings = runtimeSettings
	}
	v.srv.Sessions = ses.List
	if dictation != nil {
		v.srv.Voice = v2Voice(dictation)
	}
	sc := &v2SideChat{record: v.rt.Record, home: d.Home, runtimeFile: os.Getenv("RUNTIME_SIDE_CHAT_CONFIG")}
	if c.cfg.Tmux != nil {
		sc.cwdOf = func(session string) string { return machine.SessionDir(c.cfg.Tmux, session) }
	}
	v.srv.SideChat = sc.Serve
	v.srv.HandoffDraft = (&v2Handoff{rt: v.rt, runtimeFile: os.Getenv("RUNTIME_SIDE_CHAT_CONFIG")}).Draft
	v.srv.Files = &v2Files{record: v.rt.Record, touched: v.rt.Touched}
	v.srv.Tasks = v.rt.Tasks
	if terminals != nil {
		v.srv.Rewind = v.rt
	}
	v.srv.MachineTool = v2MachineTool{ref: d.MachineKit}
	v.srv.Apps = v2Apps{d: d}
	handlers, _ := handlersFor(d)
	checker := newMRChecker(handlers.S)
	open := v2Open{record: v.rt.Record, targets: panelTargets(), mrLinks: transcriptMRs(d.Home), mr: checker.lookup, projects: checker.projects, state: &mrState{}}
	v.srv.Open = open.Open
	v.srv.OpenAll = open.OpenAll
	v.srv.AgentSettings = newV2AgentSettings(d)
	v.srv.Context = v.rt.Context
	v.srv.Shot = func(ctx context.Context, conv, id string) (wire.ShotFile, error) {
		var f shots.File
		path, err := v.rt.ShotPath(ctx, conv, id)
		switch {
		case err == nil:
			f, err = shots.Open(path)
		case errors.Is(err, wire.ErrNoShot):
			// A picture a message names: checked against the conversation's folders again, as it is opened.
			var roots []string
			if path, roots, err = v.rt.PicturePath(ctx, conv, id); err != nil {
				return wire.ShotFile{}, err
			}
			f, err = shots.OpenIn(path, roots)
		default:
			return wire.ShotFile{}, err
		}
		if err != nil {
			return wire.ShotFile{}, wire.ErrNoShot
		}
		return wire.ShotFile{Body: f.Body, Mime: f.Mime, ModTime: f.ModTime}, nil
	}
	if c.cfg.Tmux != nil {
		tmux := c.cfg.Tmux
		v.srv.Legacy = func(_ context.Context, conv string) (wire.LegacyTab, error) {
			return legacyTabOf(v.rt.Record, tmux.ParseSessionName, conv)
		}
	}
	if c.launcher != nil && c.agents != nil {
		api := designAPI(d)
		v.designly = &v2Designly{v: v, api: api, file: filepath.Join(d.DataDir, "v2", "designly.json"),
			links: design.NewChatLinks(api.L, filepath.Join(d.DataDir, "v2", "designly-chats.json"))}
		v.srv.Designly = v.designly
	}
	if slices.Contains(features, wire.FeatureTerminal) {
		v.terms = &v2Terminals{rt: v.rt, tmux: c.cfg.Tmux, term: c.cfg.Term}
		v.srv.Terminals = v.terms
	}
	return v
}

// preflight is the core's check of the agent CLIs (kept for half a minute); nothing found when the core has no agents.
func (c *core) preflight() preflight.Result {
	if c.agents == nil {
		return preflight.Result{}
	}
	return c.agents.runtime()
}

// v2Models are the models the machine can start an agent CLI with, in the wire's words: Claude's aliases, Codex's
// models cache. Only the efforts the wire has are listed; the first model is the one a new conversation offers.
func (c *core) v2Models(p wire.Provider) []wire.AgentModel {
	out := []wire.AgentModel{}
	if c.agents == nil {
		return out
	}
	opts := c.agents.options.Launch(string(p))
	for _, m := range opts.Models {
		am := wire.AgentModel{ID: m.ID, Label: m.Label, Hint: v2ModelHint(p, m), Efforts: []wire.Effort{}}
		for _, e := range m.Efforts {
			if we := wire.Effort(e); slices.Contains(wire.Efforts, we) {
				am.Efforts = append(am.Efforts, we)
			}
		}
		// Claude's catalog lists ultracode among the levels --effort takes; on the wire it is a mode of its own.
		am.Ultracode = p == wire.ProviderClaude && slices.Contains(m.Efforts, string(wire.EffortUltracode))
		if def := v2DefaultEffort(p, m, opts); def != nil && slices.Contains(am.Efforts, *def) {
			am.DefaultEffort = def
		}
		out = append(out, am)
	}
	return out
}

// v2ModelHint is the one line the page shows under a model: Codex's own description from its model cache; Claude Code
// lists no descriptions for its aliases, so these say what the families are for.
func v2ModelHint(p wire.Provider, m catalog.Model) string {
	if p == wire.ProviderCodex {
		return m.Description
	}
	switch m.ID {
	case "opus":
		return locale.Pick("The strongest", "Самая сильная")
	case "sonnet":
		return locale.Pick("For everyday work", "Для повседневной работы")
	case "haiku":
		return locale.Pick("For simple edits", "Для простых правок")
	case "fable":
		return locale.Pick("New, for long tasks", "Новая, для длинных задач")
	}
	return ""
}

// claudeDefaultEffort is the effort each Claude alias runs on when neither the person nor the environment chose one:
// the default_effort of the model the alias names in the model catalog bundled in Claude Code 2.1.308 and 2.1.309
// (opus claude-opus-5-5, sonnet claude-sonnet-5-5, fable claude-fable-5-1). Haiku has no choice of depth, so no default.
var claudeDefaultEffort = map[string]wire.Effort{"opus": wire.EffortMedium, "sonnet": wire.EffortMedium, "fable": wire.EffortHigh}

// v2DefaultEffort is the effort a model runs on when none is asked for. Codex's cache names it per model. Claude Code
// takes, in this order, CLAUDE_CODE_EFFORT_LEVEL of the environment it runs in, the effortLevel the person saved in its
// user settings, then the model's own default. A project's settings can still override it for one conversation.
func v2DefaultEffort(p wire.Provider, m catalog.Model, opts catalog.LaunchOptions) *wire.Effort {
	var level string
	switch {
	case p == wire.ProviderCodex:
		if m.DefaultEffort != nil {
			level = *m.DefaultEffort
		}
	case os.Getenv("CLAUDE_CODE_EFFORT_LEVEL") != "":
		level = os.Getenv("CLAUDE_CODE_EFFORT_LEVEL")
	case opts.CliDefault.Effort != nil:
		level = *opts.CliDefault.Effort
	default:
		level = string(claudeDefaultEffort[m.ID])
	}
	we := wire.Effort(level)
	if level == "" || !slices.Contains(wire.Efforts, we) {
		return nil
	}
	return &we
}

// start loads what the actors wrote, takes over the Legacy tabs and their queues, and begins the sync. It returns
// once the sockets can be served.
func (v *v2Live) start(ctx context.Context) {
	ctx, v.cancel = context.WithCancel(ctx)
	if err := v.rt.Start(ctx); err != nil {
		agent.Log("warn", "v2: some of the saved state is unreadable", agent.F{K: "error", V: err.Error()})
	}
	if n := v.importTabs(); n > 0 {
		agent.Log("info", "v2: Legacy tabs became conversations", agent.F{K: "count", V: n})
	}
	v.importProjects()
	v.ready.Store(true)
	v.wg.Add(2)
	go func() { defer v.wg.Done(); v.machine.run(ctx) }()
	go func() { defer v.wg.Done(); v.syncLoop(ctx) }()
	if v.designly != nil {
		v.wg.Add(1)
		go func() { defer v.wg.Done(); v.designly.backfill(ctx) }()
	}
	v.wg.Add(1)
	go func() { defer v.wg.Done(); v.serveDelegation(ctx.Done()) }()
	agent.Log("info", "v2 started")
}

func (v *v2Live) stop() {
	v.ready.Store(false)
	if v.cancel != nil {
		v.cancel()
	}
	v.wg.Wait()
	v.hub.Close()
	v.rt.Stop()
}

// --- Legacy tabs ---

// layoutTabs are the agent tabs of every layout, as conversations to import.
func (v *v2Live) layoutTabs() []convo.ImportTab {
	var out []convo.ImportTab
	layouts := coreLayouts{store: v.c.store}
	for _, ws := range layouts.Workspaces() {
		layout := v.c.store.Layout(ws.ID)
		if layout == nil {
			continue
		}
		for _, t := range state.AllTabs(layout) {
			cwd := t.String("cwd")
			if cwd == "" && len(ws.Directories) > 0 {
				cwd = ws.Directories[0]
			}
			if it, ok := convo.TabFromLayout(t, cwd); ok {
				it.LaunchUltracode = v.c.tabLaunchedUltracode(t)
				if it.Provider == wire.ProviderClaude && it.TranscriptPath != "" {
					if h := v.heads.Read(it.TranscriptPath); h.Title != nil && *h.Title != "" {
						it.Title = *h.Title
					}
				}
				out = append(out, it)
			}
		}
	}
	return out
}

// importTabs makes conversations of the agent tabs v2 does not hold yet. The messages waiting in the Legacy queue of
// such a tab move with it: they are v2's now, and leave the Legacy queue so that only one of them types them.
func (v *v2Live) importTabs() int {
	var fresh []convo.ImportTab
	for _, t := range v.layoutTabs() {
		if v.rt.Has(t.ID) {
			v.rt.RetitleImported(t)
		} else if t.Session == "" || v.rt.ConvOfSession(t.Session) == "" {
			fresh = append(fresh, t)
		}
	}
	if len(fresh) == 0 {
		return 0
	}
	queues := map[string][]outbox.LegacyItem{}
	svc := v.c.promptService()
	if svc != nil {
		all := svc.Queue().All()
		for _, t := range fresh {
			for _, it := range all[t.ID] {
				queues[t.ID] = append(queues[t.ID], outbox.LegacyItem{ID: it.ID, Text: it.Text, Images: it.Images, CreatedAt: it.CreatedAt})
			}
		}
	}
	n := v.rt.Import(fresh, queues)
	v.importProjects()
	if svc != nil {
		for id := range queues {
			if v.rt.Has(id) {
				svc.Queue().Drop(id)
			}
		}
	}
	return n
}

// importProjects puts the conversations Legacy's organizer has in a project into that project, once each.
func (v *v2Live) importProjects() {
	file := serviceConfig(v.d).OrganizerFile
	if file == "" {
		return
	}
	projects, assign, err := convo.LegacyOrganizer(file)
	if err != nil {
		agent.Log("warn", "v2: Legacy projects unreadable", agent.F{K: "error", V: err.Error()})
		return
	}
	if n := v.rt.ImportProjects(projects, assign); n > 0 {
		agent.Log("info", "v2: Legacy projects taken over", agent.F{K: "conversations", V: n})
	}
}

// legacyTabOf is the Legacy tab a conversation runs in: the tab its tmux session names (one v2 started) or the
// conversation itself (one taken over from Legacy), with its transcript.
func legacyTabOf(record func(string) (wire.Conversation, bool), parse func(string) (string, string, string, bool),
	conv string) (wire.LegacyTab, error) {
	rec, ok := record(conv)
	if !ok || rec.Session == "" {
		return wire.LegacyTab{}, convo.ErrNotFound
	}
	// A tab taken over from Legacy is the conversation itself, whatever the prefix its session was named with.
	ws, _, tab, ok := parse(rec.Session)
	if !ok {
		ws, tab = "", rec.ID
	}
	out := wire.LegacyTab{TabID: tab, WorkspaceID: ws, SessionName: rec.Session, Cwd: rec.Cwd}
	if rec.TranscriptPath != "" {
		out.JsonlPath = &rec.TranscriptPath
	}
	if rec.AgentSessionID != "" {
		out.AgentSessionID = &rec.AgentSessionID
	}
	return out, nil
}

// --- the keeper's view, kept in step ---

// syncLoop attaches each conversation to the transcript and agent session the keeper found for its tab, and takes
// over tabs that appeared in the Legacy pages. Its own goroutine: the keeper's lock is held by the Legacy side for as
// long as that side wants.
func (v *v2Live) syncLoop(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-v.kick:
			select { // let a burst of hooks settle
			case <-time.After(300 * time.Millisecond):
			case <-ctx.Done():
				return
			}
		}
		v.syncOnce()
	}
}

func (v *v2Live) syncOnce() {
	m := v.c.manager()
	if m == nil {
		return
	}
	unknown := false
	for _, s := range m.AgentSources() {
		if s.TmuxSession == "" {
			continue
		}
		id := v.rt.ConvOfSession(s.TmuxSession)
		if id == "" {
			unknown = true
			continue
		}
		now := attachedAs{s.Transcript, s.AgentSessionID}
		if prev := v.attached[id]; prev != now && (now.transcript != "" || now.agentSession != "") {
			if v.rt.Attach(id, s.TmuxSession, now.transcript, now.agentSession) == nil {
				v.attached[id] = now // a mailbox that stayed full is tried again at the next turn of the loop
			}
		}
	}
	// A tab seen before its launch came back is not a stranger: the launch names it in a moment.
	if unknown && v.starter.starting.Load() == 0 {
		v.importTabs()
	}
}

// asked tells the sync that something is not known yet.
func (v *v2Live) asked() {
	select {
	case v.kick <- struct{}{}:
	default:
	}
}

// hook feeds the conversation of a tmux session a hook the agent fired. Called from the hook route after the keeper
// took it: a conversation not known yet is imported by the sync, and the first look at its pane catches up.
func (v *v2Live) hook(session string, ev agents.Event) {
	if !v.ready.Load() {
		return
	}
	switch ev.Kind {
	case agents.SessionStart, agents.PromptSubmit, agents.Notification, agents.Stop, agents.StopFailure, agents.PreCompact, agents.PostCompact:
	default:
		return
	}
	id := v.rt.ConvOfSession(session)
	if id == "" {
		if _, _, tab, ok := v.c.cfg.Tmux.ParseSessionName(session); ok && v.rt.Has(tab) {
			id = tab
		}
	}
	if id == "" {
		v.asked()
		return
	}
	_ = v.rt.Hook(id, convo.Hook{Kind: string(ev.Kind), Notification: ev.NotificationType})
}

// --- starting agents ---

// v2Launcher starts and resumes the agents of v2 conversations through the core's starter.
type v2Launcher struct {
	v        *v2Live
	starting atomic.Int32
	// promptFiles are the instruction files a launch in a folder takes, set by the one who creates the conversation
	// (the design library's) for the folder it is made in; Launch takes them.
	promptFiles sync.Map // cwd → []string
}

var _ convo.Launcher = (*v2Launcher)(nil)

// Launch returns at once; the start, which sends the agent's command line to a new tab, runs beside the actor.
func (l *v2Launcher) Launch(req convo.LaunchRequest) error {
	c := l.v.c
	if c.launcher == nil || c.agents == nil {
		return errors.New("the core cannot start agents")
	}
	var chosen *agents.Launch
	if req.Model != nil {
		var effort any
		if req.Model.Effort != nil {
			effort = string(*req.Model.Effort)
		}
		var msg string
		if chosen, msg = c.agents.ValidateLaunch(string(req.Provider), req.Model.ID, effort); msg != "" {
			return errors.New(msg)
		}
		if chosen != nil && req.Model.Ultracode && req.Provider == wire.ProviderClaude {
			chosen.Ultracode = true
		}
	}
	var files []string
	if f, ok := l.promptFiles.Load(req.Cwd); ok {
		files = f.([]string)
	}
	l.starting.Add(1)
	go func() {
		defer l.starting.Add(-1)
		l.start(req, chosen, files)
	}()
	return nil
}

func (l *v2Launcher) start(req convo.LaunchRequest, chosen *agents.Launch, promptFiles []string) {
	v, rt := l.v, l.v.rt
	rt.LaunchStep(req.ID, wire.StepCli)
	opts := launch.Options{Provider: string(req.Provider), Cwd: req.Cwd, CwdRoots: []string{req.Cwd}, Launch: chosen,
		ResumeSessionID: req.ResumeSessionID, ForkSession: req.Fork, PromptFiles: promptFiles}
	if req.First != "" {
		opts.Prompt = &req.First
	}
	res, err := v.c.launcher.Start(opts)
	if err != nil || !res.OK {
		reason := wire.LaunchFailedNoSession
		if !res.OK && res.Status == http.StatusConflict {
			reason = wire.LaunchFailedCliMissing
		}
		msg := ""
		if err != nil {
			msg = err.Error()
		} else if res.Body != nil {
			msg = res.Body.String("error")
		}
		agent.Log("warn", "v2: the agent did not start", agent.F{K: "conversation", V: req.ID}, agent.F{K: "error", V: msg})
		rt.LaunchFailed(req.ID, reason)
		return
	}
	tabID := res.ConversationID()
	found, ok := v.c.FindTab(tabID)
	if !ok || found.Tab.SessionName == "" {
		rt.LaunchFailed(req.ID, wire.LaunchFailedNoSession)
		return
	}
	rt.LaunchStep(req.ID, wire.StepPrompt)
	_ = rt.Attach(req.ID, found.Tab.SessionName, "", "")
}

// Resume brings a stopped agent back on its session.
func (l *v2Launcher) Resume(conv wire.Conversation) error {
	c := l.v.c
	_, _, tab, ok := c.cfg.Tmux.ParseSessionName(conv.Session)
	if !ok {
		return errors.New("the conversation has no tab to resume")
	}
	switch got := c.resumeForQueue(tab); got {
	case prompts.ResumeResumed, prompts.ResumeRunning:
		return nil
	default:
		return errors.New("not resumed: " + got)
	}
}

// v2Closer ends the agent of a deleted conversation the way closing its tab in the Legacy pages does: the tmux session
// and the layout tab go, and the keeper forgets the tab.
type v2Closer struct{ v *v2Live }

var _ convo.Closer = v2Closer{}

func (cl v2Closer) Close(conv wire.Conversation) error {
	core := cl.v.c
	ws, pane, tab, ok := core.cfg.Tmux.ParseSessionName(conv.Session)
	if !ok {
		return errors.New("not a session of this runtime: " + conv.Session)
	}
	if cl.v.terms != nil {
		cl.v.terms.closeAll(conv.ID)
	}
	err := core.store.RemoveTab(ws, pane, tab)
	if err != nil {
		// A tab already gone from the layout still leaves its session to end.
		core.cfg.Tmux.KillSession(conv.Session)
	}
	if m := core.manager(); m != nil {
		m.RemoveTab(tab)
	}
	return nil
}
