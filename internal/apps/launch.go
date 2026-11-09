package apps

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// What a chat's launch gets of the apps on in it (agents.AppLauncher): each app's package as a Claude Code plugin, their
// instructions in one system-prompt file, the snapshot of their tool rules (policy.go), and a settings file that is the
// chat's own plus the app-guard hook, which decides every tool call by that snapshot (harnessly hook app-guard
// --policy <file>). The files are written under <data>/apps/launch, named by their content, so a launch with the same
// apps and the same base settings reuses them.

// GuardTimeoutSeconds is how long Claude waits for the guard. The guard reads one local file and answers at once; one
// that could not read it refuses the call.
const GuardTimeoutSeconds = 15

// ContextTimeoutSeconds is how long Claude waits for the context hook, which calls the app's tool through the runtime.
const ContextTimeoutSeconds = 30

func snapHasContext(snap Snapshot) bool {
	for _, a := range snap.Apps {
		if len(a.Context) > 0 {
			return true
		}
	}
	return false
}

// Launcher is agents.AppLauncher over a registry.
type Launcher struct {
	Registry *Registry
	// CLIHeader is the request header the runtime takes the CLI's token in; the agent's CLI and the hooks get it, since
	// the guard runs the CLI by its path, past the pane's wrapper that names it. "" leaves the CLI's default.
	CLIHeader string
	// GuardCommand is the hook command line; "" is `harnessly hook app-guard` off the agents' PATH.
	GuardCommand string
	// GuardProbe runs the hook command line once at a launch and returns an error when the guard does not answer; nil skips
	// the check (a test).
	GuardProbe func(command string) error
	// MCPConfigPath is the runtime's `--mcp-config` file (the preview server's); the host's app tools reach the same
	// endpoint with the same token, under the server name harnessly-app. "" leaves the chat without them.
	MCPConfigPath string
	// Boundary is the operating system's wall around the agent (boundary.go); nil launches without it and says nothing,
	// which only a test does.
	Boundary *Boundary
	// RequireWall refuses a chat with apps whose wall cannot be made, with the reason and the fix (a deployment's
	// RUNTIME_APPS_REQUIRE_WALL=1); false starts it without the wall and says so, as boundary.go says why.
	RequireWall bool
	// AgentToken issues the token of one launch (gate.CLIToken.IssueAgent), which the routes that change the apps
	// refuse; the agent's CLI reads it from the file HARNESSLY_CLI_TOKEN_FILE names. nil gives the agent none (a test).
	AgentToken func(session string, ttl time.Duration) (string, error)
	// Secrets are the machine's safe, for the ${secret:<name>} an app's .mcp.json names (launch_secrets.go); nil leaves
	// the servers that name one out of the launch.
	Secrets SecretSource
	// Connections are the person's connections, for the servers of an app's .mcp.json that name one (launch_connections.go);
	// nil leaves those servers out of the launch.
	Connections ConnectionSource
}

// AgentTokenTTL is how long the token of a launch holds. A chat that runs longer is given a new one when the agent
// starts next (a resume is a launch); the files of older launches are removed when a new one is written.
const AgentTokenTTL = 30 * 24 * time.Hour

// agentTokenFile writes the token of a new launch into a private file and gives its path. The token is in a file, not in
// the launch line, so it is in no command line and no shell's history. The file lives in the launch's own folder
// (sessionDir), which the wall opens read-only for this chat and for no other: another chat's agent cannot read the token
// and go with it.
func (l *Launcher) agentTokenFile() (path, token, session string, err error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", "", "", err
	}
	session = hex.EncodeToString(raw[:])
	token, err = l.AgentToken(session, AgentTokenTTL)
	if err != nil {
		return "", "", "", err
	}
	if err := os.MkdirAll(l.sessionDir(session), 0o700); err != nil {
		return "", "", "", err
	}
	path = filepath.Join(l.sessionDir(session), tokenName)
	if err := writeFileAtomic(path, []byte(token+"\n")); err != nil {
		return "", "", "", err
	}
	return path, token, session, nil
}

var _ agents.AppLauncher = (*Launcher)(nil)

// Claude is what a Claude Code chat with the apps adds to its launch.
func (l *Launcher) Claude(ids []string, settingsPath string) (agents.ClaudeApps, error) {
	return l.ClaudeFor("", ids, settingsPath)
}

