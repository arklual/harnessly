package layoutapi

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/launch"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/oomguard"
	"github.com/ly-solutions/harnessly/internal/sidechat"
	"github.com/ly-solutions/harnessly/internal/status"
)

// Launch is what the launch slice's routes use besides Deps: the starter and document inbox, the Codex launch lines,
// eval runs and side chats.
type Launch struct {
	Starter   *launch.Starter
	Inbox     *launch.Inbox
	Headlines *launch.Headlines
	// MachineAgent: the request is the machine agent's own (loopback, its session).
	MachineAgent func(*http.Request) bool
	// SessionCreated is a tmux session's start, epoch seconds (0 when unknown).
	SessionCreated func(session string) int64
	// CodexArgs are the arguments the Codex launcher gets for codex itself.
	CodexArgs func(workspaceID, resumeID string, launch *agents.Launch) ([]string, error)
	// CodexForkArgs are the arguments that start codex on a fork of a session; nil when the runtime cannot fork.
	CodexForkArgs func(workspaceID, sessionID string, launch *agents.Launch) ([]string, error)
	// EvalRuns holds eval runs (<runId>/request.json, state.json, stop); EvalRunner is the runner script, run by Node
	// detached ("" or missing: this release has none).
	EvalRuns, EvalRunner, Node string
	// SideChat* find a tab's Claude session and run the fork.
	SideChatRuntime string
	Sources         func() []status.AgentSource
	ProjectDir      func(cwd string) string
	Log             func(level, msg string, kv ...any)
}

// LaunchRoutes are the launch slice's routes: starting conversations, documents, evals, side chats and Codex lines.
func LaunchRoutes(d *Deps, l *Launch) (main, shadow Route) {
	if d.ShellReady == 0 {
		d.ShellReady = 500 * time.Millisecond
	}
	a := &api{d: d, l: l}
	return a.routesFor(a.launchTable())
}

func (a *api) launchTable() []entry {
	p := a.d.PlatformAPI
	if p == "" {
		p = "/api/platform"
	}
	var t []entry
	add := func(pattern string, shadow bool, h handler) {
		t = append(t, entry{segments: segs(pattern), shadow: shadow, handle: h})
	}
	add(p+"/conversations", true, a.conversations)
	add(p+"/automations/start", false, a.automationStart)
	add(p+"/docs/deliver", false, a.docsDeliver)
	add(p+"/docs/resolve-pane", false, a.docsResolvePane)
	add(p+"/eval/start", false, a.evalStart)
	add(p+"/side-chat", false, a.sideChat)
	add("/api/codex/launch-args", false, a.codexLaunchArgs)
	add("/api/codex/launch-command", false, a.codexLaunchCommand)
	return t
}

func optionalString(v any) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}

// start answers a refusal itself and gives the conversation; a start that fails is a 500 that says why.
func (a *api) start(c *call, o launch.Options) *jsvalue.Object {
	r, err := a.l.Starter.Start(o)
	if err != nil {
		agent.Log("error", "the conversation did not start", agent.F{K: "provider", V: o.Provider}, agent.F{K: "error", V: err.Error()})
		c.fail(500, "Could not start the agent: "+err.Error())
		return nil
	}
	if !r.OK {
		c.reply(r.Status, r.Body)
		return nil
	}
	return r.Conversation
}

// GET, POST <platform>/conversations
func (a *api) conversations(c *call) {
	if !a.d.Session(c.r) {
		c.fail(403, "Forbidden")
		return
	}
	switch c.r.Method {
	case "GET":
		c.reply(200, a.l.Starter.Conversations(a.l.Headlines, a.l.SessionCreated))
	case "POST":
		pv, _ := c.field("provider")
		provider, _ := pv.(string)
		if provider != agents.Claude && provider != agents.Codex {
			c.fail(400, msgUnknownAgent.String())
			return
		}
		ws, _ := c.field("workspaceId")
		wsID, isStr := ws.(string)
		if jsvalue.Truthy(ws) && !isStr {
			c.fail(400, msgBadWorkspace.String())
			return
		}
		model, _ := c.field("model")
		effort, _ := c.field("effort")
		choice, refusal := a.d.Agents.ValidateLaunch(provider, model, effort)
		if refusal != "" {
			c.fail(400, refusal)
			return
		}
		prompt, _ := c.field("prompt")
		// Folder, environment, instruction files and metadata are not taken here: their callers have routes of their own.
		if conv := a.start(c, launch.Options{Provider: provider, WorkspaceID: wsID, Prompt: optionalString(prompt), Launch: choice}); conv != nil {
			c.reply(201, conv)
		}
	default:
		c.header.Set("Allow", "GET, POST")
		c.end(405)
	}
}

