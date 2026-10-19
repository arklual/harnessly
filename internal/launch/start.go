// Package launch starts and resumes agent conversations the platform's way: a tab in a workspace's first pane, the
// agent CLI's launch line with its environment and initial prompt typed into it, the tab's metadata, and the keeper told
// that a launch is under way. It also resumes, after a restart or a tmux reset, the agents that were working, and
// delivers a person's document comments to the conversation a document is bound to.
package launch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"harnessly/internal/agents"
	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
	"harnessly/internal/locale"
	"harnessly/internal/state"
	"harnessly/internal/status"
)

// Lines are the agent CLIs' launch lines.
type Lines interface {
	// Launch starts a new conversation; promptFiles go into Claude's system prompt after the workspace's.
	Launch(provider, workspaceID string, launch *agents.Launch, promptFiles []string) (string, error)
	Resume(provider, sessionID, workspaceID string, launch *agents.Launch) (string, error)
}

// Keeper is the running tab keeper.
type Keeper interface {
	RegisterTab(tabID string, e status.Entry)
	MarkAgentLaunch(tabID string, reset bool)
	AgentSources() []status.AgentSource
}

// Config is what the starter works with.
type Config struct {
	Store *state.Store
	Lines Lines
	// Availability is nil when the agent of a panel type can run.
	Availability func(panelType string) *agents.Unavailable
	// Keeper is the running keeper; nil while there is none.
	Keeper func() Keeper
	// WorkspaceRoot is the machine's project root ("" when the deployment names none): new conversations start there,
	// and an explicit launch folder must lie inside it.
	WorkspaceRoot string
	// Home is where a first workspace is made without a project root.
	Home string
	// DataDir holds the instruction files a launch may add; DataDirLabel names it in messages (`~/.<name>`).
	DataDir, DataDirLabel string
	// EnvPrefix starts every variable a launch may set (`<prefix>NAME='value' ` before the line).
	EnvPrefix string
	// PromptDir takes a prompt too long to type, read back and removed by the launch line.
	PromptDir string
	// FirstWorkspaceName names the workspace made for a machine that has none.
	FirstWorkspaceName string
	// Now is the clock in epoch milliseconds.
	Now func() float64
	// ShellReady is how long new tmux sessions get before a resume line is typed (500 ms).
	ShellReady time.Duration
}

// Starter starts conversations.
type Starter struct{ cfg Config }

// New is a starter over cfg.
func New(cfg Config) *Starter {
	if cfg.Now == nil {
		cfg.Now = func() float64 { return float64(time.Now().UnixMilli()) }
	}
	if cfg.ShellReady == 0 {
		cfg.ShellReady = 500 * time.Millisecond
	}
	if cfg.FirstWorkspaceName == "" {
		cfg.FirstWorkspaceName = "Workspace"
	}
	return &Starter{cfg: cfg}
}

// EnvEntry is one entry of a requested environment, as the request held it (a value that is not a string is refused).
type EnvEntry struct {
	Key   string
	Value any
}

// Options say what to start.
type Options struct {
	Provider    string
	WorkspaceID string
	// Prompt is the first message; nil for none.
	Prompt *string
	// Launch is the checked model and effort; nil or both nil is «Авто».
	Launch *agents.Launch
	// Cwd is an explicit launch folder: it must exist and lie inside the project root or one of CwdRoots.
	Cwd      string
	CwdRoots []string
	Env      []EnvEntry
	// ResumeSessionID reopens a saved session instead (no initial prompt then).
	ResumeSessionID string
	// PromptFiles are instruction files under DataDir (Claude only).
	PromptFiles []string
	// Meta goes into the tab's metadata next to the launch (the conversation's mode).
	Meta *jsvalue.Object
	// TabName names the tab; nil or blank names it after the agent.
	TabName *string
}

// Result is what a start answered: the conversation, or the status and body of a refusal.
type Result struct {
	OK           bool
	Status       int
	Body         *jsvalue.Object
	Conversation *jsvalue.Object
}

// JSON is the result as the platform's routes exchange it: {ok, conversation} or {ok, status, body}.
func (r Result) JSON() *jsvalue.Object {
	if r.OK {
		return jsvalue.NewObject("ok", true, "conversation", r.Conversation)
	}
	return jsvalue.NewObject("ok", false, "status", float64(r.Status), "body", r.Body)
}

// ConversationID is the started conversation's id, "" for a refusal.
func (r Result) ConversationID() string {
	if !r.OK {
		return ""
	}
	return r.Conversation.String("id")
}

func fail(status int, msg string) Result {
	return Result{Status: status, Body: jsvalue.NewObject("error", msg)}
}

// UnavailableBody is the 409 body of an agent CLI that cannot run.
func UnavailableBody(u *agents.Unavailable) *jsvalue.Object {
	return jsvalue.NewObject("error", u.Code, "providerId", u.ProviderID, "providerDisplayName", u.ProviderDisplayName,
		"panelType", u.PanelType, "suggestedCommand", u.SuggestedCommand)
}