// ClaudeFor is Claude for the launch of a chat: its grant names the chat (writeGrant).
func (l *Launcher) ClaudeFor(conversation string, ids []string, settingsPath string) (agents.ClaudeApps, error) {
	var out agents.ClaudeApps
	var prompt strings.Builder
	protected, err := l.Registry.ProtectedDirs()
	if err != nil {
		return out, err
	}
	snap := Snapshot{Protected: protected}
	dir := filepath.Join(l.Registry.Root, "launch")
	var installed []Installed
	for _, id := range ids {
		a, err := l.Registry.Get(id)
		if err != nil {
			return out, fmt.Errorf("app %s: %w", id, err)
		}
		if a.Manifest == nil || !a.Manifest.HasTarget(TargetChat) {
			return out, fmt.Errorf("app %s does not work in a chat", id)
		}
		out.PluginDirs = append(out.PluginDirs, a.Dir)
		installed = append(installed, a)
		sa := SnapshotApp{ID: a.ID, Name: a.Manifest.Name, Tools: toolsOf(a.Manifest), Context: ContextItems(a.Manifest),
			Hooks: a.Manifest.Allows("hooks.agent"), Strict: a.Manifest.Allows("hooks.agent") && a.Manifest.Allows("hooks.strict")}
		// Scripts run only for a trusted app: an install made before this rule, or edited since, does not get round it.
		if vs := ValidatorsOf(a.Manifest); len(vs) > 0 && a.Manifest.Trust == TrustTrusted {
			sa.Dir, sa.Validators = a.Dir, vs
		}
		snap.Apps = append(snap.Apps, sa)
		text, err := instructions(a)
		if err != nil {
			return out, fmt.Errorf("app %s: %w", id, err)
		}
		if text != "" {
			fmt.Fprintf(&prompt, "## App: %s (%s)\n\n%s\n\n", a.Manifest.Name, a.ID, text)
		}
	}
	if prompt.Len() > 0 {
		body := "# Apps on in this chat\n\nThe person turned these apps on in this chat. Keep to what each says; a tool the app " +
			"does not allow is refused by its guard, and some calls wait for the person to confirm.\n\n" + prompt.String()
		path, err := writeByContent(dir, "prompt", ".md", []byte(body))
		if err != nil {
			return out, err
		}
		out.PromptFile = path
	}
	rawSnap, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return out, err
	}
	policy, err := writeByContent(dir, "policy", ".json", rawSnap)
	if err != nil {
		return out, err
	}
	guard := l.guardCommand() + " --policy " + agents.ShellQuote(policy)
	if l.GuardProbe != nil {
		// A guard that does not answer here would not answer in the chat either; Claude lets a call through when its hook
		// fails, so the chat does not start without one that works (the deny and ask rules of the settings stand alone).
		if err := l.GuardProbe(guard); err != nil {
			return out, fmt.Errorf("the apps' guard does not answer, so the chat does not start with apps on (%s): %w", guard, err)
		}
	}
	settings, err := guardSettings(settingsPath, guard, snap)
	if err != nil {
		return out, err
	}
	if out.Settings, err = writeByContent(dir, "settings", ".json", settings); err != nil {
		return out, err
	}
	// The agent's calls to the runtime carry the token of this launch, not the person's: its CLI (the file
	// HARNESSLY_CLI_TOKEN_FILE names, which the hook scripts read too) and both MCP configs. Configs written before
	// this held the person's token: they go.
	token, session, sealDir := "", "", dir
	l.dropLegacyLaunchFiles(dir)
	if l.AgentToken != nil {
		file, tok, sess, err := l.agentTokenFile()
		if err != nil {
			return out, fmt.Errorf("the agent's token: %w", err)
		}
		token, session = tok, sess
		// Everything that holds this launch's token lives in its own folder, read-only inside the wall (sealDir).
		sealDir = l.sessionDir(session)
		// What this token may ask the runtime's MCP proxy for: the apps on in this chat now (launch_secrets.go). The agent
		// cannot write it: its folder is a read-only bind in the wall. A new launch writes a new one.
		if err := writeGrant(sealDir, ids, conversation); err != nil {
			return out, fmt.Errorf("the apps' grant: %w", err)
		}
		// The pane's shell names the tab's tmux session before the wall: inside it the tmux server is out of reach
		// (a window it opened would run outside the wall), and the hooks and the CLI take the session from here.
		out.Env = l.agentEnv(file)
		// Other MCP configs of the runtime (the person's MCP servers, reached through it) go in with this token too:
		// the person's would read as nothing behind the wall.
		out.Retoken = func(src string) (string, error) { return retokenMCP(sealDir, "xmcp", src, tok) }
	}
	if l.MCPConfigPath != "" {
		if out.MCPConfig, err = l.hostMCP(sealDir, token); err != nil {
			return out, err
		}
		if token != "" {
			if out.RuntimeMCPConfig, err = l.runtimeMCP(sealDir, token); err != nil {
				return out, err
			}
		}
	}
	// An app whose MCP servers use a secret loads from a folder of this launch (launch_secrets.go).
	var own []string
	if token != "" {
		own = []string{sealDir}
	}
	if l.Secrets != nil || token != "" {
		l.sweepLaunches()
		for i, a := range installed {
			pluginDir, err := l.pluginDir(a, session, token)
			if err != nil {
				return out, fmt.Errorf("app %s: %w", a.ID, err)
			}
			out.PluginDirs[i] = pluginDir
		}
	}
	wrap, err := l.wall(ids, snap.Protected, token != "", own)
	if err != nil {
		return out, err
	}
	out.Wrap = wrap
	return out, nil
}

