package claude

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// ErrLauncher: the managed launcher must be an absolute path without control characters.
var ErrLauncher = errors.New("Managed Claude launcher must be an absolute path without control characters")

// LaunchConfig is what a Claude launch line is built from; the deployment fills it.
type LaunchConfig struct {
	// HooksPath is the `--settings` file (HookSettings), MCPConfigPath the `--mcp-config` file.
	HooksPath     string
	MCPConfigPath string
	// WorkspacePrompt is a workspace's system-prompt file; nil when workspaces have none.
	WorkspacePrompt func(workspaceID string) string
	// PluginDir is the folder of the plugin carrying the workspace's skills, loaded with --plugin-dir; nil, or "" from
	// it, loads none.
	PluginDir func() string
	// EnvPluginDirs are the folders of the hub's environment plugin (skills, documents, repositories and MCP servers of
	// the team, docs/hub/environment.md), each loaded with --plugin-dir after PluginDir; nil, or none, loads none.
	EnvPluginDirs func() []string
	// JoinDir is where several prompt files are joined into one for the bare CLI.
	JoinDir string
	// Launcher is the managed launcher to run instead of `claude`; "" runs `claude`.
	Launcher string
	// SkipPermissions adds --dangerously-skip-permissions when it says so.
	SkipPermissions func() bool
	// EditorScript gives (writing it if needed) the prompt editor script Claude gets as VISUAL and EDITOR; nil or an
	// error launches without it.
	EditorScript func() (string, error)
	// HumanEditorVar is the variable the editor script finds the person's own editor in.
	HumanEditorVar string
	// Profiles gives a launch on a backend profile its settings file; nil refuses such a launch.
	Profiles agents.ProfileLauncher
	// Apps gives a launch with apps their plugins, instructions and guard; nil refuses such a launch.
	Apps agents.AppLauncher
	// ExtraMCP gives, at every launch and resume, one more --mcp-config file (the person's MCP servers from Settings →
	// Connections, reached through the runtime) and the permission rules that let its tools run without asking. nil, or
	// "" from it, adds none.
	ExtraMCP func() (path string, allow []string)
}

// ErrNoProfiles: a launch names a backend profile and this deployment has none.
var ErrNoProfiles = errors.New("Backend profiles are not available on this machine")

// LaunchEnv keeps a Claude the runtime starts from updating itself mid-session: the machine installs new versions
// ahead of time, and the next session starts on them.
const LaunchEnv = "DISABLE_AUTOUPDATER=1"

// LauncherCommand is what runs Claude: `claude`, or the managed launcher single-quoted, after LaunchEnv.
func LauncherCommand(launcher string) (string, error) {
	if launcher == "" {
		return LaunchEnv + " " + Binary, nil
	}
	if !filepath.IsAbs(launcher) || strings.ContainsFunc(launcher, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "", ErrLauncher
	}
	return LaunchEnv + " " + agents.ShellQuote(launcher), nil
}

// wrappedLauncher is LauncherCommand with the CLI under wrap (shell words), after the environment: `VAR=1 <wrap> claude`.
func wrappedLauncher(launcher, wrap string) (string, error) {
	command, err := LauncherCommand(launcher)
	if err != nil || wrap == "" {
		return command, err
	}
	return strings.Replace(command, LaunchEnv+" ", LaunchEnv+" "+wrap+" ", 1), nil
}

// LaunchFlags are `--model '<m>'` and `--effort <e>` of a choice, checked again first.
func LaunchFlags(l *agents.Launch) ([]string, error) {
	if l == nil {
		return nil, nil
	}
	if err := agents.CheckLaunch(l); err != nil {
		return nil, err
	}
	var flags []string
	if l.Model != nil && *l.Model != "" {
		flags = append(flags, "--model "+agents.ShellQuote(*l.Model))
	}
	if l.Effort != nil && *l.Effort != "" {
		flags = append(flags, "--effort "+*l.Effort)
	}
	return flags, nil
}

// EditorEnv is the environment that gives Claude the prompt editor. The person's editor is read by the pane's shell
// before VISUAL is replaced: assignments before a command expand left to right.
func EditorEnv(humanEditorVar, script string) string {
	return humanEditorVar + `="${VISUAL:-$EDITOR}" VISUAL=` + agents.ShellQuote(script) + " EDITOR=" + agents.ShellQuote(script)
}