func isInside(child, root string) bool {
	if child == root {
		return true
	}
	if !strings.HasSuffix(root, "/") {
		root += "/"
	}
	return strings.HasPrefix(child, root)
}

func realpath(p string) string {
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		return ""
	}
	r, _ = filepath.Abs(r)
	return r
}

// ResolveCwd checks an explicit launch folder: absolute, existing, a folder, and inside workspaceRoot or one of
// extraRoots once symlinks are resolved. The error is the message for the caller.
func ResolveCwd(requested, workspaceRoot string, extraRoots []string) (string, error) {
	if workspaceRoot == "" && len(extraRoots) == 0 {
		return "", errors.New(msgNoWorkspaceRoot.String())
	}
	if !filepath.IsAbs(requested) {
		return "", errors.New(msgCwdNotAbsolute.String())
	}
	var roots []string
	for _, r := range append([]string{workspaceRoot}, extraRoots...) {
		if r == "" {
			continue
		}
		if real := realpath(r); real != "" {
			roots = append(roots, real)
		}
	}
	if len(roots) == 0 {
		return "", errors.New(msgRootUnavailable.String())
	}
	resolved := realpath(requested)
	inside := false
	for _, r := range roots {
		inside = inside || resolved != "" && isInside(resolved, r)
	}
	if !inside {
		return "", errors.New(msgCwdOutsideRoot.String())
	}
	if st, err := os.Stat(resolved); err != nil || !st.IsDir() {
		return "", errors.New(msgCwdNotFound.String())
	}
	return resolved, nil
}

func (s *Starter) keeper() Keeper {
	if s.cfg.Keeper == nil {
		return nil
	}
	return s.cfg.Keeper()
}

func firstDir(ws *jsvalue.Object) string {
	if dirs := ws.Array("directories"); len(dirs) > 0 {
		d, _ := dirs[0].(string)
		return d
	}
	return ""
}

// workspaceFor is the requested workspace, else the first one, else a new one at the project root.
func (s *Starter) workspaceFor(id string) (*jsvalue.Object, *Result, error) {
	if id != "" {
		ws := s.cfg.Store.Workspace(id)
		if ws == nil {
			r := fail(404, msgNoWorkspace.String())
			return nil, &r, nil
		}
		return ws, nil, nil
	}
	for _, v := range s.cfg.Store.Workspaces().Array("workspaces") {
		if ws, ok := v.(*jsvalue.Object); ok {
			return ws, nil, nil
		}
	}
	dir := s.cfg.WorkspaceRoot
	if dir == "" {
		dir = s.cfg.Home
	}
	if dir == "" {
		dir = "/"
	}
	ws, err := s.cfg.Store.CreateWorkspace(dir, s.cfg.FirstWorkspaceName, "")
	return ws, nil, err
}

func envList(entries []EnvEntry) []agents.EnvVar {
	out := make([]agents.EnvVar, len(entries))
	for i, e := range entries {
		v, _ := e.Value.(string) // not a string: "" fails the value check, as the old runtime refused it
		out[i] = agents.EnvVar{Key: e.Key, Value: v}
	}
	return out
}

