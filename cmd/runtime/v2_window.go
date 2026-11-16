package main

import (
	"github.com/ly-solutions/harnessly/internal/agentcfg"
	"github.com/ly-solutions/harnessly/internal/history"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/transcript"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// v2Window is the window an agent reports for its session: Claude's statusline keeps context_window_size in the
// session's stats, Codex's transcript names model_context_window. (0, 0) where the agent has said nothing yet.
func v2Window(stats *history.Stats) func(p wire.Provider, sessionID, transcriptPath string) (used, limit int64) {
	return func(p wire.Provider, sessionID, transcriptPath string) (int64, int64) {
		var st *jsvalue.Object
		switch {
		case p == wire.ProviderClaude && sessionID != "":
			st = stats.Read(sessionID)
		case p == wire.ProviderCodex && transcriptPath != "":
			st = transcript.CodexTimelineStats(transcriptPath)
		}
		if st == nil {
			return 0, 0
		}
		return windowNum(st, "currentContextTokens"), windowNum(st, "contextWindowSize")
	}
}

// v2Cost is what a Claude session cost by its statusline (cost.total_cost_usd), the figure Claude Code itself shows;
// 0 for Codex, whose transcript has no price.
func v2Cost(stats *history.Stats) func(p wire.Provider, sessionID string) float64 {
	return func(p wire.Provider, sessionID string) float64 {
		if p != wire.ProviderClaude || sessionID == "" {
			return 0
		}
		st := stats.Read(sessionID)
		if st == nil {
			return 0
		}
		v, _ := st.Get("cost")
		if f, ok := v.(float64); ok && f > 0 {
			return f
		}
		return 0
	}
}

func windowNum(o *jsvalue.Object, k string) int64 {
	v, _ := o.Get(k)
	if f, ok := v.(float64); ok && f > 0 {
		return int64(f)
	}
	return 0
}

// v2Compact is what a chat's agent is set to compact at, by the CLI's settings files of this machine (and the chat's
// project for Claude); the files are read again only when they change.
func v2Compact(files *agentcfg.Store) func(p wire.Provider, cwd, model string) (window, at int64) {
	return func(p wire.Provider, cwd, model string) (int64, int64) {
		c := files.CompactOf(p, cwd, model)
		return c.Window, c.At
	}
}
