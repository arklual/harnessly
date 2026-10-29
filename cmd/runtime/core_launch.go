package main

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/claude"
	"github.com/ly-solutions/harnessly/internal/agents/codex"
	"github.com/ly-solutions/harnessly/internal/coreapi/layoutapi"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/launch"
	"github.com/ly-solutions/harnessly/internal/proc"
	"github.com/ly-solutions/harnessly/internal/sessions"
	"github.com/ly-solutions/harnessly/internal/sleep"
	"github.com/ly-solutions/harnessly/internal/status"
)

// Starting and resuming agents (slice "launch"): conversations, automations, documents, evals, side chats, the Codex
// lines and the startup resume. The routes answer only while the core runs; with the slice off, the core's agent start
// and resume still go to the old runtime.

// launchConfig is what the deployment says about launching; see the README's runtime table.
type launchConfig struct {
	// WorkspaceRoot is the project root as the deployment sets it ("" when it names none).
	WorkspaceRoot string
	// EnvPrefix starts the variables a launch may set; DataDirLabel names the data folder in messages.
	EnvPrefix, DataDirLabel string
	FirstWorkspaceName      string
	// EvalRunner is the eval runner script and Node what runs it.
	EvalRunner, Node string
	// SideChatRuntime is the machine's runtime config naming the Claude binary and login a side chat uses.
	SideChatRuntime string
}

func launchConfigFromEnv(home, dataDir string) launchConfig {
	label := os.Getenv("RUNTIME_DATA_DIR_LABEL")
	if label == "" {
		label = dataDir
		if rel, err := filepath.Rel(home, dataDir); err == nil && home != "" && !strings.HasPrefix(rel, "..") {
			label = "~/" + rel
		}
	}
	return launchConfig{
		WorkspaceRoot:      os.Getenv("RUNTIME_WORKSPACE_DIR"),
		EnvPrefix:          env("RUNTIME_START_ENV_PREFIX", "HARNESS_"),
		DataDirLabel:       label,
		FirstWorkspaceName: env("RUNTIME_FIRST_WORKSPACE_NAME", "Workspace"),
		EvalRunner:         os.Getenv("RUNTIME_EVAL_RUNNER"),
		Node:               env("RUNTIME_NODE_BIN", "node"),
		SideChatRuntime:    os.Getenv("RUNTIME_SIDE_CHAT_CONFIG"),
	}
}