// Start starts a Claude or Codex conversation in a new tab of the workspace's first pane. Everything typed into the
// shell is checked before a tab or workspace is made: the environment, then the launch line, then the prompt. An error
// is a failure the old runtime answered with a bare 500.
func (s *Starter) Start(o Options) (Result, error) {
	if o.Provider != agents.Claude && o.Provider != agents.Codex {
		return fail(400, msgUnknownAgent.String()), nil
	}
	env, err := agents.FormatStartEnv(envList(o.Env), s.cfg.EnvPrefix)
	if err != nil {
		return fail(400, err.Error()), nil
	}
	files, err := agents.ResolvePromptFiles(o.Provider, o.PromptFiles, s.cfg.DataDir, s.cfg.DataDirLabel)
	if err != nil {
		return fail(400, err.Error()), nil
	}
	explicit := ""
	if o.Cwd != "" {
		if explicit, err = ResolveCwd(o.Cwd, s.cfg.WorkspaceRoot, o.CwdRoots); err != nil {
			return fail(400, err.Error()), nil
		}
	}
	panelType := agents.PanelOf(o.Provider)
	if s.cfg.Availability != nil {
		if u := s.cfg.Availability(panelType); u != nil {
			return Result{Status: 409, Body: UnavailableBody(u)}, nil
		}
	}
	ws, refusal, err := s.workspaceFor(o.WorkspaceID)
	if refusal != nil || err != nil {
		if refusal != nil {
			return *refusal, nil
		}
		return Result{}, err
	}
	wsID := ws.String("id")
	// Tabs kept from before report their old folder into the workspace's directories; a configured project root still
	// takes new conversations.
	cwd := explicit
	if cwd == "" {
		cwd = s.cfg.WorkspaceRoot
	}
	if cwd == "" {
		cwd = firstDir(ws)
	}
	layout, err := s.cfg.Store.GetLayout(wsID, cwd)
	if err != nil {
		return Result{}, err
	}
	panes := state.Panes(layout)
	if len(panes) == 0 {
		return fail(500, msgNoPane.String()), nil
	}
	launch := o.Launch
	if launch != nil && launch.Model == nil && launch.Effort == nil {
		launch = nil
	}
	var line string
	if o.ResumeSessionID != "" {
		line, err = s.cfg.Lines.Resume(o.Provider, o.ResumeSessionID, wsID, launch)
	} else {
		line, err = s.cfg.Lines.Launch(o.Provider, wsID, launch, files)
	}
	if err != nil {
		return Result{}, err
	}
	prompt := ""
	if o.Prompt != nil {
		prompt = *o.Prompt
	}
	promptFile, err := agents.WriteLongPrompt(s.cfg.PromptDir, agents.InitialPromptText(prompt))
	if err != nil {
		return Result{}, err
	}
	command := env + agents.WithInitialPrompt(o.Provider, line, prompt, promptFile)
	name := ""
	if o.TabName != nil {
		name = jscompat.Trim(*o.TabName)
	}
	if name == "" {
		name = agents.DisplayName(o.Provider)
	}
	tab, err := s.cfg.Store.AddTab(wsID, panes[0].String("id"), state.TabOptions{Name: name, Cwd: cwd, PanelType: panelType, Command: command})
	var se *state.Error
	if errors.As(err, &se) || err == nil && tab == nil {
		return fail(500, msgStartFailed.String()), nil
	}
	if err != nil {
		return Result{}, err
	}
	tabID, session := tab.String("id"), tab.String("sessionName")
	if k := s.keeper(); k != nil {
		agentSession := status.Null[string]()
		if o.ResumeSessionID != "" {
			agentSession = status.Some(o.ResumeSessionID)
		}
		pt, _ := tab.Get("panelType")
		ptOpt := status.Null[string]()
		if p, ok := pt.(string); ok {
			ptOpt = status.Some(p)
		}
		k.RegisterTab(tabID, status.Entry{CliState: status.Busy, WorkspaceID: wsID, TabName: tab.String("name"), TmuxSession: session,
			PanelType: ptOpt, AgentProviderID: status.Some(o.Provider), AgentSessionID: agentSession, LastEvent: status.Null[status.LastEvent]()})
		k.MarkAgentLaunch(tabID, false)
	}
	meta := jsvalue.NewObject()
	if o.Meta != nil {
		meta.Assign(o.Meta)
	}
	if launch != nil {
		meta.Set("launch", launchJSON(launch))
	}
	stored := jsvalue.NewObject()
	if meta.Len() > 0 {
		if stored, err = s.cfg.Store.UpdateTabMeta(session, meta); err != nil {
			return Result{}, err
		}
	}
	// The tab vanished between the two layout writes: the session runs, but without its mode it must not look ordinary.
	if stored == nil {
		return fail(500, msgMetaNotSaved.String()), nil
	}
	trimmed := jscompat.Trim(prompt)
	// The list names a tab by its last prompt until Claude titles it.
	if trimmed != "" {
		if err := s.cfg.Store.UpdateTabLastUserMessage(session, jscompat.Slice(trimmed, 200)); err != nil {
			return Result{}, err
		}
	}
	title, _ := tab.Get("name")
	if trimmed != "" {
		title = jscompat.Slice(trimmed, 120)
	}
	conv := jsvalue.NewObject("id", tabID, "title", title, "provider", o.Provider, "workspaceId", wsID)
	wsName, _ := ws.Get("name")
	conv.Set("workspaceName", wsName)
	conv.Set("updatedAt", s.cfg.Now())
	conv.Assign(stored)
	return Result{OK: true, Conversation: conv}, nil
}

func launchJSON(l *agents.Launch) *jsvalue.Object {
	o := jsvalue.NewObject("model", nil, "effort", nil)
	if l.Model != nil {
		o.Set("model", *l.Model)
	}
	if l.Effort != nil {
		o.Set("effort", *l.Effort)
	}
	return o
}

// The texts Start answers people with, picked by RUNTIME_LOCALE when the answer is built; the Russian ones are the
// previous runtime's texts, byte for byte.
var (
	msgNoWorkspaceRoot = locale.T("No projects root folder is set on this machine", "На этой машине не задан корневой каталог проектов")
	msgCwdNotAbsolute  = locale.T("The launch folder must be an absolute path", "Каталог запуска должен быть абсолютным путём")
	msgRootUnavailable = locale.T("The projects root folder is unavailable", "Корневой каталог проектов недоступен")
	msgCwdOutsideRoot  = locale.T("The launch folder is outside the projects root folder", "Каталог запуска вне корневого каталога проектов")
	msgCwdNotFound     = locale.T("Launch folder not found", "Каталог запуска не найден")
	msgNoWorkspace     = locale.T("Project not found", "Проект не найден")
	msgUnknownAgent    = locale.T("Unknown agent", "Неизвестный агент")
	msgNoPane          = locale.T("The project has no pane for a conversation", "В проекте нет панели для диалога")
	msgStartFailed     = locale.T("Could not start the session", "Не удалось запустить сессию")
	msgMetaNotSaved    = locale.T("The conversation started, but its settings were not saved", "Диалог запущен, но его параметры не сохранились")
)
