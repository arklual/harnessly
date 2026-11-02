package apps

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// Launcher is agents.AppLauncher over a registry.
type Launcher struct {
	Registry *Registry
	// GuardCommand is the hook command line; "" is `harnessly hook app-guard` off the agents' PATH.
	GuardCommand string
	// MCPConfigPath is the runtime's `--mcp-config` file (the preview server's); the host's app tools reach the same
	// endpoint with the same token, under the server name harnessly-app. "" leaves the chat without them.
	MCPConfigPath string
	// Boundary is the operating system's wall around the agent (boundary.go); nil launches without it and says nothing,
	// which only a test does.
	Boundary *Boundary
	// AgentToken issues the token of one launch (gate.CLIToken.IssueAgent), which the routes that change the apps
	// refuse; the agent's CLI reads it from the file HARNESSLY_CLI_TOKEN_FILE names. nil gives the agent none (a test).
	AgentToken func(session string, ttl time.Duration) (string, error)
}

// AgentTokenTTL is how long the token of a launch holds. A chat that runs longer is given a new one when the agent
// starts next (a resume is a launch); the files of older launches are removed when a new one is written.
const AgentTokenTTL = 30 * 24 * time.Hour

// agentTokenFile writes the token of a new launch into a private file under dir and gives its path. The token is in a
// file, not in the launch line, so it is in no command line and no shell's history.
func (l *Launcher) agentTokenFile(dir string) (path, token string, err error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", "", err
	}
	session := hex.EncodeToString(raw[:])
	token, err = l.AgentToken(session, AgentTokenTTL)
	if err != nil {
		return "", "", err
	}
	if old, _ := filepath.Glob(filepath.Join(dir, "agent-token-*")); len(old) > 0 {
		for _, f := range old {
			if info, err := os.Stat(f); err == nil && time.Since(info.ModTime()) > AgentTokenTTL {
				_ = os.Remove(f)
			}
		}
	}
	path = filepath.Join(dir, "agent-token-"+session)
	if err := writeFileAtomic(path, []byte(token+"\n")); err != nil {
		return "", "", err
	}
	return path, token, nil
}

var _ agents.AppLauncher = (*Launcher)(nil)

// Claude is what a Claude Code chat with the apps adds to its launch.
func (l *Launcher) Claude(ids []string, settingsPath string) (agents.ClaudeApps, error) {
	var out agents.ClaudeApps
	var prompt strings.Builder
	protected, err := l.Registry.ProtectedDirs()
	if err != nil {
		return out, err
	}
	snap := Snapshot{Protected: protected}
	for _, id := range ids {
		a, err := l.Registry.Get(id)
		if err != nil {
			return out, fmt.Errorf("app %s: %w", id, err)
		}
		if a.Manifest == nil || !a.Manifest.HasTarget(TargetChat) {
			return out, fmt.Errorf("app %s does not work in a chat", id)
		}
		out.PluginDirs = append(out.PluginDirs, a.Dir)
		snap.Apps = append(snap.Apps, SnapshotApp{ID: a.ID, Name: a.Manifest.Name, Tools: toolsOf(a.Manifest)})
		text, err := instructions(a)
		if err != nil {
			return out, fmt.Errorf("app %s: %w", id, err)
		}
		if text != "" {
			fmt.Fprintf(&prompt, "## App: %s (%s)\n\n%s\n\n", a.Manifest.Name, a.ID, text)
		}
	}
	dir := filepath.Join(l.Registry.Root, "launch")
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
	settings, err := guardSettings(settingsPath, l.guardCommand()+" --policy "+agents.ShellQuote(policy), snap.Protected)
	if err != nil {
		return out, err
	}
	if out.Settings, err = writeByContent(dir, "settings", ".json", settings); err != nil {
		return out, err
	}
	// The agent's calls to the runtime carry the token of this launch, not the person's: its CLI (the file
	// HARNESSLY_CLI_TOKEN_FILE names, which the hook scripts read too) and both MCP configs. Configs written before
	// this held the person's token: they go.
	token := ""
	if l.AgentToken != nil {
		file, tok, err := l.agentTokenFile(dir)
		if err != nil {
			return out, fmt.Errorf("the agent's token: %w", err)
		}
		token = tok
		// The pane's shell names the tab's tmux session before the wall: inside it the tmux server is out of reach
		// (a window it opened would run outside the wall), and the hooks and the CLI take the session from here.
		out.Env = "HARNESSLY_CLI_TOKEN_FILE=" + agents.ShellQuote(file) +
			` HARNESSLY_SESSION="$(tmux display-message -p '#{session_name}' 2>/dev/null)"`
		if old, _ := filepath.Glob(filepath.Join(dir, "mcp-*.json")); len(old) > 0 {
			for _, f := range old {
				_ = os.Remove(f)
			}
		}
	}
	if l.MCPConfigPath != "" {
		if out.MCPConfig, err = l.hostMCP(dir, token); err != nil {
			return out, err
		}
		if token != "" {
			if out.RuntimeMCPConfig, err = l.runtimeMCP(dir, token); err != nil {
				return out, err
			}
		}
	}
	out.Wrap = l.wall(ids, snap.Protected, token != "")
	return out, nil
}