// machineAgentPost: the machine agent's own POST, else the refusal is answered.
func (a *api) machineAgentPost(c *call) bool {
	if !a.l.MachineAgent(c.r) {
		c.fail(403, "Forbidden")
		return false
	}
	if c.r.Method != "POST" {
		c.header.Set("Allow", "POST")
		c.end(405)
		return false
	}
	return true
}

// POST <platform>/automations/start: the automation's Claude conversation, tagged as one.
func (a *api) automationStart(c *call) {
	if !a.machineAgentPost(c) {
		return
	}
	pv, _ := c.field("prompt")
	prompt, isStr := pv.(string)
	if !isStr || jscompat.Trim(prompt) == "" {
		c.fail(400, msgEmptyAutomation.String())
		return
	}
	model := any("sonnet")
	if m, ok := c.str("model"); ok {
		model = m
	}
	choice, refusal := a.d.Agents.ValidateLaunch(agents.Claude, model, nil)
	if refusal != "" {
		c.fail(400, refusal)
		return
	}
	cwd, _ := c.str("cwd")
	var env []launch.EnvEntry
	if v, _ := c.field("env"); jsvalue.Truthy(v) {
		switch e := v.(type) {
		case *jsvalue.Object:
			for _, k := range e.Keys() {
				val, _ := e.Get(k)
				env = append(env, launch.EnvEntry{Key: k, Value: val})
			}
		case []any:
			for i, val := range e {
				env = append(env, launch.EnvEntry{Key: jscompat.Number(float64(i)), Value: val})
			}
		}
	}
	ws, _ := c.str("workspaceId")
	conv := a.start(c, launch.Options{Provider: agents.Claude, WorkspaceID: ws, Prompt: &prompt, Launch: choice, Cwd: cwd, Env: env,
		Meta: jsvalue.NewObject("mode", "automation")})
	if conv != nil {
		c.reply(201, jsvalue.NewObject("conversationId", conv.String("id")))
	}
}

// POST <platform>/docs/deliver: a person's comment for the conversation its document is bound to.
func (a *api) docsDeliver(c *call) {
	if !a.machineAgentPost(c) {
		return
	}
	mv, _ := c.field("message")
	message, isStr := mv.(string)
	if !isStr || jscompat.Trim(message) == "" {
		c.fail(400, msgEmptyMessage.String())
		return
	}
	opt := func(k string) string { s, _ := c.str(k); return s }
	out, err := a.l.Inbox.Deliver(launch.Delivery{TabID: opt("tabId"), SessionName: opt("sessionName"), AgentSessionID: opt("agentSessionId"),
		Cwd: opt("cwd"), Message: message})
	must(err)
	c.reply(200, out)
}

// POST <platform>/docs/resolve-pane: which tab published a document, from the publishing shell's pane id.
func (a *api) docsResolvePane(c *call) {
	if !a.machineAgentPost(c) {
		return
	}
	pane, isStr := c.str("tmuxPane")
	if !isStr {
		c.fail(400, msgNeedTmuxPane.String())
		return
	}
	socket, _ := c.str("tmuxSocket")
	c.reply(200, a.l.Inbox.ResolvePane(pane, socket))
}

var evalRunID = regexp.MustCompile(`^[0-9a-f-]{36}$`)

func isEvalRequest(body any) (string, bool) {
	b, ok := body.(*jsvalue.Object)
	if !ok {
		return "", false
	}
	str := func(o *jsvalue.Object, k string) (string, bool) {
		if o == nil {
			return "", false
		}
		v, _ := o.Get(k)
		s, ok := v.(string)
		return s, ok
	}
	runID, ok := str(b, "runId")
	if !ok || !evalRunID.MatchString(runID) {
		return "", false
	}
	task, arm := b.Object("task"), b.Object("arm")
	_, hasPrompt := str(task, "prompt")
	_, hasBase := str(task, "base")
	_, hasModel := str(arm, "model")
	if task == nil || !hasPrompt || !hasBase || arm == nil || !hasModel {
		return "", false
	}
	checks, _ := task.Get("checks")
	harness, _ := arm.Get("harness")
	_, isArr := checks.([]any)
	return runID, isArr && (harness == "personal" || harness == "repo")
}