// UltracodeSettings writes, next to the hooks settings file, a copy of it with `"ultracode": true` (rewritten only when
// its content changes) and gives its path. Claude Code takes ultracode for the session from this key and never saves
// it: a session started on it is the only way to start in the mode.
func UltracodeSettings(hooksPath string) (string, error) {
	raw, err := os.ReadFile(hooksPath)
	if err != nil {
		return "", err
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		return "", err
	}
	settings["ultracode"] = true
	body, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", err
	}
	target := strings.TrimSuffix(hooksPath, ".json") + ".ultracode.json"
	if have, err := os.ReadFile(target); err == nil && string(have) == string(body) {
		return target, nil
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return target, nil
}

// FastSettings writes, next to a settings file, a copy of it with `"fastMode"` set to on (rewritten only when its
// content changes) and gives its path: Claude Code reads one --settings.
func FastSettings(settingsPath string, on bool) (string, error) {
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		return "", err
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		return "", err
	}
	settings["fastMode"] = on
	body, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", err
	}
	suffix := ".fast-off.json"
	if on {
		suffix = ".fast-on.json"
	}
	target := strings.TrimSuffix(settingsPath, ".json") + suffix
	if have, err := os.ReadFile(target); err == nil && string(have) == string(body) {
		return target, nil
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return target, nil
}

// ClaudePermissionModes are Claude Code's names of the picker's permission modes (`--permission-mode`); `default` is
// the one that asks, named so by every version (2.1.319 calls it `manual` too).
var ClaudePermissionModes = map[wire.PermissionMode]string{
	wire.PermissionPlan:   "plan",
	wire.PermissionAsk:    "default",
	wire.PermissionBypass: "bypassPermissions",
}

// permissionFlags are how the launch's Claude asks before it acts. A launch that names a permission mode
// starts in it with --permission-mode, whatever the person's settings say (a `defaultMode` of theirs used to start a
// chat in plan mode). Never with --allow-dangerously-skip-permissions: Claude Code counts such a session as started
// with bypass, and its plan mode then «won't ask before most edits or commands». Without a mode the machine's default
// stands.
func (c LaunchConfig) permissionFlags(launch *agents.Launch) []string {
	if launch != nil && launch.Permission != "" {
		if mode, ok := ClaudePermissionModes[launch.Permission]; ok {
			return []string{"--permission-mode " + mode}
		}
		return nil
	}
	if c.SkipPermissions != nil && c.SkipPermissions() {
		return []string{"--dangerously-skip-permissions"}
	}
	return nil
}

// AllowSettings writes, next to a settings file, a copy of it with the rules added to permissions.allow (rewritten only
// when its content changes) and gives its path: Claude Code reads one --settings, so rules that change between launches
// go into a copy of the one the launch has.
func AllowSettings(settingsPath string, rules []string) (string, error) {
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		return "", err
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		return "", err
	}
	perms, _ := settings["permissions"].(map[string]any)
	if perms == nil {
		perms = map[string]any{}
	}
	have, _ := perms["allow"].([]any)
	for _, rule := range rules {
		if !slices.Contains(have, any(rule)) {
			have = append(have, rule)
		}
	}
	perms["allow"] = have
	settings["permissions"] = perms
	body, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	target := strings.TrimSuffix(settingsPath, ".json") + ".allow-" + hex.EncodeToString(sum[:8]) + ".json"
	if have, err := os.ReadFile(target); err == nil && string(have) == string(body) {
		return target, nil
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return target, nil
}

// joinPromptFiles writes the files' trimmed texts, a blank line apart, into one file named by their hash: the bare
// CLI keeps only the last --append-system-prompt-file.
func (c LaunchConfig) joinPromptFiles(files []string) (string, error) {
	parts := make([]string, len(files))
	for i, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		parts[i] = jscompat.Trim(strings.ToValidUTF8(string(raw), "�"))
	}
	body := strings.Join(parts, "\n\n")
	sum := sha256.Sum256([]byte(body))
	target := filepath.Join(c.JoinDir, hex.EncodeToString(sum[:])[:16]+".md")
	if err := os.MkdirAll(c.JoinDir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(target, []byte(body+"\n"), 0o600); err != nil {
		return "", err
	}
	return target, nil
}

