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

func TestTheProfilesCapIsReadFromTheAddress(t *testing.T) {
	for addr, want := range map[string]bool{
		"/api/v2/socket?machine=local&caps=delegation,workflows,profiles": true,
		"/api/v2/machines?caps=profiles":                                  true,
		"/api/v2/socket?caps=delegation,workflows":                        false,
		"/api/v2/socket?caps=profilesx":                                   false,
	} {
		if got := CapsOf(httptest.NewRequest("GET", addr, nil)).Profiles; got != want {
			t.Errorf("%s: %v", addr, got)
		}
	}
}

func TestASnapshotKeepsProfilesFromAPageThatDoesNotReadThem(t *testing.T) {
	prof := &Model{ID: "kimi-k3.1-pro", Profile: "kimi"}
	from := &Origin{K: OriginHandoff, FromModel: &Model{ID: "grok-build-0.1", Profile: "xai"}}
	log := []ModelSwitch{{From: &Model{ID: "a", Profile: "kimi"}, To: Model{ID: "b", Profile: "kimi"}}}
	convs := &ConvsSnapshot{Conversations: []Conversation{{ID: "tab-a", Model: prof, Origin: from, ModelLog: log}, {ID: "tab-b", Model: &Model{ID: "opus"}}}}
	machine := &MachineSnapshot{Machine: Machine{Agents: []MachineAgent{{Provider: ProviderClaude, Profiles: []ProfileOffer{{ID: "kimi"}}}}}}
	snap := SnapshotFrame{Ch: ChannelConvs, Convs: convs, Machine: machine}

	old := snapshotFor(snap, Caps{Delegation: true, Workflows: true})
	a := old.Convs.Conversations[0]
	if a.Model.Profile != "" || a.Origin.FromModel.Profile != "" || a.ModelLog[0].To.Profile != "" || a.ModelLog[0].From.Profile != "" ||
		a.Model.ID != prof.ID || old.Machine.Machine.Agents[0].Profiles != nil {
		t.Fatalf("snapshot for an older page: %+v %+v", a, old.Machine.Machine.Agents)
	}
	if prof.Profile != "kimi" || from.FromModel.Profile != "xai" || log[0].To.Profile != "kimi" || machine.Machine.Agents[0].Profiles == nil {
		t.Fatal("the snapshot itself changed")
	}
	if got := snapshotFor(snap, AllCaps); got.Convs != convs || got.Machine != machine {
		t.Fatal("a page that reads profiles gets the snapshot as it is")
	}
	if _, changed := eventFor(ConversationUpsertEv{Conv: Conversation{ID: "tab-b", Model: &Model{ID: "opus"}}}, Caps{Delegation: true, Workflows: true}); changed {
		t.Fatal("a chat without a profile is the same for every page")
	}
	if got := MachineFor(machine.Machine, Caps{}); got.Agents[0].Profiles != nil || machine.Machine.Agents[0].Profiles == nil {
		t.Fatal("MachineFor")
	}
}

// A stalled message is failed{unconfirmed} for a page that does not read a stall, with the same rev, and its queue does
// not list it; a page with the cap gets it as it is. The status answer is cut the same way.
func TestAnOlderPageGetsAStallAsAnUnconfirmedFailure(t *testing.T) {
	stalled := MessageState{K: MsgQueued, Position: 1, Reason: QueuedStalled, At: 1700}
	older := Caps{Delegation: true, Workflows: true, Profiles: true}
	e, changed := eventFor(PromptStateEv{ClientID: "m1", State: stalled, Rev: 4}, older)
	ps, _ := e.(PromptStateEv)
	if !changed || ps.Rev != 4 || ps.State.K != MsgFailed || ps.State.Code != MsgFailedUnconfirmed || !ps.State.Retryable || ps.State.At != 1700 {
		t.Fatalf("prompt_state: %v %#v", changed, e)
	}
	if _, changed := eventFor(PromptStateEv{ClientID: "m1", State: stalled, Rev: 4}, AllCaps); changed {
		t.Fatal("a page with the cap gets the stall")
	}
	msg := Message{ClientID: "m1", ConversationID: "tab-a0000001", State: stalled, Rev: 4}
	if e, changed := eventFor(MessageUpsertEv{Message: msg}, older); !changed || e.(MessageUpsertEv).Message.State.K != MsgFailed {
		t.Fatalf("message_upsert: %#v", e)
	}
	q := Queue{ConversationID: "tab-a0000001", Items: []string{"m1", "m2"}, Rev: 3, Stalled: []string{"m1"}}
	e, changed = eventFor(QueueChangedEv{Queue: q}, older)
	if items := e.(QueueChangedEv).Queue.Items; !changed || len(items) != 1 || items[0] != "m2" || len(q.Items) != 2 {
		t.Fatalf("queue: %v %v", changed, items)
	}
	if _, changed := eventFor(MessageUpsertEv{Message: Message{State: MessageState{K: MsgQueued, Reason: QueuedBehind}}}, Caps{}); changed {
		t.Fatal("a message that is not stalled is the same for every page")
	}

	snap := SnapshotFrame{Ch: "conv:tab-a0000001", Conv: &ConvSnapshot{Queue: q, Messages: []Message{msg}}}
	got := snapshotFor(snap, older)
	if got.Conv.Messages[0].State.K != MsgFailed || len(got.Conv.Queue.Items) != 1 {
		t.Fatalf("snapshot: %#v", got.Conv)
	}
	if snap.Conv.Messages[0].State.K != MsgQueued || len(snap.Conv.Queue.Items) != 2 {
		t.Fatal("the snapshot itself is never changed")
	}
	if got := snapshotFor(snap, AllCaps); got.Conv != snap.Conv {
		t.Fatal("a page with the cap gets the snapshot as it is")
	}
	if !CapsOf(httptest.NewRequest("GET", "/api/v2/socket?caps=delegation,workflows,profiles,msgstatus", nil)).MsgStatus {
		t.Fatal("the cap is read")
	}
}
