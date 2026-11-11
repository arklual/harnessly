package main

import (
	"reflect"
	"testing"

	"github.com/ly-solutions/harnessly/internal/sleep"
	"github.com/ly-solutions/harnessly/internal/status"
)

// The conversations own the chats' sessions unless the variable says 0.
func TestSessionOwnerSwitch(t *testing.T) {
	for value, want := range map[string]bool{"": true, "1": true, "on": true, "0": false} {
		if got := sessionOwnerOn(func(string) string { return value }); got != want {
			t.Errorf("%s=%q: owner on %v, want %v", sessionOwnerEnv, value, got, want)
		}
	}
	// With no v2 running nothing is a chat's.
	if (&core{}).chatOwns("hs-ws-a-pane-1-tab-x") {
		t.Errorf("a core without v2 owns a session")
	}
}

type fakeSleepKeeper struct{ tabs []status.AgentSource }

func (k fakeSleepKeeper) AgentSources() []status.AgentSource { return k.tabs }
func (k fakeSleepKeeper) Tab(id string) (status.TabInfo, bool) {
	for _, t := range k.tabs {
		if t.TabID == id {
			return status.TabInfo{CliState: t.CliState, Provider: t.Provider, PanelType: t.PanelType, TmuxSession: t.TmuxSession}, true
		}
	}
	return status.TabInfo{}, false
}

// The tabs' idle sleep never sees a tab whose session is a chat's: neither as a candidate nor when it asks for the
// tab again right before stopping its agent. With no owner (the switch off) it sees every agent tab, as before.
func TestSleepTabsLeaveChatsToTheirOwner(t *testing.T) {
	keeper := fakeSleepKeeper{tabs: []status.AgentSource{
		{TabID: "chat", TmuxSession: "hs-ws-pane-chat", Provider: "claude", PanelType: "claude-code", CliState: "idle"},
		{TabID: "tab", TmuxSession: "hs-ws-pane-tab", Provider: "claude", PanelType: "claude-code", CliState: "idle"},
	}}
	ids := func(tabs []sleep.Tab) []string {
		var out []string
		for _, t := range tabs {
			out = append(out, t.TabID)
		}
		return out
	}
	owned := sleepTabs{m: func() sleepKeeper { return keeper }, owned: func(s string) bool { return s == "hs-ws-pane-chat" }}
	if got := ids(owned.AgentTabs()); !reflect.DeepEqual(got, []string{"tab"}) {
		t.Fatalf("the sleep sees %v, want the tab that is no chat", got)
	}
	if _, ok := owned.Current("chat"); ok {
		t.Fatalf("the sleep was given a chat's tab to stop")
	}
	if cur, ok := owned.Current("tab"); !ok || cur.TmuxSession != "hs-ws-pane-tab" {
		t.Fatalf("the tab that is no chat: %+v %v", cur, ok)
	}
	all := sleepTabs{m: func() sleepKeeper { return keeper }}
	if got := ids(all.AgentTabs()); !reflect.DeepEqual(got, []string{"chat", "tab"}) {
		t.Fatalf("without an owner the sleep sees %v", got)
	}
	if _, ok := all.Current("chat"); !ok {
		t.Fatalf("without an owner the chat's tab is not found")
	}
	// No keeper yet: nothing to sleep.
	none := sleepTabs{m: func() sleepKeeper { return nil }}
	if got := none.AgentTabs(); len(got) != 0 {
		t.Fatalf("without a keeper: %v", got)
	}
}