// POST <platform>/eval/start[?stop=<runId>]: writes the run request and starts the eval runner detached, so a runtime
// restart does not kill a run in flight; ?stop asks a run to stop.
func (a *api) evalStart(c *call) {
	if !a.machineAgentPost(c) {
		return
	}
	if stop, ok := c.qString("stop"); ok {
		if !evalRunID.MatchString(stop) {
			c.fail(400, msgBadRun.String())
			return
		}
		dir := filepath.Join(a.l.EvalRuns, stop)
		must(os.MkdirAll(dir, 0o777))
		must(os.WriteFile(filepath.Join(dir, "stop"), []byte(time.Now().UTC().Format("2006-01-02T15:04:05.000Z")), 0o666))
		_ = os.Chmod(filepath.Join(dir, "stop"), 0o600)
		c.reply(202, jsvalue.NewObject("ok", true))
		return
	}
	runID, ok := isEvalRequest(c.body)
	if !ok {
		c.fail(400, msgIncompleteRun.String())
		return
	}
	dir := filepath.Join(a.l.EvalRuns, runID)
	var previous any
	if raw, err := os.ReadFile(filepath.Join(dir, "state.json")); err == nil {
		previous, _ = jsvalue.Parse(raw)
	}
	// A run put off as skipped comes back with the same id: it starts afresh.
	if p, isObj := previous.(*jsvalue.Object); isObj && p.String("state") == "skipped" {
		_ = os.RemoveAll(dir)
	} else if jsvalue.Truthy(previous) {
		c.reply(200, jsvalue.NewObject("accepted", true, "already", true))
		return
	}
	if a.l.EvalRunner == "" {
		c.fail(501, msgNoRunner.String())
		return
	}
	if _, err := os.Stat(a.l.EvalRunner); err != nil {
		c.fail(501, msgNoRunner.String())
		return
	}
	must(os.MkdirAll(dir, 0o777))
	must(os.WriteFile(filepath.Join(dir, "request.json"), jsvalue.Stringify(c.body, ""), 0o666))
	node := a.l.Node
	if node == "" {
		node = "node"
	}
	program, args := oomguard.Unprotected(node, a.l.EvalRunner, runID)
	cmd := exec.Command(program, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		agent.Log("warn", "eval runner did not start", agent.F{K: "err", V: err.Error()})
	} else {
		_ = cmd.Process.Release()
	}
	c.reply(202, jsvalue.NewObject("accepted", true))
}

var sideChatRefusals = map[string]struct {
	status int
	text   locale.Text
}{
	"unknown":     {404, locale.T("Tab not found", "Вкладка не найдена")},
	"unsupported": {422, locale.T("Side chat is only available for Claude for now", "Side chat пока только для Claude")},
	"no-session":  {409, locale.T("The agent has not started a session yet: send it something and try again", "Агент ещё не начал сессию: напишите ему что-нибудь и попробуйте снова")},
}

var errNoAgents = errors.New("agent lines are not available")

// sideChatTab is the tab's Claude session and folder, from the tab's own agent, never from the caller.
func (a *api) sideChatTab(tabID string) (sessionID, cwd, refusal string) {
	var src *status.AgentSource
	for _, s := range a.l.Sources() {
		if s.TabID == tabID {
			src = &s
			break
		}
	}
	if src == nil {
		return "", "", "unknown"
	}
	if src.Provider != agents.Claude {
		return "", "", "unsupported"
	}
	cwd = a.d.Tmux.SessionCwd(src.TmuxSession)
	if cwd == "" || !agents.IsUUID(src.AgentSessionID) {
		return "", "", "no-session"
	}
	if st, err := os.Stat(filepath.Join(a.l.ProjectDir(cwd), src.AgentSessionID+".jsonl")); err != nil || !st.Mode().IsRegular() {
		return "", "", "no-session"
	}
	return src.AgentSessionID, cwd, ""
}

const sideChatHeartbeat = 15 * time.Second

