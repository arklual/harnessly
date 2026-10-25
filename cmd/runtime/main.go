// Command runtime is the per-person runtime on a machine, being moved here a slice at a time. It holds the runtime's
// port: what a slice switched on for this machine covers it answers itself, the rest goes to the previous runtime on
// an inner port.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/agents/codex"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/integrations"
	"github.com/ly-solutions/harnessly/internal/limits"
	"github.com/ly-solutions/harnessly/internal/preflight"
	"github.com/ly-solutions/harnessly/internal/terminal"
	"github.com/ly-solutions/harnessly/internal/tmux"
	"github.com/ly-solutions/harnessly/internal/wire"
)

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func fail(err error) {
	agent.Log("error", "runtime failed to start", agent.F{K: "error", V: err.Error()})
	os.Exit(1)
}

// inheritedEnv is the environment a tmux client of a terminal gets: the person's own variables, a known terminal type.
func inheritedEnv() []string {
	keep := map[string]bool{"HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "PATH": true, "LANG": true, "TMPDIR": true,
		"TMUX_TMPDIR": true, "SSH_AUTH_SOCK": true, "DISPLAY": true, "WAYLAND_DISPLAY": true, "XDG_RUNTIME_DIR": true, "TZ": true}
	out := []string{"TERM=xterm-256color", "COLORTERM=truecolor"}
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if keep[k] || strings.HasPrefix(k, "LC_") {
			out = append(out, kv)
		}
	}
	return out
}

type terminalBridge struct {
	core *coreRef // the Enter clock and the layouts are the running core's
}

// coreRef is set once the core exists (it needs the terminal, which needs this bridge).
type coreRef struct{ c *core }

func (r *coreRef) running() *core {
	if r == nil || r.c == nil || r.c.manager() == nil {
		return nil
	}
	return r.c
}

func (b terminalBridge) ClaimPrompt(id, session, text string) bool {
	if c := b.core.running(); c != nil {
		if svc := c.promptService(); svc != nil {
			return svc.ClaimPrompt(id, session, text)
		}
	}
	// No core yet keeps nothing to compare with: the text is typed, as before there was a check.
	return true
}

func (b terminalBridge) PromptEnter(session string) {
	if c := b.core.running(); c != nil {
		if svc := c.promptService(); svc != nil {
			svc.PromptEnter(session)
		}
	}
}

func (b terminalBridge) ReconcileCwd(session string) {
	if c := b.core.running(); c != nil {
		_ = c.store.ReconcileTabCwd(session)
	}
}