// runtimeMCP is the runtime's own --mcp-config for a chat with apps: the same server, with the launch's token in its
// headers in place of the person's.
func (l *Launcher) runtimeMCP(dir, token string) (string, error) {
	return retokenMCP(dir, "rmcp", l.MCPConfigPath, token)
}

// retokenMCP writes a copy of the MCP config at src with token in every header of every server, into dir under prefix,
// and gives its path.
func retokenMCP(dir, prefix, src, token string) (string, error) {
	raw, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	var cfg struct {
		Servers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "", fmt.Errorf("%s: %w", src, err)
	}
	for _, srv := range cfg.Servers {
		withToken(srv, token)
	}
	body, err := json.MarshalIndent(map[string]any{"mcpServers": cfg.Servers}, "", "  ")
	if err != nil {
		return "", err
	}
	return writeByContent(dir, prefix, ".json", body)
}

// withToken puts the token in every header of a server's config (the runtime's config carries the cli-token in each).
func withToken(srv map[string]any, token string) {
	if token == "" {
		return
	}
	if headers, ok := srv["headers"].(map[string]any); ok {
		for k := range headers {
			headers[k] = token
		}
	}
}

// wall is the wall's words for the launch line, "" when the machine cannot make it: the chat then starts without it,
// and the log says so at every such launch (see boundary.go for why it is not refused), unless the deployment requires
// the wall (RequireWall): then the launch fails with the reason and the fix.
func (l *Launcher) wall(ids []string, protected []string, hideCredentials bool, own []string) (string, error) {
	if l.Boundary == nil {
		if l.RequireWall {
			return "", ErrWallRequired{Cause: errors.New("this runtime has no wall")}
		}
		return "", nil
	}
	if err := l.Boundary.Ready(l.Registry.Root); err != nil {
		if l.RequireWall {
			return "", ErrWallRequired{Cause: err}
		}
		jsonlog.Log("warn", "apps: the agent of a chat with apps starts without the operating system's wall around the apps",
			jsonlog.F{K: "apps", V: strings.Join(ids, ",")}, jsonlog.F{K: "error", V: err.Error()})
		return "", nil
	}
	if !hideCredentials {
		return l.Boundary.Words(protected), nil
	}
	data := filepath.Dir(l.Registry.Root)
	hidden := []string{filepath.Join(data, "cli-token"), filepath.Join(data, gate.AgentTokenFile)}
	if l.MCPConfigPath != "" {
		hidden = append(hidden, l.MCPConfigPath)
	}
	home, _ := os.UserHomeDir()
	return l.Boundary.SealedWords(protected, &Seal{Data: data, Home: home, Open: own}, hidden...), nil
}

// ErrWallRequired: the deployment requires the wall around the apps (RUNTIME_APPS_REQUIRE_WALL) and this machine cannot
// make it. The text names the cause and the command that puts it right.
type ErrWallRequired struct{ Cause error }