func (c LaunchConfig) promptFileFlags(workspaceID string, extra []string) ([]string, error) {
	workspacePrompt := ""
	if workspaceID != "" && c.WorkspacePrompt != nil {
		workspacePrompt = c.WorkspacePrompt(workspaceID)
	}
	if len(extra) == 0 {
		if workspacePrompt == "" {
			return nil, nil
		}
		return []string{"--append-system-prompt-file " + workspacePrompt}, nil
	}
	files := extra
	if workspacePrompt != "" {
		files = append([]string{workspacePrompt}, extra...)
	}
	if c.Launcher != "" {
		out := make([]string, len(files))
		for i, f := range files {
			out[i] = "--append-system-prompt-file " + agents.ShellQuote(f)
		}
		return out, nil
	}
	joined, err := c.joinPromptFiles(files)
	if err != nil {
		return nil, err
	}
	return []string{"--append-system-prompt-file " + agents.ShellQuote(joined)}, nil
}

// Flags are the launch flags in order: settings, MCP config, skills plugin, the apps' and the person's MCP configs,
// system-prompt files, permissions, model and effort.
func (c LaunchConfig) Flags(workspaceID string, launch *agents.Launch, promptFiles []string) (string, error) {
	flags, _, err := c.flagsAndWrap(workspaceID, launch, promptFiles)
	return flags, err
}

// flagsAndWrap are Flags and the words the CLI runs under: the apps' environment and wall (agents.ClaudeApps Env and
// Wrap), in that order.
func (c LaunchConfig) flagsAndWrap(workspaceID string, launch *agents.Launch, promptFiles []string) (string, string, error) {
	settings := c.HooksPath
	// settingsFile is the same file unquoted, for the apps' file to extend.
	settingsFile := c.HooksPath
	// lineEnv are the profile's own words of the launch line's environment (the GPT chat's pane header).
	lineEnv := ""
	if launch != nil && launch.Profile != "" {
		// The profile's settings file is the hooks file plus the vendor's address, models and key helper. Claude Code
		// reads one --settings (the last one given), so it replaces the hooks file here, and the model goes in the file's
		// environment as well as in --model.
		if c.Profiles == nil {
			return "", "", ErrNoProfiles
		}
		if err := agents.CheckLaunch(launch); err != nil {
			return "", "", err
		}
		if launch.Effort != nil {
			m, ok := c.Profiles.Model(launch.Profile, *launch.Model)
			if !ok || !slices.Contains(m.Efforts, wire.Effort(*launch.Effort)) {
				return "", "", agents.ErrLaunchEffort
			}
		}
		path, err := c.Profiles.ClaudeSettings(launch.Profile, *launch.Model, c.HooksPath)
		if err != nil {
			return "", "", err
		}
		settings, settingsFile = agents.ShellQuote(path), path
		if e, ok := c.Profiles.(profileLineEnv); ok {
			lineEnv = e.ClaudeLineEnv(launch.Profile)
		}
	} else if launch != nil && launch.Ultracode {
		// Claude Code reads one --settings (the last one given): the mode goes into a copy of the hooks settings.
		path, err := UltracodeSettings(c.HooksPath)
		if err != nil {
			return "", "", err
		}
		settings, settingsFile = path, path
	}
	// Fast mode is a setting too: a launch that names its modes says it either way, so a `fastMode` the person's
	// own settings keep (Claude Code saves /fast there) does not decide it. A profile's model has no fast mode.
	if launch != nil && launch.Profile == "" && (launch.Fast || launch.Permission != "") {
		path, err := FastSettings(settingsFile, launch.Fast)
		if err != nil {
			return "", "", err
		}
		settings, settingsFile = agents.ShellQuote(path), path
	}
	// The person's MCP servers: their tools are allowed in a copy of the settings chosen above, which the apps' file
	// extends in turn. A copy that cannot be written leaves the tools asking.
	extraMCP, extraAllow := "", []string(nil)
	if c.ExtraMCP != nil {
		extraMCP, extraAllow = c.ExtraMCP()
	}
	if extraMCP != "" && len(extraAllow) > 0 {
		if path, err := AllowSettings(settingsFile, extraAllow); err == nil {
			settings, settingsFile = agents.ShellQuote(path), path
		}
	}
	var apps agents.ClaudeApps
	launch = agents.WithActiveApps(launch, c.Apps)
	if launch != nil && len(launch.Apps) > 0 {
		// The apps' settings file is the one chosen above plus the app-guard hook: Claude reads one --settings.
		if c.Apps == nil {
			return "", "", agents.ErrNoApps
		}
		if err := agents.CheckLaunch(launch); err != nil {
			return "", "", err
		}
		var err error
		if apps, err = c.Apps.ClaudeFor(launch.Conversation, launch.Apps, settingsFile); err != nil {
			return "", "", err
		}
		settings = agents.ShellQuote(apps.Settings)
	}
	mcp := c.MCPConfigPath
	if apps.RuntimeMCPConfig != "" {
		mcp = agents.ShellQuote(apps.RuntimeMCPConfig)
	}
	parts := []string{"--settings " + settings, "--mcp-config " + mcp}
	if c.PluginDir != nil {
		if dir := c.PluginDir(); dir != "" {
			parts = append(parts, "--plugin-dir "+agents.ShellQuote(dir))
		}
	}
	if c.EnvPluginDirs != nil {
		for _, dir := range c.EnvPluginDirs() {
			parts = append(parts, "--plugin-dir "+agents.ShellQuote(dir))
		}
	}
	for _, dir := range apps.PluginDirs {
		parts = append(parts, "--plugin-dir "+agents.ShellQuote(dir))
	}
	if apps.MCPConfig != "" {
		parts = append(parts, "--mcp-config "+agents.ShellQuote(apps.MCPConfig))
	} else if host, ok := c.Apps.(agents.HostMCPLauncher); ok && (launch == nil || len(launch.Apps) == 0) {
		// A chat without apps has their server too, quiet until one turns on (internal/apps live.go). One that cannot be
		// written leaves the chat without it: apps then come with the agent's next start.
		if path, err := host.HostMCP(); err == nil && path != "" {
			parts = append(parts, "--mcp-config "+agents.ShellQuote(path))
		}
	}
	if extraMCP != "" && apps.Retoken != nil {
		// Under the apps' wall the file with the person's token reads as nothing: the agent gets a copy with its own.
		var err error
		if extraMCP, err = apps.Retoken(extraMCP); err != nil {
			extraMCP = ""
		}
	}
	if extraMCP != "" {
		parts = append(parts, "--mcp-config "+agents.ShellQuote(extraMCP))
	}
	if apps.PromptFile != "" {
		promptFiles = append(slices.Clone(promptFiles), apps.PromptFile)
	}
	prompts, err := c.promptFileFlags(workspaceID, promptFiles)
	if err != nil {
		return "", "", err
	}
	parts = append(parts, prompts...)
	parts = append(parts, c.permissionFlags(launch)...)
	flags, err := LaunchFlags(launch)
	if err != nil {
		return "", "", err
	}
	parts = append(parts, flags...)
	return strings.Join(parts, " "), strings.TrimSpace(lineEnv + " " + apps.Env + " " + apps.Wrap), nil
}