func isUpgrade(r *http.Request) bool { return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") }

func pathIs(r *http.Request, p string) bool {
	return r.URL.Path == p || r.URL.Path == "/agents"+p
}

// runtimeGate is the assembled gate and what main must stop with it.
type runtimeGate struct {
	*gate.Gate
	terms *terminal.Server
	core  *coreRef
	tmux  *tmux.Tmux
	home  string
}

// configFileOf is the runtime's config (the pair's secret, the person's settings).
func configFileOf(cfg gate.Config) string { return env("AGENT_CONFIG", cfg.DataDir+"/config.json") }

// build assembles the gate with every slice's routes; the environment names the product's values.
func build(cfg gate.Config) (*runtimeGate, error) {
	configFile := configFileOf(cfg)
	secret, err := agent.Config{SecretFile: os.Getenv("AGENT_SECRET_FILE"), ConfigFile: configFile}.LoadSecret()
	if err != nil {
		return nil, err
	}
	cookieName := env("RUNTIME_SESSION_COOKIE", "session-token")
	cli := &gate.CLIToken{DataDir: cfg.DataDir, Headers: strings.Split(env("RUNTIME_CLI_TOKEN_HEADERS", "x-cli-token"), ",")}
	home, _ := os.UserHomeDir()
	nice, niceSet := os.LookupEnv("RUNTIME_AGENT_NICE")
	integ := integrations.FromEnv(os.Getenv)
	// Every pane learns where the runtime keeps its files (the harnessly command finds the runtime there); the services'
	// addresses and tokens come with them when boards or artifacts are configured.
	paneEnv := func(conversation string) []string {
		env := []string{"HARNESSLY_DATA_DIR=" + cfg.DataDir}
		if integ.Enabled() {
			env = append(env, integ.AgentEnv(conversation)...)
		}
		return env
	}
	tm := tmux.NewConfig(tmux.Config{
		Names: tmux.Names{
			Socket:       env("RUNTIME_TMUX_SOCKET", "harnessly"),
			LegacySocket: os.Getenv("RUNTIME_TMUX_LEGACY_SOCKET"),
			Prefix:       env("RUNTIME_SESSION_PREFIX", "hs-"),
			LegacyPrefix: os.Getenv("RUNTIME_LEGACY_SESSION_PREFIX"),
		},
		ConfigFile: tmuxConfigFile(cfg),
		PaneBinDir: paneBinDir(home),
		Nice:       tmux.ParseNiceness(nice, niceSet),
		Home:       home,
		PaneEnv:    paneEnv,
	})
	// Without a previous runtime to write the Codex launcher, this one does, and needs a folder and a variable for it:
	// the environment's, else the defaults, so Codex works with no configuration.
	scriptDirs, tokenEnv := splitList(os.Getenv("RUNTIME_SCRIPT_DATA_DIRS")), os.Getenv("RUNTIME_MCP_TOKEN_ENV")
	if cfg.Standalone() {
		scriptDirs, tokenEnv = scriptDataDirs(cfg.DataDir, home), mcpTokenEnv()
	}
	ref := &coreRef{}
	terms := terminal.New(terminal.Config{
		Tmux: tm, Bridge: terminalBridge{core: ref}, Home: home, Env: inheritedEnv(),
	})
	tmuxNames = tm
	hooks := hookConfigFromEnv(configFile)
	pusher := newSharedPusher(cfg.DataDir, hooks)
	ref.c = newCore(coreConfig{DataDir: cfg.DataDir, Home: home, Tmux: tm, Term: terms,
		MetaKey: os.Getenv("RUNTIME_TAB_META_KEY"), AgentHeader: os.Getenv("RUNTIME_AGENT_TOKEN_HEADER"),
		CodexbarBin: os.Getenv("RUNTIME_CODEXBAR_BIN"), CodexHome: limits.CodexHome(os.Getenv("CODEX_HOME"), home),
		Prompts: promptConfigFromEnv(cfg.DataDir),
		Hooks:   hooks,
		Push:    pusher,
		Agents: newCoreAgents(coreAgentsConfig{DataDir: cfg.DataDir, Home: home, ConfigFile: configFile,
			ClaudeDir: env("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude")), CodexDir: env("CODEX_HOME", filepath.Join(home, ".codex")),
			DefaultsKey: env("RUNTIME_LAUNCH_DEFAULTS_KEY", "launchDefaults"), Launcher: os.Getenv("RUNTIME_CLAUDE_LAUNCHER"),
			EditorScript: promptEditorScript(cfg.DataDir), HumanEditorVar: promptHumanEditorVar(),

			Port: listenPort(cfg.Listen), MCP: codex.MCP{Server: env("RUNTIME_MCP_SERVER", "harnessly"), TokenEnv: tokenEnv},
			MCPPath: env("RUNTIME_MCP_PATH", "/api/cli/preview/mcp"), ScriptDataDirs: scriptDirs,
			TokenHeaders: cli.Headers, SkillsPluginDir: skillsPluginDir(cfg.DataDir), PassServiceEnv: integ.Enabled(),
			CodexContainerSandbox: env("RUNTIME_CODEX_SANDBOX", "codex") == "container"}),
		Launch:     launchConfigFromEnv(home, cfg.DataDir),
		Timeline:   timelineConfigFromEnv(cfg.DataDir),
		Standalone: cfg.Standalone()})
	if cfg.Standalone() {
		ref.c.cfg.WorkspacePrompts = workspacePrompts()
		ref.c.cfg.Telemetry = mismatchTelemetry(configFile)
	}

	// A page session: the cookie the cluster gateway (or the machine agent) signs with the pair's secret.
	session := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(cookieName)
			if err != nil || agent.VerifySession(secret, c.Value, time.Now()) != nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
	// The old runtime's own calls to what the Go runtime holds.
	internal := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !cli.OK(r) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next(w, r)
		}
	}
	reply := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	var body struct {
		Session string `json:"session"`
		Cols    int    `json:"cols"`
		Rows    int    `json:"rows"`
	}
	decode := func(r *http.Request) (string, int, int) {
		b := body
		_ = json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4096)).Decode(&b)
		return b.Session, b.Cols, b.Rows
	}

	routes := []gate.Route{
		{
			Slice: "terminal",
			Match: func(r *http.Request) bool {
				return pathIs(r, "/api/terminal") && isUpgrade(r)
			},
			Handler: session(terms.Handle),
		},
		{
			Match: func(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/internal/terminal/") },
			Handler: internal(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/internal/terminal/size":
					if cols, rows, ok := terms.Size(r.URL.Query().Get("session")); ok {
						reply(w, 200, map[string]int{"cols": cols, "rows": rows})
						return
					}
					reply(w, 404, map[string]string{"error": "no terminal of this session"})
				case "/internal/terminal/pause":
					s, _, _ := decode(r)
					if cols, rows, ok := terms.Pause(s); ok {
						reply(w, 200, map[string]int{"cols": cols, "rows": rows})
						return
					}
					reply(w, 409, map[string]string{"error": "no terminal of this session, or it is paused"})
				case "/internal/terminal/resume":
					s, _, _ := decode(r)
					terms.Resume(s)
					reply(w, 200, map[string]bool{"ok": true})
				case "/internal/terminal/resize":
					s, cols, rows := decode(r)
					terms.Resize(s, cols, rows)
					reply(w, 200, map[string]bool{"ok": true})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}),
		},
	}

	f, err := gate.New(cfg, routes...)
	if err != nil {
		return nil, err
	}
	// With the launch slice on the core starts and resumes agents itself; off, the old runtime still does.
	ref.c.agents.launchOn = func() bool { return f.SliceOn("launch") }
	ref.c.sliceOn = f.SliceOn
	// Like the previous runtime's API, every request a Go route answers re-signs a session past half its age.
	f.RefreshSessions(gate.SessionRefresh{Secret: secret, Cookie: cookieName, MaxAge: sessionMaxAge,
		Skip: func(r *http.Request, p string) bool {
			// The old router answered a path it had no route for before asking for a session; v2 asks for one.
			return openOrToken(cliTokenFirst(cfg.DataDir, cli.Headers))(r, p) || !oldRouteMatches(r) && !wire.Matches(r)
		}})
	sessionOK := func(r *http.Request) bool {
		c, err := r.Cookie(cookieName)
		return err == nil && agent.VerifySession(secret, c.Value, time.Now()) == nil
	}
	// The old runtime's API lets in a page session or the runtime's cli-token, and refuses anyone else like this.
	apiRefusal := func(r *http.Request) (int, any, http.Header, bool) {
		if sessionOK(r) || cli.OK(r) {
			return 0, nil, nil, false
		}
		return http.StatusUnauthorized, map[string]string{"error": "Unauthorized"}, http.Header{"Content-Type": {"application/json"}}, true
	}
	d := deps{
		Home:          home,
		DataDir:       cfg.DataDir,
		ConfigFile:    configFile,
		Secret:        secret,
		WorkspaceRoot: env("RUNTIME_WORKSPACE_DIR", home),
		Tmux:          tm,
		TokenHeaders:  cli.Headers,
		CLI:           cli,
		SessionOK:     sessionOK,
		APIRefusal:    apiRefusal,
		MachineAgent:  machineAgent(secret, cookieName),
		Cookie:        cookieName,
		SignSession:   func() string { return agent.SignHS256(secret, "user", "", sessionMaxAge) },
		Tools:         preflight.New(home),
		Upstream:      cfg.Upstream,
		Standalone:    cfg.Standalone(),
		SliceOn:       f.SliceOn,
		RuntimePort:   runtimePort(cfg),
		CoreRunning:   func() bool { return ref.running() != nil },
		MachineKit:    &machineKitRef{},
		FirstWorkspace: func() error {
			c := ref.running()
			if c == nil {
				return errors.New("the core is not running")
			}
			_, err := c.store.CreateWorkspace(env("RUNTIME_WORKSPACE_DIR", home), env("RUNTIME_FIRST_WORKSPACE_NAME", "Workspace"), "")
			return err
		},
		Claims: func(r *http.Request) map[string]any {
			c, err := r.Cookie(cookieName)
			if err != nil {
				return nil
			}
			claims, _ := agent.VerifiedPayload(secret, c.Value, time.Now())
			return claims
		},
	}
	ref.c.v2d = &d
	f.Add(ref.c.routes(func(r *http.Request) bool { return sessionOK(r) || cli.OK(r) }, cli.OK)...)
	f.Add(ref.c.promptRoutes(sessionOK, apiRefusal)...)
	f.Add(ref.c.launchRoutes(f, d, d.MachineAgent)...)
	f.Add(ref.c.layoutRoutes(f, d)...)
	f.Add(ref.c.checkpointRoutes(f, d)...)
	f.Add(ref.c.hookRoutes(cliTokenFirst(cfg.DataDir, cli.Headers), apiRefusal)...)
	f.Add(ref.c.timelineSocketRoutes(sessionOK)...)
	f.Add(ref.c.timelineRoutes(f, d)...)
	f.Add(ref.c.transcriptRoutes(f, d)...)
	f.Add(ref.c.designRoutes(f, d, pusher)...)
	startDesign(f, d)
	// The core runs when its slice is on.
	ref.c.resumeAtStart.Store(f.SliceOn("core"))
	if f.SliceOn("core") {
		go ref.c.start()
	}
	f.Add(f.AsRoutes(machineSliceRoutes(d, cfg.Listen, cfg.Upstream, pusher, hooks)...)...)
	f.Add(voiceRoutes(d)...)
	for _, group := range routeGroups {
		f.Add(f.AsRoutes(group(d)...)...)
	}
	for _, group := range bodyRouteGroups {
		f.Add(f.AsBodyRoutes(group(d)...)...)
	}
	settingsStore := stateSettings(d) // the one writer of the settings files in this process
	f.Add(f.AsRoutes(stateRoutes(d, settingsStore, ref.c.agents.ValidateLaunch)...)...)
	f.Add(f.AsRoutes(ref.c.stateCoreRoutes(d)...)...)
	f.Add(unshadowed(f, append(automationRoutes(d, settingsStore), ref.c.browserTabRoutes(d)...))...)
	f.Add(integrationRoutes(d, integ)...)
	f.Add(docsRoutes(d, integ)...)
	f.Add(githubRoutes(d, githubFromEnv())...)
	f.Add(peerRoutes(d, ref.c)...)
	f.Add(v2Routes(d, ref.c)...)
	if w := webConfigFromEnv(cfg.Standalone()); w != nil {
		f.Add(gate.Route{Match: w.matches, Handler: w.handler()})
	}
	if cfg.Standalone() {
		f.SetFallback(standaloneFallback(func(r *http.Request) bool { return sessionOK(r) || cli.OK(r) }))
	} else {
		// The old runtime behind the gate does not get an install socket, or a socket of anyone without a session.
		f.Add(upstreamGuard(func(r *http.Request) bool { return sessionOK(r) || cli.OK(r) }, previewOf(d).proxy.Matches))
	}
	return &runtimeGate{Gate: f, terms: terms, core: ref, tmux: tm, home: home}, nil
}