func (e ErrWallRequired) Error() string {
	fix := "allow unprivileged user namespaces: sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0 " +
		"(or kernel.unprivileged_userns_clone=1), then check «Проверка машины» → apps_boundary"
	if errors.Is(e.Cause, ErrNoBwrap) {
		fix = "install bubblewrap: sudo apt-get install -y bubblewrap (dnf install bubblewrap), then check «Проверка машины» → apps_boundary"
	}
	return "the chat does not start with apps on: this deployment requires the wall around the apps " +
		"(RUNTIME_APPS_REQUIRE_WALL) and it cannot be made here (" + e.Cause.Error() + "); " + fix +
		", or turn the apps off in this chat"
}

func (e ErrWallRequired) Unwrap() error { return e.Cause }

// HostMCPFile is the name, in the runtime's data folder, of the harnessly-app config a chat without apps starts with.
const HostMCPFile = "app-mcp.json"

// HostMCP is the --mcp-config of the harnessly-app server for a Claude Code chat that starts with no app on:
// the server is quiet until an app turns on in the chat, then its tools come live (live.go). It holds the person's token,
// as the runtime's own config beside it does, so it lives in the data folder, which the wall of a launch with apps
// leaves unreadable (seal.go). "" when the runtime has no MCP config.
func (l *Launcher) HostMCP() (string, error) {
	if l.MCPConfigPath == "" {
		return "", nil
	}
	return l.hostMCPAt(filepath.Join(filepath.Dir(l.Registry.Root), HostMCPFile))
}

func (l *Launcher) hostMCPAt(path string) (string, error) {
	body, err := l.hostMCPBody("")
	if err != nil {
		return "", err
	}
	if have, err := os.ReadFile(path); err == nil && string(have) == string(body) {
		return path, nil
	}
	if err := writeFileAtomic(path, body); err != nil {
		return "", err
	}
	return path, nil
}

// hostMCP writes the config of the harnessly-app server: the runtime's own MCP endpoint and token (read from its config
// file), selected by ?server=harnessly-app, so the app tools come under that name and not the runtime's.
func (l *Launcher) hostMCP(dir, token string) (string, error) {
	body, err := l.hostMCPBody(token)
	if err != nil {
		return "", err
	}
	// The file holds the runtime's token; writeFileAtomic keeps it 0600.
	return writeByContent(dir, "hmcp", ".json", body)
}

// hostMCPBody is the config of the harnessly-app server with token in its headers ("" keeps the runtime's own).
func (l *Launcher) hostMCPBody(token string) ([]byte, error) {
	raw, err := os.ReadFile(l.MCPConfigPath)
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Servers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", l.MCPConfigPath, err)
	}
	for _, srv := range cfg.Servers {
		url, _ := srv["url"].(string)
		if url == "" {
			continue
		}
		sep := "?"
		if strings.Contains(url, "?") {
			sep = "&"
		}
		srv["url"] = url + sep + "server=" + HostServer
		withToken(srv, token)
		return json.MarshalIndent(map[string]any{"mcpServers": map[string]any{HostServer: srv}}, "", "  ")
	}
	return nil, fmt.Errorf("%s names no server with an address", l.MCPConfigPath)
}

func (l *Launcher) guardCommand() string {
	if l.GuardCommand != "" {
		return l.GuardCommand
	}
	return "harnessly hook app-guard"
}

func toolsOf(m *Manifest) *ToolPolicy {
	if m.Agent == nil {
		return nil
	}
	return m.Agent.Tools
}

// instructions is an app's instructions file, trimmed; "" when it has none.
func instructions(a Installed) (string, error) {
	if a.Manifest.Agent == nil || a.Manifest.Agent.Instructions == "" {
		return "", nil
	}
	p := &Package{Dir: a.Dir, Manifest: a.Manifest}
	raw, err := p.File(a.Manifest.Agent.Instructions)
	if err != nil {
		return "", err
	}
	if len(raw) > MaxInstructionsBytes {
		raw = raw[:MaxInstructionsBytes]
	}
	return strings.TrimSpace(string(raw)), nil
}

