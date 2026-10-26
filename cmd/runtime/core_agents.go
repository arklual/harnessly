package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/claude"
	"github.com/ly-solutions/harnessly/internal/agents/codex"
	"github.com/ly-solutions/harnessly/internal/catalog"
	"github.com/ly-solutions/harnessly/internal/coreapi/layoutapi"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/launch"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/preflight"
	"github.com/ly-solutions/harnessly/internal/providers"
)

// coreAgents is what the core's routes need of the agent CLIs: whether they can run, the lines that start and resume
// them, and starting a conversation the platform's way: here with the launch slice on, else by the old runtime.
type coreAgents struct {
	checker *preflight.Checker
	options catalog.LaunchConfig
	claude  claude.LaunchConfig
	// codexLauncher is the Codex launcher script; the old runtime writes it, the line only names it.
	codexLauncher string
	// launcherScript writes the launcher when it is missing; nil leaves it to the old runtime.
	launcherScript *codex.LauncherScriptConfig
	codexArgs      codex.LaunchConfig
	// profiles keeps the keys of the backend profiles and gives a launch on one what it needs (providers).
	profiles *providers.Store
	// launchOn: the launch slice is on, so conversations start and resume here (set by main); starter and resume are
	// the core's, set once it exists.
	launchOn func() bool
	starter  *launch.Starter
	resume   func() int
	// preflight answers which CLIs run; the checker's by default.
	preflight func() preflight.Result
}

type coreAgentsConfig struct {
	DataDir, Home, ConfigFile string
	ClaudeDir, CodexDir       string
	DefaultsKey               string
	// Launcher is the managed Claude launcher ("" runs `claude`).
	Launcher string
	// EditorScript is the prompt editor script Claude gets as its editor (the prompt service's RUNTIME_PROMPT_EDITOR),
	// HumanEditorVar the variable it finds the person's editor in.
	EditorScript, HumanEditorVar string
	// Port is the runtime's port (the gate's), for the MCP address Codex is given.
	Port int
	// MCP names the runtime's MCP server for Codex; ScriptDataDirs are the home-relative data folders the Codex launcher
	// looks for (the launcher is written here only with both).
	MCP            codex.MCP
	MCPPath        string
	ScriptDataDirs []string
	TokenHeaders   []string
	// SkillsPluginDir is the Claude plugin folder holding the workspace's skills ("" when they are off): Claude loads it
	// once it exists. PassServiceEnv is set when boards or artifacts are configured: Codex then keeps their tokens in the
	// environment of its commands.
	SkillsPluginDir string
	PassServiceEnv  bool
	// CodexContainerSandbox: RUNTIME_CODEX_SANDBOX=container, Codex runs without its own sandbox (the container is one).
	CodexContainerSandbox bool
}

// skipPermissions: the runtime config's dangerouslySkipPermissions, read at every launch as the old runtime did.
func skipPermissions(configFile string) func() bool {
	return func() bool {
		raw, err := os.ReadFile(configFile)
		if err != nil {
			return false
		}
		v, err := jsvalue.Parse(raw)
		o, _ := v.(*jsvalue.Object)
		if err != nil || o == nil {
			return false
		}
		skip, _ := o.Get("dangerouslySkipPermissions")
		return jsvalue.Truthy(skip)
	}
}

func newCoreAgents(c coreAgentsConfig) *coreAgents {
	profiles := &providers.Store{Dir: filepath.Join(c.DataDir, "providers"), Logins: profileLogins(c.Home)}
	launcher := providers.Launcher{Store: profiles, Relay: codexRelay(c)}
	editor := func() (string, error) {
		script := c.EditorScript
		if script == "" {
			return "", errors.New("no prompt editor configured")
		}
		if _, err := os.Stat(script); err != nil {
			return "", err
		}
		return script, nil
	}
	return &coreAgents{
		profiles: profiles,
		checker:  preflight.New(c.Home),
		options:  catalog.LaunchConfig{ClaudeDir: c.ClaudeDir, CodexDir: c.CodexDir, RuntimeConfig: c.ConfigFile, DefaultsKey: c.DefaultsKey},
		claude: claude.LaunchConfig{
			HooksPath:       filepath.Join(c.DataDir, "hooks.json"),
			MCPConfigPath:   filepath.Join(c.DataDir, "preview-mcp.json"),
			WorkspacePrompt: func(ws string) string { return filepath.Join(c.DataDir, "workspaces", ws, "claude-prompt.md") },
			PluginDir:       pluginDirOnce(c.SkillsPluginDir),
			JoinDir:         filepath.Join(c.DataDir, "launch-prompts"),
			Launcher:        c.Launcher,
			SkipPermissions: skipPermissions(c.ConfigFile),
			EditorScript:    editor,
			HumanEditorVar:  c.HumanEditorVar,
			Profiles:        launcher,
		},
		codexLauncher:  filepath.Join(c.DataDir, "codex-launcher.js"),
		launcherScript: launcherScriptConfig(c),
		codexArgs: codex.LaunchConfig{
			Hooks:            &codex.HookConfig{Script: filepath.Join(c.DataDir, "codex-hook.sh"), UserConfig: filepath.Join(c.Home, ".codex", "config.toml")},
			MCP:              codexMCP(c),
			WorkspacePrompt:  func(ws string) string { return filepath.Join(c.DataDir, "workspaces", ws, "codex-prompt.md") },
			SkipPermissions:  skipPermissions(c.ConfigFile),
			PassServiceEnv:   c.PassServiceEnv,
			ContainerSandbox: c.CodexContainerSandbox,
			ShellEnvExclude:  []string{c.MCP.TokenEnv},
			Profiles:         launcher,
		},
	}
}