// defaultTmuxConf is the tmux.conf a standalone runtime writes when none is named: the page's terminals have tabs
// of their own, so tmux's status bar (with the host name and clock) stays off.
const defaultTmuxConf = `# Written by the runtime because RUNTIME_TMUX_CONF names no file. It is never overwritten: edit it freely.
set -g status off
`

// tmuxConfigFile is RUNTIME_TMUX_CONF; set but empty, tmux's defaults. Unset on a standalone runtime, it is tmux.conf in
// the data directory, written with defaultTmuxConf when missing; in front of the previous runtime, none.
func tmuxConfigFile(cfg gate.Config) string {
	if f, ok := os.LookupEnv("RUNTIME_TMUX_CONF"); ok || !cfg.Standalone() || cfg.DataDir == "" {
		return f
	}
	f := filepath.Join(cfg.DataDir, "tmux.conf")
	if _, err := os.Stat(f); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(f, []byte(defaultTmuxConf), 0o644); err != nil {
			agent.Log("warn", "standalone: default tmux.conf not written; tmux runs with its defaults", agent.F{K: "err", V: err.Error()})
			return ""
		}
	}
	return f
}

// runtimePort is the port the previous runtime reported as its own: its inner one in front of it, the gate's alone.
func runtimePort(cfg gate.Config) int {
	if cfg.Standalone() {
		return portOf(cfg.Listen)
	}
	return portOf(cfg.Upstream)
}

