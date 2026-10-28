// Command agent is the machine agent: it serves one Unix account's transcripts, runtime state and loopback services
// to the cluster over a single WebSocket link, or, once registered, to a harnessly hub (docs/hub/machine.md).
//
// With no arguments it runs. `register`, `status`, `off`, `on` and `policy` are the short commands around it.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/ly-solutions/harnessly/internal/agent"
)

func main() {
	if code, handled := agent.RunCLI(os.Args[1:], os.Stdout, os.Stderr); handled {
		os.Exit(code)
	}
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
	// SIGHUP reads the local policy file again.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			agent.Log("info", "SIGHUP: reading the policy again")
			a.ReloadPolicy()
		}
	}()
	go func() {
		<-ctx.Done()
		agent.Log("info", "shutting down")
	}()
	if err := a.Run(ctx); err != nil {
		agent.Log("error", "agent failed to start", agent.F{K: "error", V: err.Error()})
		os.Exit(1)
	}
}
