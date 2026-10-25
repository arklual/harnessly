package main

import (
	"errors"
	"testing"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A conversation names the Legacy tab of its tmux session, with its transcript once the agent wrote one; one taken
// over from Legacy under another prefix is its own tab; without a session it has none yet.
func TestTheLegacyTabOfAConversation(t *testing.T) {
	recs := map[string]wire.Conversation{
		"tab-v2conv": {ID: "tab-v2conv", Cwd: "/w", Session: "hl-ws-a-pane-b-tab-c", TranscriptPath: "/t.jsonl", AgentSessionID: "s1"},
		"tab-fresh1": {ID: "tab-fresh1", Cwd: "/w"},
		"tab-olden1": {ID: "tab-olden1", Cwd: "/w", Session: "pt-ws-x-pane-y-tab-olden1"},
	}
	record := func(id string) (wire.Conversation, bool) { r, ok := recs[id]; return r, ok }
	parse := func(name string) (string, string, string, bool) {
		if name != "hl-ws-a-pane-b-tab-c" {
			return "", "", "", false
		}
		return "ws-a", "pane-b", "tab-c", true
	}
	got, err := legacyTabOf(record, parse, "tab-v2conv")
	if err != nil || got.TabID != "tab-c" || got.WorkspaceID != "ws-a" || got.SessionName != "hl-ws-a-pane-b-tab-c" ||
		got.JsonlPath == nil || *got.JsonlPath != "/t.jsonl" || got.AgentSessionID == nil || got.Cwd != "/w" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	if old, err := legacyTabOf(record, parse, "tab-olden1"); err != nil || old.TabID != "tab-olden1" || old.WorkspaceID != "" {
		t.Fatalf("taken over: %+v %v", old, err)
	}
	for _, id := range []string{"tab-fresh1", "tab-absent"} {
		if _, err := legacyTabOf(record, parse, id); !errors.Is(err, convo.ErrNotFound) {
			t.Fatalf("%s: %v", id, err)
		}
	}
}
