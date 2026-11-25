package hlcli

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/antigravity"
	"github.com/ly-solutions/harnessly/internal/agents/antigravity/acpbridge"
)

// antigravityACP runs a native Antigravity chat in this terminal over the CLI's Agent Client Protocol: the runtime
// starts it as the chat's agent and talks to it on --socket.
func (c *cli) antigravityACP(args []string) error {
	cfg := acpbridge.Config{Agy: "agy"}
	for i := 0; i < len(args); i++ {
		name, value, inline := strings.Cut(args[i], "=")
		if !inline {
			if i+1 >= len(args) {
				return usageErr("antigravity-acp: %s needs a value", name)
			}
			i++
			value = args[i]
		}
		switch name {
		case "--agy":
			cfg.Agy = value
		case "--socket":
			cfg.Socket = value
		case "--conversation":
			if !agents.IsUUID(value) {
				return usageErr("antigravity-acp: --conversation takes a session UUID")
			}
			cfg.Conversation = value
		case "--model":
			cfg.Model = value
		case "--effort":
			cfg.Effort = value
		case "--mcp":
			// The harnessly tools reach the session as an MCP server of its own, unless the official plugin that
			// carries them is installed (two servers of one name would clash).
			if value == "harnessly" && !antigravityPluginInstalled(c.env.Home) {
				if bin, err := os.Executable(); err == nil {
					cfg.MCP = append(cfg.MCP, acpbridge.MCPServer{Name: "harnessly", Command: bin, Args: []string{"antigravity-mcp"}, Env: harnesslyEnv()})
				}
			}
		case "--prompt-interactive", "-i":
			// The chat's first message, as the launch line gives it to the CLI.
			cfg.First = value
		case "--mode":
			switch value {
			case acpbridge.ModeAsk, acpbridge.ModePlan, acpbridge.ModeBypass:
			default:
				return usageErr("antigravity-acp: --mode is ask, plan or bypass")
			}
			cfg.Mode = value
		default:
			return usageErr("antigravity-acp: unknown option %s", name)
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg.Cwd = cwd
	home := c.env.Home
	cfg.Transcript = func(id string) string { return antigravity.TranscriptPath(home, id) }
	cfg.In, cfg.Out = os.Stdin, os.Stdout
	if restore, err := acpbridge.MakeRaw(int(os.Stdin.Fd())); err == nil {
		defer restore()
		cfg.Raw = true
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	if file := c.env.Getenv("HARNESSLY_CLI_TOKEN_FILE"); file != "" && filepath.Base(file) == ".credentials.json" {
		// The launch's token lives as long as the chat's agent: the bridge took the place of the shell that removed it.
		defer os.Remove(file)
	}
	err = acpbridge.Run(ctx, cfg)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// antigravityPluginInstalled: the official CLI has the harnessly-native plugin, whose MCP server gives the tools.
func antigravityPluginInstalled(home string) bool {
	st, err := os.Stat(filepath.Join(home, ".gemini", "config", "plugins", "harnessly-native"))
	return err == nil && st.IsDir()
}

// harnesslyEnv is what the MCP server of a managed launch reads: the launch's marks and token file, and the terminal.
func harnesslyEnv() []acpbridge.EnvVar {
	var out []acpbridge.EnvVar
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "HARNESSLY_") || k == "TMUX" || k == "TMUX_PANE" || k == "HOME" || k == "PATH" {
			out = append(out, acpbridge.EnvVar{Name: k, Value: v})
		}
	}
	return out
}