// pluginDirOnce is the skills plugin's folder while it exists (it is written after the runtime starts, and by then the
// first launch may already be asking); "" for no plugin.
func pluginDirOnce(dir string) func() string {
	if dir == "" {
		return nil
	}
	return func() string {
		if _, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json")); err != nil {
			return ""
		}
		return dir
	}
}

func codexMCP(c coreAgentsConfig) codex.MCP {
	m := c.MCP
	m.URL = codex.MCPURL(c.Port, c.MCPPath)
	if m.TokenHeader == "" && len(c.TokenHeaders) > 0 {
		m.TokenHeader = c.TokenHeaders[0]
	}
	return m
}

// profileLogins are the vendor CLIs' sign-ins of the person a profile can run on: the Grok CLI's for xai.
func profileLogins(home string) map[string]providers.Login {
	if home == "" {
		return nil
	}
	return map[string]providers.Login{"xai": &providers.GrokLogin{Path: filepath.Join(providers.GrokHome(home), "auth.json")}}
}

// codexRelay is the runtime's relay as Codex is told about it: on the runtime's port, behind the CLI token Codex
// already has for the MCP server. nil (straight to the vendor with the key) while the port or the token is unknown.
func codexRelay(c coreAgentsConfig) *providers.RelayEndpoint {
	base := codex.MCPURL(c.Port, providers.RelayPrefix)
	if base == "" || c.MCP.TokenEnv == "" || len(c.TokenHeaders) == 0 {
		return nil
	}
	return &providers.RelayEndpoint{Base: base, Header: c.TokenHeaders[0], TokenEnv: c.MCP.TokenEnv}
}

func launcherScriptConfig(c coreAgentsConfig) *codex.LauncherScriptConfig {
	if len(c.ScriptDataDirs) == 0 || c.MCP.TokenEnv == "" {
		return nil
	}
	return &codex.LauncherScriptConfig{DataDirs: c.ScriptDataDirs, PortFile: "port", TokenFile: "cli-token",
		LaunchArgsPath: "/api/codex/launch-args", TokenHeaders: c.TokenHeaders, TokenEnv: c.MCP.TokenEnv}
}

// CodexArgs are codex's own arguments, for the launcher script in a pane.
func (a *coreAgents) CodexArgs(workspaceID, resumeID string, launch *agents.Launch) ([]string, error) {
	args, _, err := a.codexArgs.RuntimeArgs(workspaceID, resumeID, launch)
	return args, err
}

// CodexForkArgs are the arguments of codex on a fork of the session.
func (a *coreAgents) CodexForkArgs(workspaceID, sessionID string, launch *agents.Launch) ([]string, error) {
	args, _, err := a.codexArgs.ForkArgs(workspaceID, sessionID, launch)
	return args, err
}

// Availability is the previous runtime's agent-availability over the preflight's answer.
func (a *coreAgents) Availability(panelType string) *layoutapi.Unavailable {
	if agents.ProviderOfPanel(panelType) == "" {
		return nil
	}
	return availability(a.runtime(), panelType)
}

func (a *coreAgents) runtime() preflight.Result {
	if a.preflight != nil {
		return a.preflight()
	}
	return a.checker.Runtime()
}

