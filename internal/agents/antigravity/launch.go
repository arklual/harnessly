// Package antigravity implements the official Antigravity CLI, not a Claude compatibility wrapper.
package antigravity

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/wire"
)

const (
	ProviderID  = agents.Antigravity
	DisplayName = "Antigravity"
	PanelType   = agents.AntigravityPanel
	Binary      = "agy"
)

var ErrUnsupported = errors.New("Antigravity launch option is not supported")

// LaunchConfig contains only native CLI options. Settings is JSON or a settings file,
// never hook configuration. Hooks live in .agents/hooks.json or ~/.gemini/config/hooks.json.
type LaunchConfig struct {
	Launcher string
	Settings string
	Mode     string
	Sandbox  bool
	Env      []agents.EnvVar
	// EnvPrefix is the trusted deployment's agents.EnvSourcePrefix output.
	EnvPrefix string
}

func LaunchFlags(l *agents.Launch) ([]string, error) {
	if err := agents.CheckLaunch(l); err != nil {
		return nil, err
	}
	if l == nil {
		return nil, nil
	}
	if l.Fast || l.Ultracode || l.Profile != "" || len(l.Apps) != 0 || l.Input != "" {
		return nil, ErrUnsupported
	}
	var flags []string
	if l.Model != nil {
		flags = append(flags, "--model "+agents.ShellQuote(*l.Model))
	}
	if l.Effort != nil {
		switch *l.Effort {
		case "low", "medium", "high", "xhigh", "max":
		default:
			return nil, agents.ErrLaunchEffort
		}
		flags = append(flags, "--effort "+agents.ShellQuote(*l.Effort))
	}
	return flags, nil
}

func (c LaunchConfig) Flags(_ string, l *agents.Launch, promptFiles []string) (string, error) {
	if len(promptFiles) != 0 {
		return "", ErrUnsupported
	}
	flags, err := LaunchFlags(l)
	if err != nil {
		return "", err
	}
	mode := c.Mode
	if l != nil {
		switch l.Permission {
		case "":
		case wire.PermissionAsk:
			mode = "" // Ask uses the CLI's own default, not an unsupported --mode default.
		case wire.PermissionPlan:
			mode = "plan"
		default:
			return "", ErrUnsupported
		}
	}
	if mode != "" {
		switch mode {
		case "accept-edits", "auto", "plan":
		default:
			return "", agents.ErrLaunchMode
		}
		flags = append(flags, "--mode "+agents.ShellQuote(mode))
	}
	if c.Settings != "" {
		flags = append(flags, "--settings "+agents.ShellQuote(c.Settings))
	}
	if c.Sandbox {
		flags = append(flags, "--sandbox")
	}
	return strings.Join(flags, " "), nil
}

func (c LaunchConfig) LaunchCommand(workspaceID string, l *agents.Launch, promptFiles []string) (string, error) {
	flags, err := c.Flags(workspaceID, l, promptFiles)
	if err != nil {
		return "", err
	}
	command := Binary
	if c.Launcher != "" {
		if !filepath.IsAbs(c.Launcher) || strings.ContainsFunc(c.Launcher, func(r rune) bool { return r < 32 || r == 127 }) {
			return "", errors.New("Antigravity launcher must be an absolute path without control characters")
		}
		command = agents.ShellQuote(c.Launcher)
	}
	env, err := agents.FormatStartEnv(c.Env, "HARNESSLY_")
	if err != nil {
		return "", err
	}
	if flags != "" {
		command += " " + flags
	}
	return c.EnvPrefix + env + command, nil
}

func (c LaunchConfig) ResumeCommand(id, workspaceID string, l *agents.Launch) (string, error) {
	if !agents.IsUUID(id) {
		return "", errors.New("Invalid Antigravity conversation UUID")
	}
	command, err := c.LaunchCommand(workspaceID, l, nil)
	if err != nil {
		return "", err
	}
	return command + " --conversation " + agents.ShellQuote(id), nil
}

// ShellCommand wraps a native launch for the pane's interactive shell.
func (c LaunchConfig) ShellCommand(workspaceID string, l *agents.Launch, prompt, promptFile string) (string, error) {
	line, err := c.LaunchCommand(workspaceID, l, nil)
	if err != nil {
		return "", err
	}
	return agents.ForAnyShell(agents.WithInitialPrompt(ProviderID, line, prompt, promptFile)), nil
}

// NewLine is a fresh launch safe for any supported pane shell.
func (c LaunchConfig) NewLine(workspaceID string, l *agents.Launch) (string, error) {
	line, err := c.LaunchCommand(workspaceID, l, nil)
	if err != nil {
		return "", err
	}
	return agents.ForAnyShell(line), nil
}

// ResumeLine resumes only the explicitly named native conversation.
func (c LaunchConfig) ResumeLine(id, workspaceID string, l *agents.Launch) (string, error) {
	line, err := c.ResumeCommand(id, workspaceID, l)
	if err != nil {
		return "", err
	}
	return agents.ForAnyShell(line), nil
}

// RunsWithHooks is deliberately false until the deployment installs and verifies native hooks.
func (LaunchConfig) RunsWithHooks() bool { return false }
