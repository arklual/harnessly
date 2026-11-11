package hlcli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/ly-solutions/harnessly/internal/appmcp"
)

// appMCP is `harnessly app-mcp`: the harnessly-app MCP server over stdio, started by a chat's MCP config
// (internal/apps launch.go). It passes the agent's JSON-RPC to the runtime and waits out the runtime's restarts
// It ends when the agent closes its input.
func (c *cli) appMCP(args []string) error {
	if len(args) != 0 {
		return usageErr("%s takes no arguments: the chat's MCP config starts it", appmcp.Command)
	}
	in := c.env.Stdin
	if in == nil {
		in = os.Stdin
	}
	p, err := appmcp.FromEnv(c.env.Getenv, in, c.env.Stdout)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	return p.Run(ctx)
}