func availability(r preflight.Result, panelType string) *layoutapi.Unavailable {
	switch agents.ProviderOfPanel(panelType) {
	case agents.Claude:
		if r.Claude.Installed {
			return nil
		}
		if r.Claude.BinaryPath != nil {
			return &layoutapi.Unavailable{Code: "agent-path-missing", ProviderID: agents.Claude, ProviderDisplayName: "Claude Code",
				PanelType: agents.ClaudePanel, SuggestedCommand: "claude-path"}
		}
		return &layoutapi.Unavailable{Code: "agent-not-installed", ProviderID: agents.Claude, ProviderDisplayName: "Claude Code",
			PanelType: agents.ClaudePanel, SuggestedCommand: "claude"}
	case agents.Codex:
		if r.Codex.Installed {
			return nil
		}
		return &layoutapi.Unavailable{Code: "agent-not-installed", ProviderID: agents.Codex, ProviderDisplayName: "Codex",
			PanelType: agents.CodexPanel}
	}
	return nil
}

var (
	claudeEfforts = []string{"low", "medium", "high", "xhigh", "max", "ultracode"}
	effortLabels  = map[string]string{"minimal": "Minimal", "low": "Low", "medium": "Medium", "high": "High",
		"xhigh": "Extra high", "max": "Max", "ultra": "Ultra", "ultracode": "Ultracode"}
	providerTitles = map[string]string{agents.Claude: "Claude", agents.Codex: "Codex"}
)

// effortNone is the label of "none", the one effort whose label is not the same in every language.
var effortNone = locale.T("No reasoning", "Без рассуждения")

func effortLabel(e string) string {
	if e == "none" {
		return effortNone.String()
	}
	if l, ok := effortLabels[e]; ok {
		return l
	}
	return e
}

func findModel(models []catalog.Model, id string) *catalog.Model {
	for i := range models {
		if models[i].ID == id {
			return &models[i]
		}
	}
	return nil
}

// effortsFor: the levels offered for a model; for "Auto", those of the CLI's default model, else Claude's own list.
func effortsFor(provider string, o catalog.LaunchOptions, model *string) []string {
	if model != nil {
		if m := findModel(o.Models, *model); m != nil {
			return m.Efforts
		}
		return nil
	}
	if o.CliDefault.Model != nil {
		if m := findModel(o.Models, *o.CliDefault.Model); m != nil {
			return m.Efforts
		}
	}
	if provider == agents.Claude {
		return claudeEfforts
	}
	return nil
}

// ValidateLaunch is validateLaunchChoice: the shape first, then this machine's catalog.
func (a *coreAgents) ValidateLaunch(provider string, model, effort any) (*agents.Launch, string) {
	if model == nil && effort == nil {
		return nil, ""
	}
	l := &agents.Launch{}
	if model != nil {
		s, ok := model.(string)
		if !ok || !agents.ModelIDRe.MatchString(s) {
			return nil, locale.Pick("Invalid model name", "Недопустимое имя модели")
		}
		l.Model = &s
	}
	if effort != nil {
		s, ok := effort.(string)
		known := ok && agents.IsKnownEffort(s)
		if provider == agents.Claude {
			known = ok && slices.Contains(claudeEfforts, s)
		}
		if !known {
			return nil, locale.Pick("Invalid reasoning effort", "Недопустимая глубина рассуждения")
		}
		l.Effort = &s
	}
	o := a.options.Launch(provider)
	if l.Model != nil && findModel(o.Models, *l.Model) == nil {
		return nil, locale.Pick(`Model "`+*l.Model+`" is not available for `+providerTitles[provider]+" on this machine",
			"Модель «"+*l.Model+"» недоступна для "+providerTitles[provider]+" на этой машине")
	}
	if l.Effort != nil && !slices.Contains(effortsFor(provider, o, l.Model), *l.Effort) {
		label := effortLabel(*l.Effort)
		var who *string
		if l.Model != nil {
			who = l.Model
		} else if o.CliDefault.Model != nil {
			who = o.CliDefault.Model
		}
		if who == nil {
			return nil, locale.Pick(`The default model does not support reasoning effort "`+label+`"`, "Модель по умолчанию не поддерживает глубину «"+label+"»")
		}
		return nil, locale.Pick("Model "+*who+` does not support reasoning effort "`+label+`"`, "Модель "+*who+" не поддерживает глубину «"+label+"»")
	}
	return l, ""
}

func (a *coreAgents) codexLine(workspaceID, resumeID string, launch *agents.Launch) (string, error) {
	cfg, err := a.codexLauncherConfig(launch)
	if err != nil {
		return "", err
	}
	return cfg.LauncherLine(workspaceID, resumeID, launch)
}

// codexLauncherConfig is what a Codex launch line is built from: the launcher script as the pane runs it, and the
// profiles a launch may name.
func (a *coreAgents) codexLauncherConfig(launch *agents.Launch) (codex.LaunchConfig, error) {
	content, err := a.codexLauncherContent()
	if err != nil {
		return codex.LaunchConfig{}, err
	}
	return codex.LaunchConfig{LauncherScript: a.codexLauncher, LauncherContent: content, Profiles: a.codexArgs.Profiles}, nil
}

