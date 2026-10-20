// Command agent is the machine agent: it serves one Unix account's transcripts, runtime state and loopback services
// to the cluster over a single WebSocket link.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/ly-solutions/harnessly/internal/agent"
)

func main() {
	cfg, err := agent.LoadConfig()
	if err != nil {
		agent.Log("error", "agent failed to start", agent.F{K: "error", V: err.Error()})
		os.Exit(1)
	}
	a, err := agent.New(cfg)
	if err != nil {
		agent.Log("error", "agent failed to start", agent.F{K: "error", V: err.Error()})
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go func() {
		<-ctx.Done()
		agent.Log("info", "shutting down")
	}()
	if err := a.Run(ctx); err != nil {
		agent.Log("error", "agent failed to start", agent.F{K: "error", V: err.Error()})
		os.Exit(1)
	}
}