// POST <platform>/side-chat {tabId, question, excerpts?, history?, model?}: a server-sent stream of deltas, then done or
// error, answered by a fork of the tab's session that is never saved. One question runs per tab; closing the stream
// stops it.
func (a *api) sideChat(c *call) {
	if !a.d.Session(c.r) {
		c.fail(403, "Forbidden")
		return
	}
	c.header.Set("Cache-Control", "no-store")
	if c.r.Method != "POST" {
		c.methodNotAllowed("POST")
		return
	}
	if !strings.HasPrefix(c.r.Header.Get("Content-Type"), "application/json") {
		c.fail(415, msgNeedJSON.String())
		return
	}
	req, msg := sidechat.ParseRequest(c.body)
	if msg != "" {
		c.fail(400, msg)
		return
	}
	sessionID, cwd, refusal := a.sideChatTab(req.TabID)
	if refusal != "" {
		r := sideChatRefusals[refusal]
		c.reply(r.status, jsvalue.NewObject("error", r.text.String(), "code", refusal))
		return
	}
	release := sidechat.Claim(req.TabID)
	if release == nil {
		c.reply(409, jsvalue.NewObject("error", msgSideChatBusy.String(), "code", "busy"))
		return
	}
	r := c.r
	c.stream = func(w http.ResponseWriter) {
		defer release()
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store, no-transform")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		flush := func() {
			if flusher != nil {
				flusher.Flush()
			}
		}
		flush()
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		events := make(chan string, 64)
		go func() {
			defer close(events)
			sidechat.Ask(ctx, sidechat.Run{SessionID: sessionID, Cwd: cwd, Prompt: sidechat.BuildPrompt(req), Model: req.Model,
				Runtime: sidechat.ReadRuntime(a.l.SideChatRuntime), BaseEnv: os.Environ(), ProjectDir: a.l.ProjectDir(cwd), Log: a.l.Log},
				func(e sidechat.Event) { events <- sidechat.Format(e) })
		}()
		tick := time.NewTicker(sideChatHeartbeat)
		defer tick.Stop()
		for {
			select {
			case frame, ok := <-events:
				if !ok {
					return
				}
				_, _ = w.Write([]byte(frame))
				flush()
			case <-tick.C:
				_, _ = w.Write([]byte(": ping\n\n"))
				flush()
			}
		}
	}
}

// launchShape is a model and effort read by shape only (the catalog check happened when the line was built).
func (c *call) launchShape() (*agents.Launch, bool) {
	model, _ := c.field("model")
	effort, _ := c.field("effort")
	return agents.ReadLaunchShape(model, effort)
}

func (c *call) trimmedField(k string) string {
	s, _ := c.str(k)
	return jscompat.Trim(s)
}

// POST /api/codex/launch-args: codex's own arguments, for the launcher script in the pane.
func (a *api) codexLaunchArgs(c *call) {
	if c.r.Method != "POST" {
		c.methodNotAllowed("POST")
		return
	}
	ws := c.trimmedField("workspaceId")
	if ws == "" {
		ws = a.d.Store.ActiveWorkspaceID()
	}
	resume := c.trimmedField("resumeSessionId")
	fork := c.trimmedField("forkSessionId")
	l, ok := c.launchShape()
	if !ok {
		c.fail(400, "Invalid model or effort")
		return
	}
	var args []string
	var err error
	switch {
	case fork != "" && a.l.CodexForkArgs == nil:
		err = errors.New("the runtime cannot fork a Codex session")
	case fork != "":
		args, err = a.l.CodexForkArgs(ws, fork, l)
	default:
		args, err = a.l.CodexArgs(ws, resume, l)
	}
	if err != nil {
		agent.Log("error", "codex launch args build failed", agent.F{K: "err", V: err.Error()})
		c.fail(500, "Failed to build Codex launch args")
		return
	}
	c.reply(200, jsvalue.NewObject("args", jsvalue.StringArray(args)))
}

// POST /api/codex/launch-command: the line that starts or resumes Codex in a tab.
func (a *api) codexLaunchCommand(c *call) {
	if c.r.Method != "POST" {
		c.methodNotAllowed("POST")
		return
	}
	ws := c.trimmedField("workspaceId")
	resume := c.trimmedField("resumeSessionId")
	if ws == "" {
		ws = a.d.Store.ActiveWorkspaceID()
	}
	// A restart repeats the conversation's stored choice, so only its shape is checked here.
	l, ok := c.launchShape()
	if !ok {
		c.fail(400, msgBadLaunchShape.String())
		return
	}
	if u := a.availability(agents.CodexPanel); u != nil {
		c.unavailable(u)
		return
	}
	var line string
	var err error
	if a.d.Agents == nil {
		err = errNoAgents
	} else if resume != "" {
		line, err = a.d.Agents.ResumeLine(agents.Codex, resume, ws, l)
	} else {
		line, err = a.d.Agents.LaunchLine(agents.Codex, ws, l)
	}
	if err != nil {
		agent.Log("error", "codex launch command build failed", agent.F{K: "err", V: err.Error()})
		c.fail(500, "Failed to build Codex launch command")
		return
	}
	c.reply(200, jsvalue.NewObject("command", line))
}
