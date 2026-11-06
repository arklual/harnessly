package hlcli

import (
	"context"
	"flag"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/supervise"
)

// supervise keeps the runtime (and the agent) of this machine running where there is no service manager.
func (c *cli) supervise(args []string) error {
	fs := flag.NewFlagSet("supervise", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	runtime := fs.String("runtime", "harnessly-runtime", "the runtime's program")
	agent := fs.String("agent", "", "the agent's program; empty runs the runtime alone")
	statusFile := fs.String("status-file", "", "where to note restarts and the last end of each (default: supervisor.json in the data folder)")
	base := fs.Duration("backoff-base", time.Second, "the wait after the first quick failure")
	max := fs.Duration("backoff-max", 30*time.Second, "the longest wait between starts")
	if err := fs.Parse(args); err != nil {
		return usageErr("supervise: %v", err)
	}
	if fs.NArg() > 0 {
		return usageErr("supervise takes only flags")
	}
	children := []supervise.Child{{Name: "runtime", Path: *runtime}}
	if *agent != "" {
		children = append(children, supervise.Child{Name: "agent", Path: *agent})
	}
	status := *statusFile
	if status == "" {
		data := c.env.Getenv("AGENT_DATA_DIR")
		if data == "" && c.env.Home != "" {
			data = filepath.Join(c.env.Home, ".harnessly")
		}
		if data != "" {
			status = filepath.Join(data, "supervisor.json")
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	env := os.Environ()
	if c.env.Environ != nil {
		env = c.env.Environ()
	}
	return supervise.Run(ctx, supervise.Options{
		Children: children, Stdout: c.env.Stdout, Stderr: c.env.Stderr, Env: env,
		BackoffBase: *base, BackoffMax: *max, StatusFile: status,
	})
}
