package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/catalog"
	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/proc"
	"github.com/ly-solutions/harnessly/internal/prompts"
	"github.com/ly-solutions/harnessly/internal/state"
	"github.com/ly-solutions/harnessly/internal/status"
)

// The prompt queue and every route that types into an agent's pane (internal/prompts), wired to the core's keeper,
// stores and tmux. It lives and dies with the core.

// promptConfig is what the deployment says about prompt delivery. The editor hand-off must match the script running
// Claudes were started with, so every name comes from the environment.
type promptConfig struct {
	// QueueFile keeps the queue across restarts.
	QueueFile string
	// UploadsDir holds the pictures a message may carry.
	UploadsDir string
	Editor     prompts.EditorConfig
}

// promptEditorScript is the prompt editor script (RUNTIME_PROMPT_EDITOR); the earlier name-only setting
// (RUNTIME_PROMPT_EDITOR_NAME, under the data folder's bin/) is still read.
func promptEditorScript(dataDir string) string {
	if p := os.Getenv("RUNTIME_PROMPT_EDITOR"); p != "" {
		return p
	}
	if n := os.Getenv("RUNTIME_PROMPT_EDITOR_NAME"); n != "" {
		return filepath.Join(dataDir, "bin", n)
	}
	return ""
}

// promptHumanEditorVar is RUNTIME_PROMPT_HUMAN_EDITOR_VAR, or the earlier RUNTIME_HUMAN_EDITOR_VAR.
func promptHumanEditorVar() string {
	return env("RUNTIME_PROMPT_HUMAN_EDITOR_VAR", os.Getenv("RUNTIME_HUMAN_EDITOR_VAR"))
}

// promptConfigFromEnv reads the prompt settings: see the README's runtime table.
func promptConfigFromEnv(dataDir string) promptConfig {
	script := promptEditorScript(dataDir)
	var markers []string
	for _, p := range append([]string{script}, splitList(os.Getenv("RUNTIME_PROMPT_EDITOR_LEGACY"))...) {
		if p != "" {
			markers = append(markers, "VISUAL="+p)
		}
	}
	return promptConfig{
		QueueFile:  filepath.Join(dataDir, "prompt-queue.json"),
		UploadsDir: env("RUNTIME_UPLOADS_DIR", filepath.Join(dataDir, "uploads")),
		Editor: prompts.EditorConfig{
			Dir:            env("RUNTIME_PROMPT_HANDOFF_DIR", filepath.Join(dataDir, "prompt-handoff")),
			Script:         script,
			Markers:        markers,
			UnkeyedSocket:  env("RUNTIME_PROMPT_HANDOFF_UNKEYED_SOCKET", os.Getenv("RUNTIME_TMUX_LEGACY_SOCKET")),
			HumanEditorVar: promptHumanEditorVar(),
		},
	}
}

// commandCatalog is the slash-command catalog the routes serve; a reload of plugins or skills makes it stale.
var commandCatalog atomic.Pointer[catalog.Commands]

// promptWiring answers the prompt service's questions from the core's keeper. m is set right after the keeper is made
// (the keeper needs the service as its queue first).
type promptWiring struct {
	c         *core
	m         *status.Manager
	providers map[string]status.Provider
	resume    func(tabID string) string
}

func providerOfTab(info status.TabInfo) string {
	if info.Provider == agents.Codex || info.PanelType == agents.CodexPanel {
		return prompts.ProviderCodex
	}
	if p := agents.ProviderOfPanel(info.PanelType); p != "" {
		return p
	}
	return info.Provider
}

func (w *promptWiring) Tab(tabID string) (prompts.Tab, bool) {
	info, ok := w.m.Tab(tabID)
	if !ok {
		return prompts.Tab{}, false
	}
	return prompts.Tab{ID: tabID, Session: info.TmuxSession, Provider: providerOfTab(info), CliState: info.CliState,
		TranscriptPath: info.Transcript, Launching: info.Launching, LaunchFailed: info.LaunchFailed,
		HasAgentSession: info.AgentSessionID != ""}, true
}

// TabOfSession is the tab a session name names (any tab, as the previous runtime looked it up by name).
func (w *promptWiring) TabOfSession(session string) (prompts.Tab, bool) {
	_, _, tabID, ok := w.c.cfg.Tmux.ParseSessionName(session)
	if !ok {
		return prompts.Tab{}, false
	}
	return w.Tab(tabID)
}

// Running asks the tab's agent CLI (by its panel) whether it runs under the pane.
func (w *promptWiring) Running(tab prompts.Tab) bool {
	info, ok := w.m.Tab(tab.ID)
	if !ok {
		return false
	}
	p := w.providers[agents.ProviderOfPanel(info.PanelType)]
	pid := w.c.cfg.Tmux.PanePID(tab.Session)
	return p != nil && pid > 0 && p.Running(pid, proc.ChildPIDs(pid))
}

func (w *promptWiring) RecoverSession(tab prompts.Tab) bool { return w.m.RecoverSession(tab.ID) }
func (w *promptWiring) Resume(tab prompts.Tab) string       { return w.resume(tab.ID) }
func (w *promptWiring) GiveUpLaunch(tabID string)           { w.m.LaunchFailed(tabID) }