// codexLauncherContent is the Codex launcher script as the pane runs it.
func (a *coreAgents) codexLauncherContent() ([]byte, error) {
	content, err := os.ReadFile(a.codexLauncher)
	if err != nil && a.launcherScript != nil {
		content, err = codex.LauncherScript(*a.launcherScript), nil
	}
	if err != nil {
		return nil, errors.New("the Codex launcher " + a.codexLauncher + " is not written yet (a runtime in front of a previous one leaves it to that one, " +
			"or needs RUNTIME_SCRIPT_DATA_DIRS and RUNTIME_MCP_TOKEN_ENV to write it)")
	}
	return content, nil
}

// LaunchLine is the line that starts a new conversation.
func (a *coreAgents) LaunchLine(provider, workspaceID string, launch *agents.Launch) (string, error) {
	if provider == agents.Codex {
		return a.codexLine(workspaceID, "", launch)
	}
	return a.claude.LaunchCommand(workspaceID, launch, nil)
}

// ResumeLine is the line that resumes a conversation.
func (a *coreAgents) ResumeLine(provider, sessionID, workspaceID string, launch *agents.Launch) (string, error) {
	if provider == agents.Codex {
		return a.codexLine(workspaceID, sessionID, launch)
	}
	return a.claude.ResumeCommand(sessionID, workspaceID, launch)
}

// ForkLine is the line that starts the agent on a fork of a session: Codex's `fork`, Claude's `--fork-session`.
func (a *coreAgents) ForkLine(provider, sessionID, workspaceID string, launch *agents.Launch) (string, error) {
	if provider == agents.Codex {
		cfg, err := a.codexLauncherConfig(launch)
		if err != nil {
			return "", err
		}
		return cfg.ForkLauncherLine(workspaceID, sessionID, launch)
	}
	line, err := a.claude.ResumeCommand(sessionID, workspaceID, launch)
	if err != nil {
		return "", err
	}
	return line + " --fork-session", nil
}

func (a *coreAgents) launching() bool { return a.launchOn != nil && a.launchOn() && a.starter != nil }

// lines are the launch lines as the starter asks for them.
type lines struct{ a *coreAgents }

func (l lines) Launch(provider, workspaceID string, launch *agents.Launch, promptFiles []string) (string, error) {
	if provider == agents.Codex {
		return l.a.codexLine(workspaceID, "", launch)
	}
	return l.a.claude.LaunchCommand(workspaceID, launch, promptFiles)
}

func (l lines) Resume(provider, sessionID, workspaceID string, launch *agents.Launch) (string, error) {
	return l.a.ResumeLine(provider, sessionID, workspaceID, launch)
}

func (l lines) Fork(provider, sessionID, workspaceID string, launch *agents.Launch) (string, error) {
	return l.a.ForkLine(provider, sessionID, workspaceID, launch)
}

func optionalText(v any) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}

// Start starts a conversation while the launch slice is on. A start that fails (the Codex launcher cannot be written,
// say) is a 500 whose body says why.
func (a *coreAgents) Start(r layoutapi.StartRequest) layoutapi.StartResult {
	if !a.launching() {
		return layoutapi.StartResult{Status: http.StatusServiceUnavailable, Body: errorJSON(locale.Pick("Could not start the agent: the runtime is starting", "Не удалось запустить агента: рантайм запускается"))}
	}
	res, err := a.starter.Start(launch.Options{Provider: r.Provider, WorkspaceID: r.WorkspaceID, Prompt: optionalText(r.Prompt),
		Launch: r.Launch, TabName: optionalText(r.TabName)})
	if err != nil {
		agent.Log("error", "core: the conversation did not start", agent.F{K: "provider", V: r.Provider}, agent.F{K: "error", V: err.Error()})
		return layoutapi.StartResult{Status: http.StatusInternalServerError,
			Body: errorJSON(locale.Pick("Could not start the agent: "+err.Error(), "Не удалось запустить агента: "+err.Error()))}
	}
	if res.OK {
		return layoutapi.StartResult{ConversationID: res.ConversationID()}
	}
	return layoutapi.StartResult{Status: res.Status, Body: jsvalue.Stringify(res.Body, "")}
}

// AutoResume resumes the agents that ran before a restart, while the launch slice is on.
func (a *coreAgents) AutoResume() {
	if a.launching() && a.resume != nil {
		a.resume()
	}
}

// errorJSON is {"error": text} as a body.
func errorJSON(text string) []byte {
	raw, _ := json.Marshal(errorBody(text))
	return raw
}