// listenPort is the port of a host:port address, 0 when there is none.
func listenPort(addr string) int {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	n := 0
	for _, r := range p {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// keeperOf is the running keeper as the starter sees it (a nil interface while the core is off).
func (c *core) keeperOf() launch.Keeper {
	if m := c.manager(); m != nil {
		return m
	}
	return nil
}

// envSource is the env files' prefix of a provider's launch ("" without a machine agent's files).
func (c *core) envSource(provider string) string {
	if c.agents == nil {
		return ""
	}
	return c.agents.envSource(provider)
}

// paneShell is the login shell the panes run, which launch lines are typed into ("" without tmux).
func (c *core) paneShell() string {
	if c.cfg.Tmux == nil {
		return ""
	}
	return c.cfg.Tmux.Shell()
}

// newLauncher makes the core's starter and document inbox, and hands the agents their Go start and resume.
func (c *core) newLauncher() {
	lc := c.cfg.Launch
	var lines launch.Lines
	var avail func(string) *agents.Unavailable
	if c.agents != nil {
		lines = launchLines{c.agents}
		avail = c.agents.Availability
	}
	c.launcher = launch.New(launch.Config{
		Store: c.store, Lines: lines, Availability: avail, Keeper: c.keeperOf,
		WorkspaceRoot: lc.WorkspaceRoot, Home: c.cfg.Home, DataDir: c.cfg.DataDir, DataDirLabel: lc.DataDirLabel,
		EnvPrefix: lc.EnvPrefix, EnvSource: c.envSource, PromptDir: filepath.Join(c.cfg.DataDir, "launch-prompts"), FirstWorkspaceName: lc.FirstWorkspaceName,
		Shell: c.paneShell(),
	})
	c.inbox = &launch.Inbox{Starter: c.launcher, Tmux: inboxTmux{c}, Log: coreLog,
		Submit: func(session, text string) error {
			svc := c.promptService()
			if svc == nil {
				return errCoreOff
			}
			return svc.SubmitText(session, text)
		}}
	if c.agents != nil {
		c.agents.starter = c.launcher
		c.agents.resume = func() int { return c.launcher.AutoResume(c.resumeConfig()) }
	}
}

type launchLines struct{ a *coreAgents }

func (l launchLines) Launch(provider, ws string, launch *agents.Launch, files []string) (string, error) {
	return lines(l).Launch(provider, ws, launch, files)
}
func (l launchLines) Resume(provider, sid, ws string, launch *agents.Launch) (string, error) {
	return l.a.resumeLine(provider, sid, ws, launch)
}
func (l launchLines) Fork(provider, sid, ws string, launch *agents.Launch) (string, error) {
	return l.a.ForkLine(provider, sid, ws, launch)
}

type inboxTmux struct{ c *core }

func (t inboxTmux) HasSession(name string) bool { return t.c.cfg.Tmux.HasSession(name) }
func (t inboxTmux) SessionsOfPane(paneID, socket string) []string {
	return t.c.cfg.Tmux.SessionsOfPane(paneID, socket)
}
func (t inboxTmux) SessionCwd(name string) string {
	return layoutapi.FromTmux(t.c.cfg.Tmux).SessionCwd(name)
}

var errCoreOff = &coreOffError{}

type coreOffError struct{}

func (*coreOffError) Error() string { return "the Go core is off" }

// resumeConfig is what the startup resume reads: the preflight, the transcripts' last activity and the sleep rules.
func (c *core) resumeConfig() launch.ResumeConfig {
	installed := func(provider string) (bool, bool) {
		if c.agents == nil {
			return true, false
		}
		r := c.agents.runtime()
		if provider == agents.Codex {
			return r.Codex.Installed, false
		}
		return r.Claude.Installed, r.Claude.BinaryPath != nil
	}
	return newResumeConfig(c.cfg.Home, c.cfg.Tmux, installed, sleep.ReadConfig(os.Getenv, c.cfg.Hooks.Sleep), time.Now)
}

func newResumeConfig(home string, tm launch.ResumeTmux, installed func(string) (bool, bool), rules sleep.Config, now func() time.Time) launch.ResumeConfig {
	activity := &claude.ActivityReader{}
	projects := claude.NewDetector(home).ProjectsDir()
	codexRoot := codex.NewDetector(home).SessionsRoot()
	return launch.ResumeConfig{
		Tmux:      tm,
		Installed: installed,
		LastActivity: func(provider, sid string) *float64 {
			if provider == agents.Codex {
				m, ok := codex.FindSessionByID(codexRoot, sid, now(), codex.ScanDays)
				if !ok {
					return nil
				}
				st, err := os.Stat(m.JSONLPath)
				if err != nil {
					return nil
				}
				ms := float64(st.ModTime().UnixNano()) / 1e6
				return &ms
			}
			path := claude.FindTranscript(projects, sid)
			if path == "" {
				return nil
			}
			return activity.At(path)
		},
		Sleep:     rules,
		ChildPIDs: proc.ChildPIDs,
		Cmdline:   proc.Cmdline,
		Log:       coreLog,
	}
}

// machineAgent: the machine agent's own request, from loopback with a session it signed with its name.
func machineAgent(secret []byte, cookie string) func(*http.Request) bool {
	return func(r *http.Request) bool {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || host != "127.0.0.1" && host != "::1" && host != "::ffff:127.0.0.1" {
			return false
		}
		ck, err := r.Cookie(cookie)
		if err != nil {
			return false
		}
		sub, ok := agent.SessionSubject(secret, ck.Value, time.Now())
		return ok && sub == "machine-agent"
	}
}

// launchRoutes are the launch slice's routes (the conversation list also under "launch-shadow"), answered only while
// the core runs.
func (c *core) launchRoutes(f *gate.Gate, d deps, isMachineAgent func(*http.Request) bool) []gate.Route {
	lc := c.cfg.Launch
	deps := &layoutapi.Deps{
		Store: c.store, Tmux: layoutapi.FromTmux(c.cfg.Tmux), Status: coreStatusHook{c}, Agents: c.agents, Sync: coreSyncHook{c},
		Session: d.SessionOK, CLIToken: cliTokenFirst(d.DataDir, d.TokenHeaders), PlatformAPI: platformAPI, Home: d.Home,
	}
	if c.agents == nil {
		deps.Agents = nil
	}
	l := &layoutapi.Launch{
		Starter: c.launcher, Inbox: c.inbox, Headlines: &launch.Headlines{}, MachineAgent: isMachineAgent,
		SessionCreated: func(s string) int64 { return c.cfg.Tmux.PaneDetail(s).SessionCreated },
		CodexArgs: func(ws, resume string, l *agents.Launch) ([]string, error) {
			if c.agents == nil {
				return nil, errCoreOff
			}
			return c.agents.CodexArgs(ws, resume, l)
		},
		CodexForkArgs: func(ws, session string, l *agents.Launch) ([]string, error) {
			if c.agents == nil {
				return nil, errCoreOff
			}
			return c.agents.CodexForkArgs(ws, session, l)
		},
		EvalRuns: filepath.Join(c.cfg.DataDir, "eval", "runs"), EvalRunner: lc.EvalRunner, Node: lc.Node,
		SideChatRuntime: lc.SideChatRuntime,
		Sources: func() []status.AgentSource {
			if m := c.manager(); m != nil {
				return m.AgentSources()
			}
			return nil
		},
		ProjectDir: func(cwd string) string {
			return filepath.Join(c.cfg.Home, ".claude", "projects", sessions.ProjectName(cwd))
		},
		Log: coreLog,
	}
	main, shadow := layoutapi.LaunchRoutes(deps, l)
	running := func(m func(*http.Request) bool) func(*http.Request) bool {
		return func(r *http.Request) bool { return c.manager() != nil && m(r) }
	}
	routes := []gate.Route{{Slice: "launch", Match: running(main.Match), Handler: main.Handler}}
	return append(routes, f.AsBodyRoutes(gate.BodyRoute{Slice: "launch", Match: running(shadow.Match), Serve: shadow.Handler.ServeHTTP})...)
}
