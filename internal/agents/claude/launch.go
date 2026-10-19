package claude

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"harnessly/internal/agents"
	"harnessly/internal/jscompat"
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
}

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

// Flags are the launch flags in order: settings, MCP config, system-prompt files, permissions, model and effort.
func (c LaunchConfig) Flags(workspaceID string, launch *agents.Launch, promptFiles []string) (string, error) {
	parts := []string{"--settings " + c.HooksPath, "--mcp-config " + c.MCPConfigPath}
	prompts, err := c.promptFileFlags(workspaceID, promptFiles)
	if err != nil {
		return "", err
	}
	parts = append(parts, prompts...)
	if c.SkipPermissions != nil && c.SkipPermissions() {
		parts = append(parts, "--dangerously-skip-permissions")
	}
	flags, err := LaunchFlags(launch)
	if err != nil {
		return "", err
	}
	parts = append(parts, flags...)
	return strings.Join(parts, " "), nil
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
	flags, err := c.Flags(workspaceID, launch, promptFiles)
	if err != nil {
		return "", err
	}
	launcher, err := LauncherCommand(c.Launcher)
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
	flags, err := c.Flags(workspaceID, launch, nil)
	if err != nil {
		return "", err
	}
	launcher, err := LauncherCommand(c.Launcher)
	if err != nil {
		return "", err
	}
	return c.withEditor(launcher + " --resume " + sessionID + " " + flags), nil
}