func main() {
	if ran, err := subcommand(os.Args[1:]); ran {
		if err != nil {
			fail(err)
		}
		return
	}
	cfg, err := gate.LoadConfig()
	if err != nil {
		fail(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := run(ctx, cfg); err != nil {
		fail(err)
	}
}

// run serves the gate until ctx ends. Standalone, it first does what the old runtime did on start (the lock, the config
// and the agents' files) and undoes it on the way out.
func run(ctx context.Context, cfg gate.Config) error {
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	var alone *standalone
	standaloneSlots = nil
	if cfg.Standalone() {
		standaloneSlots = listenPreviewSlots(cfg.Listen, portOf(cfg.Listen))
		if portOf(cfg.Listen) == 0 {
			cfg.Listen = ln.Addr().String()
		}
		if alone, err = standaloneStart(cfg, configFileOf(cfg), port); err != nil {
			_ = ln.Close()
			standaloneSlots.close()
			return err
		}
		defer alone.stop()
		alone.files(port, nil)
	}
	f, err := build(cfg)
	if err != nil {
		_ = ln.Close()
		standaloneSlots.close()
		return err
	}
	if alone != nil {
		f.tmux.ApplyConfig("")
		go alone.cleanUp(f.home, env("RUNTIME_UPLOADS_DIR", filepath.Join(cfg.DataDir, "uploads")))
	}
	terms := f.terms
	var handler http.Handler = f
	if cfg.Standalone() {
		handler = standaloneSlots.guard(apiAlias(appBase(), f))
		standaloneSlots.serve(ctx)
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		t := time.NewTicker(cfg.CountEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				f.LogCounts()
			}
		}
	}()
	agent.Log("info", "runtime gate listening", agent.F{K: "listen", V: ln.Addr().String()}, agent.F{K: "upstream", V: cfg.Upstream},
		agent.F{K: "standalone", V: cfg.Standalone()}, agent.F{K: "version", V: cfg.Version})
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		agent.Log("info", "shutting down")
		f.LogCounts()
		terms.Shutdown()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
		f.core.c.stop()
	}
	return nil
}
