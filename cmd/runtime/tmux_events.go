package main

import (
	"context"
	"os"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

// tmuxEventsEnv set to 0 keeps the runtime asking tmux for its panes with a command a second, as before the servers
// were followed by their events; anything else, or nothing, follows the events.
const tmuxEventsEnv = "RUNTIME_TMUX_EVENTS"

func tmuxEventsOn(getenv func(string) string) bool { return getenv(tmuxEventsEnv) != "0" }

// watchTmux starts following the tmux servers' events until ctx ends; nil (which Stop takes) when switched off.
func watchTmux(ctx context.Context, tm *tmux.Tmux) *tmux.Watcher {
	if !tmuxEventsOn(os.Getenv) {
		agent.Log("info", "tmux: panes are polled ("+tmuxEventsEnv+"=0)")
		return nil
	}
	return tmux.Watch(ctx, tm, tmux.WatchOptions{Log: func(level, msg string, kv ...any) {
		fs := make([]agent.F, 0, len(kv)/2)
		for i := 0; i+1 < len(kv); i += 2 {
			if k, ok := kv[i].(string); ok {
				fs = append(fs, agent.F{K: k, V: kv[i+1]})
			}
		}
		agent.Log(level, msg, fs...)
	}})
}