// guardSettings is the settings file at path with the app-guard hook first among its PreToolUse hooks, the apps' deny and
// confirm rules as Claude's permissions.deny and permissions.ask (claude_rules.go), and the agent's
// own file tools denied in the protected folders: Claude refuses these writes itself, so they do not rest on the hook
// alone. (Neither holds against a shell the agent bends to its will, as it runs as the person: the wall of boundary.go does.)
func guardSettings(path, command string, snap Snapshot) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	pre, _ := hooks["PreToolUse"].([]any)
	guard := map[string]any{
		"matcher": "*",
		"hooks":   []any{map[string]any{"type": "command", "command": command, "timeout": GuardTimeoutSeconds}},
	}
	hooks["PreToolUse"] = append([]any{guard}, pre...)
	if snapHasContext(snap) {
		// The apps' context goes in on the two events that start a turn; a hook that fails adds nothing and stops nothing.
		ctx := map[string]any{"hooks": []any{map[string]any{
			"type": "command", "command": strings.Replace(command, "hook app-guard", "hook app-context", 1), "timeout": ContextTimeoutSeconds,
		}}}
		for _, ev := range []string{"SessionStart", "UserPromptSubmit"} {
			have, _ := hooks[ev].([]any)
			hooks[ev] = append(have, ctx)
		}
	}
	if snap.HasValidators() {
		// The apps' checks of the turn: after a tool call and before the agent stops. A hook that fails blocks nothing.
		base := strings.Replace(command, "hook app-guard", "hook app-validate", 1)
		for _, ev := range []string{ValidatorPostToolUse, ValidatorStop} {
			entry := map[string]any{"hooks": []any{map[string]any{
				"type": "command", "command": base + " --event " + ev, "timeout": ValidatorHookTimeoutSeconds,
			}}}
			if ev == ValidatorPostToolUse {
				entry["matcher"] = "*"
			}
			have, _ := hooks[ev].([]any)
			hooks[ev] = append(have, entry)
		}
	}
	settings["hooks"] = hooks
	deny := denyEdits(snap.Protected)
	appDeny, appAsk := ClaudeRules(snap.Apps)
	deny = append(deny, appDeny...)
	if len(deny) > 0 || len(appAsk) > 0 {
		perms, _ := settings["permissions"].(map[string]any)
		if perms == nil {
			perms = map[string]any{}
		}
		for key, rules := range map[string][]string{"deny": deny, "ask": appAsk} {
			have, _ := perms[key].([]any)
			for _, rule := range rules {
				if !slices.Contains(have, any(rule)) {
					have = append(have, rule)
				}
			}
			if len(have) > 0 {
				perms[key] = have
			}
		}
		settings["permissions"] = perms
	}
	return json.MarshalIndent(settings, "", "  ")
}

// denyEdits are Claude's own deny rules for the file tools in the protected folders. A path rule starting with // is an
// absolute path; the folders are named as the launch has them and as they really are. Edit's rule covers every tool
// that changes a file (Write, MultiEdit, NotebookEdit): a Write(…) path rule is not matched, and Claude warns of it at
// every start.
func denyEdits(protected []string) []string {
	var out []string
	for _, dir := range protected {
		for _, form := range protectedForms(dir) {
			if !filepath.IsAbs(form) {
				continue
			}
			if rule := "Edit(/" + form + "/**)"; !slices.Contains(out, rule) {
				out = append(out, rule)
			}
		}
	}
	return out
}

// SnapshotIntact says whether a snapshot's bytes are the ones its name was made from (writeByContent): a snapshot
// changed after the launch is not one the guard decides by.
func SnapshotIntact(path string, body []byte) bool {
	sum := sha256.Sum256(body)
	return filepath.Base(path) == "policy-"+hex.EncodeToString(sum[:8])+".json"
}

// writeByContent writes body to <dir>/<prefix>-<hash><ext> unless it is there, and gives the path.
func writeByContent(dir, prefix, ext string, body []byte) (string, error) {
	sum := sha256.Sum256(body)
	path := filepath.Join(dir, prefix+"-"+hex.EncodeToString(sum[:8])+ext)
	if have, err := os.ReadFile(path); err == nil && string(have) == string(body) {
		return path, nil
	}
	if err := writeFileAtomic(path, body); err != nil {
		return "", err
	}
	return path, nil
}

// agentEnv are the shell words the agent of a chat with apps starts with: its launch's token file, the header the
// runtime takes it in, and the tab's tmux session, named before the wall.
func (l *Launcher) agentEnv(tokenFile string) string {
	env := "HARNESSLY_CLI_TOKEN_FILE=" + agents.ShellQuote(tokenFile)
	if l.CLIHeader != "" {
		env += " HARNESSLY_CLI_HEADER=" + agents.ShellQuote(l.CLIHeader)
	}
	return env + ` HARNESSLY_SESSION="$(tmux display-message -p '#{session_name}' 2>/dev/null)"`
}