// profileLineEnv is a profile launcher that adds words to a chat's launch-line environment: sh words, the pane's
// shell expanding them when the line runs (providers.Launcher).
type profileLineEnv interface {
	ClaudeLineEnv(profile string) string
}

func (c LaunchConfig) withEditor(command string) string {
	if c.EditorScript == nil {
		return command
	}
	script, err := c.EditorScript()
	if err != nil || script == "" {
		return command
	}
	return EditorEnv(c.HumanEditorVar, script) + " " + command
}

// LaunchCommand is the line that starts a new conversation (an initial prompt goes after it, see
// agents.WithInitialPrompt).
func (c LaunchConfig) LaunchCommand(workspaceID string, launch *agents.Launch, promptFiles []string) (string, error) {
	flags, wrap, err := c.flagsAndWrap(workspaceID, launch, promptFiles)
	if err != nil {
		return "", err
	}
	launcher, err := wrappedLauncher(c.Launcher, wrap)
	if err != nil {
		return "", err
	}
	return c.withEditor(launcher + " " + flags), nil
}

// ResumeCommand is the line that resumes a conversation; the id must be a UUID.
func (c LaunchConfig) ResumeCommand(sessionID, workspaceID string, launch *agents.Launch) (string, error) {
	if !agents.IsUUID(sessionID) {
		return "", errors.New("Invalid session ID format: " + sessionID)
	}
	flags, wrap, err := c.flagsAndWrap(workspaceID, launch, nil)
	if err != nil {
		return "", err
	}
	launcher, err := wrappedLauncher(c.Launcher, wrap)
	if err != nil {
		return "", err
	}
	return c.withEditor(launcher + " --resume " + sessionID + " " + flags), nil
}