// Interrupted: the route sent Escape already; only the state settles.
func (w *promptWiring) Interrupted(_, session string)  { w.m.Interrupted(session) }
func (w *promptWiring) MenuOnScreen(tabID string) bool { return w.m.MarkNeedsInputFromScreen(tabID) }
func (w *promptWiring) RecheckStuck(tabID string)      { w.m.RecheckStuck(tabID) }
func (w *promptWiring) CommandsReloaded(provider string) {
	if c := commandCatalog.Load(); c != nil {
		c.Invalidate(provider)
	}
}

// LaunchChosen stores a confirmed model or effort as the conversation's launch, keeping the other half.
func (w *promptWiring) LaunchChosen(session string, applied prompts.Applied) {
	current := w.c.tabLaunch(session)
	value := func(v *string, cur *string) any {
		switch {
		case v != nil:
			return *v
		case cur != nil:
			return *cur
		}
		return nil
	}
	launch := jsvalue.NewObject()
	launch.Set("model", value(applied.Model, current.Model))
	launch.Set("effort", value(applied.Effort, current.Effort))
	patch := jsvalue.NewObject()
	patch.Set("launch", launch)
	if _, err := w.c.store.UpdateTabMeta(session, patch); err != nil {
		agent.Log("warn", "core: launch not saved", agent.F{K: "session", V: session}, agent.F{K: "err", V: err.Error()})
	}
}

func (w *promptWiring) Broadcast(msg any) { w.m.Broadcast(msg) }

// queueAdapter is the service as the keeper drives its queue.
type queueAdapter struct{ svc *prompts.Service }

func (q queueAdapter) Schedule(tabID string, delay time.Duration) { q.svc.Schedule(tabID, delay) }
func (q queueAdapter) Pending(tabID string) bool                  { return q.svc.PendingDelivery(tabID) }
func (q queueAdapter) Snapshot() any                              { return q.svc.QueueSync().Queues }
func (q queueAdapter) Tabs() []string {
	var out []string
	for id := range q.svc.Queue().All() {
		out = append(out, id)
	}
	return out
}

// newPrompts builds the service over the wiring; the caller sets wiring.m before the keeper starts.
func (c *core) newPrompts(w *promptWiring) *prompts.Service {
	cfg := c.cfg.Prompts
	return prompts.New(prompts.Config{
		Panes:     prompts.TmuxPanes{Tmux: c.cfg.Tmux, Term: c.cfg.Term},
		Tabs:      w,
		Agents:    w,
		Events:    w,
		Broadcast: w,
		QueueFile: cfg.QueueFile,
		Editor:    cfg.Editor,
		IsUploadImage: func(p string) bool {
			return isUploadImage(cfg.UploadsDir, p)
		},
		Log: coreLog,
	})
}

func coreLog(level, msg string, kv ...any) {
	fs := make([]agent.F, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		k, _ := kv[i].(string)
		fs = append(fs, agent.F{K: k, V: kv[i+1]})
	}
	agent.Log(level, "core: "+msg, fs...)
}

var uploadImageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}

// isUploadImage: a picture file under the uploads folder (not the folder itself), as the previous runtime checked
// before pasting a path into an agent's prompt.
func isUploadImage(dir, p string) bool {
	if strings.Contains(p, "\x00") {
		return false
	}
	abs := func(s string) string {
		if !filepath.IsAbs(s) {
			wd, _ := os.Getwd()
			s = filepath.Join(wd, s)
		}
		return filepath.Clean(s)
	}
	root, resolved := abs(dir), abs(p)
	return resolved != root && strings.HasPrefix(resolved, root+"/") && uploadImageExts[strings.ToLower(files.ExtName(resolved))]
}

// layoutTab is a tab of the layouts with its workspace.
type layoutTab struct {
	tab       *jsvalue.Object
	workspace status.Workspace
}

// findLayoutTab is the tab whose tmux session this is, in any workspace.
func (c *core) findLayoutTab(session string) (layoutTab, bool) {
	layouts := coreLayouts{store: c.store}
	for _, ws := range layouts.Workspaces() {
		layout := c.store.Layout(ws.ID)
		if layout == nil {
			continue
		}
		for _, t := range state.AllTabs(layout) {
			if t.String("sessionName") == session {
				return layoutTab{tab: t, workspace: ws}, true
			}
		}
	}
	return layoutTab{}, false
}

// tabLaunch is the launch (model, effort) the conversation of a session keeps in its tab's metadata.
func (c *core) tabLaunch(session string) agents.Launch {
	found, ok := c.findLayoutTab(session)
	if !ok {
		return agents.Launch{}
	}
	meta := found.tab.Object(c.cfg.MetaKey)
	if meta == nil {
		return agents.Launch{}
	}
	l := meta.Object("launch")
	if l == nil {
		return agents.Launch{}
	}
	str := func(k string) *string {
		v, _ := l.Get(k)
		if s, ok := v.(string); ok {
			return &s
		}
		return nil
	}
	return agents.Launch{Model: str("model"), Effort: str("effort")}
}
