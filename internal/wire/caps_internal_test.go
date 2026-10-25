package wire

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestCapsAreReadFromTheAddress(t *testing.T) {
	for addr, want := range map[string]bool{
		"/api/v2/socket?machine=local&caps=delegation":        true,
		"/api/v2/socket?caps=voice,delegation":                true,
		"/api/v2/socket?caps=voice&caps=delegation&machine=x": true,
		"/api/v2/runtime-settings?caps=delegationx":           false,
		"/api/v2/socket?machine=local":                        false,
	} {
		if got := CapsOf(httptest.NewRequest("GET", addr, nil)).Delegation; got != want {
			t.Errorf("%s: %v", addr, got)
		}
	}
}

func TestAnOlderPageGetsNoDelegation(t *testing.T) {
	parent := "tab-parent01"
	kid := Conversation{ID: "tab-kid00001", ParentID: &parent}
	e, changed := eventFor(ConversationUpsertEv{Conv: kid}, Caps{})
	if !changed || e.(ConversationUpsertEv).Conv.ParentID != nil {
		t.Fatalf("older form: %v %#v", changed, e)
	}
	if _, changed := eventFor(ConversationUpsertEv{Conv: kid}, AllCaps); changed {
		t.Fatal("a page with the cap gets the event as it is")
	}
	if _, changed := eventFor(ConversationUpsertEv{Conv: Conversation{ID: "tab-plain0001"}}, Caps{}); changed {
		t.Fatal("a chat without a parent is the same for every page")
	}

	snap := SnapshotFrame{Ch: ChannelConvs, Convs: &ConvsSnapshot{Conversations: []Conversation{kid}}}
	if got := snapshotFor(snap, Caps{}); got.Convs.Conversations[0].ParentID != nil {
		t.Fatal("snapshot keeps the parent for an older page")
	}
	if snap.Convs.Conversations[0].ParentID == nil {
		t.Fatal("the snapshot itself is never changed")
	}

	set := RuntimeSettings{IdleStop: IdleStopOff, Delegation: &DelegationSettings{MaxChildren: 3}}
	if settingsFor(set, Caps{}).Delegation != nil || settingsFor(set, AllCaps).Delegation == nil {
		t.Fatal("settings: delegation only with the cap")
	}

	// The ring hands each page its form.
	it := ringItem{seq: 1, at: time.Now(), frame: []byte("new"), older: map[Caps][]byte{{}: []byte("old")}}
	if string(it.forCaps(AllCaps)) != "new" || string(it.forCaps(Caps{})) != "old" {
		t.Fatal("ring item forms")
	}
}

func TestASnapshotKeepsWorkflowsFromAPageThatDoesNotReadThem(t *testing.T) {
	wait := &BgWait{Agents: 1, Workflows: 2}
	conv := &ConvSnapshot{Background: []BackgroundTask{
		{ID: "a1", Kind: BgSubagent},
		{ID: "w1", Kind: BgWorkflow, Workflow: &WorkflowRun{Name: "checkout-review"}},
	}}
	convs := &ConvsSnapshot{Conversations: []Conversation{
		{ID: "tab-a", BgAgents: 3, BgWait: wait},
		{ID: "tab-b", BgAgents: 2, BgWait: &BgWait{Workflows: 2}},
	}}
	snap := SnapshotFrame{Conv: conv, Convs: convs}

	old := snapshotFor(snap, Caps{Delegation: true})
	if len(old.Conv.Background) != 1 || old.Conv.Background[0].ID != "a1" {
		t.Fatalf("background %+v", old.Conv.Background)
	}
	if a, b := old.Convs.Conversations[0], old.Convs.Conversations[1]; a.BgAgents != 1 || a.BgWait.Workflows != 0 ||
		b.BgAgents != 0 || b.BgWait != nil {
		t.Fatalf("conversations %+v %+v", a, b)
	}
	if len(conv.Background) != 2 || wait.Workflows != 2 || convs.Conversations[0].BgAgents != 3 {
		t.Fatal("the snapshot itself changed")
	}
	if got := snapshotFor(snap, AllCaps); got.Conv != conv || got.Convs != convs {
		t.Fatal("a page that reads workflows gets the snapshot as it is")
	}
}

func TestTheWorkflowsCapIsReadFromTheAddress(t *testing.T) {
	for addr, want := range map[string]bool{
		"/api/v2/socket?machine=local&caps=delegation,workflows": true,
		"/api/v2/socket?caps=delegation":                         false,
		"/api/v2/socket?caps=workflowsx":                         false,
	} {
		if got := CapsOf(httptest.NewRequest("GET", addr, nil)).Workflows; got != want {
			t.Errorf("%s: %v", addr, got)
		}
	}
}