// runtimeMCP is the runtime's own --mcp-config for a chat with apps: the same server, with the launch's token in its
// headers in place of the person's.
func (l *Launcher) runtimeMCP(dir, token string) (string, error) {
	raw, err := os.ReadFile(l.MCPConfigPath)
	if err != nil {
		return "", err
	}
	var cfg struct {
		Servers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "", fmt.Errorf("%s: %w", l.MCPConfigPath, err)
	}
	for _, srv := range cfg.Servers {
		withToken(srv, token)
	}
	body, err := json.MarshalIndent(map[string]any{"mcpServers": cfg.Servers}, "", "  ")
	if err != nil {
		return "", err
	}
	return writeByContent(dir, "rmcp", ".json", body)
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
// and the log says so at every such launch (see boundary.go for why it is not refused).
func (l *Launcher) wall(ids []string, protected []string, hideCredentials bool) string {
	if l.Boundary == nil {
		return ""
	}
	if err := l.Boundary.Ready(l.Registry.Root); err != nil {
		jsonlog.Log("warn", "apps: the agent of a chat with apps starts without the operating system's wall around the apps",
			jsonlog.F{K: "apps", V: strings.Join(ids, ",")}, jsonlog.F{K: "error", V: err.Error()})
		return ""
	}
	if !hideCredentials {
		return l.Boundary.Words(protected)
	}
	data := filepath.Dir(l.Registry.Root)
	hidden := []string{filepath.Join(data, "cli-token"), filepath.Join(data, gate.AgentTokenFile)}
	if l.MCPConfigPath != "" {
		hidden = append(hidden, l.MCPConfigPath)
	}
	home, _ := os.UserHomeDir()
	return l.Boundary.SealedWords(protected, &Seal{Data: data, Home: home}, hidden...)
}

// hostMCP writes the config of the harnessly-app server: the runtime's own MCP endpoint and token (read from its config
// file), selected by ?server=harnessly-app, so the app tools come under that name and not the runtime's.
func (l *Launcher) hostMCP(dir, token string) (string, error) {
	raw, err := os.ReadFile(l.MCPConfigPath)
	if err != nil {
		return "", err
	}
	var cfg struct {
		Servers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "", fmt.Errorf("%s: %w", l.MCPConfigPath, err)
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
		body, err := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{HostServer: srv}}, "", "  ")
		if err != nil {
			return "", err
		}
		// The file holds the runtime's token; writeFileAtomic keeps it 0600.
		return writeByContent(dir, "hmcp", ".json", body)
	}
	return "", fmt.Errorf("%s names no server with an address", l.MCPConfigPath)
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

// guardSettings is the settings file at path with the app-guard hook first among its PreToolUse hooks, and the agent's
// own file tools denied in the protected folders: Claude refuses these writes itself, so they do not rest on the hook
// alone. (Neither holds against a shell the agent bends to its will, as it runs as the person: the wall of boundary.go does.)
func guardSettings(path, command string, protected []string) ([]byte, error) {
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
	settings["hooks"] = hooks
	if deny := denyEdits(protected); len(deny) > 0 {
		perms, _ := settings["permissions"].(map[string]any)
		if perms == nil {
			perms = map[string]any{}
		}
		have, _ := perms["deny"].([]any)
		for _, rule := range deny {
			if !slices.Contains(have, any(rule)) {
				have = append(have, rule)
			}
		}
		perms["deny"] = have
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
