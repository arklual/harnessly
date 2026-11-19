package antigravity

import (
	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/transcript"
)

// ReadSnapshot uses native normalized entries without treating a step's DONE as
// turn completion. Only validated hooks may make the session idle.
func ReadSnapshot(path string) (agents.Snapshot, bool) {
	if SessionIDFromPath(path) == "" {
		return agents.Snapshot{}, false
	}
	entries := transcript.ReadAntigravityTail(path, 100).Entries
	if len(entries) == 0 {
		return agents.Snapshot{}, false
	}
	snap := agents.Snapshot{}
	for _, e := range entries {
		switch e.String("type") {
		case "user-message":
			snap.Reset = true
			snap.LastAssistantSnippet = nil
			snap.CurrentAction = nil
		case "assistant-message":
			text := e.String("markdown")
			if text != "" {
				snap.LastAssistantSnippet = &text
				snap.CurrentAction = agents.Text(text)
				snap.Reset = false
			}
		case "tool-call":
			snap.CurrentAction = agents.Tool(e.String("toolName"), e.String("summary"))
		}
	}
	return snap, true
}

// ReadHistoryStats reuses the native normalizer and does not infer turn duration
// or touched files from ambiguous native result entries.
func ReadHistoryStats(path string) agents.HistoryStats {
	stats := agents.EmptyHistoryStats()
	if SessionIDFromPath(path) == "" {
		return stats
	}
	for _, e := range transcript.ReadAntigravityTail(path, 500).Entries {
		switch e.String("type") {
		case "user-message":
			text := e.String("text")
			stats.LastUserText = &text
		case "assistant-message":
			text := e.String("markdown")
			if text != "" {
				stats.LastAssistantText = &text
			}
		case "tool-call":
			stats.ToolUsage.Add(e.String("toolName"))
		}
	}
	return stats
}
