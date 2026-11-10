package convo

import (
	"context"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// HRN-842: a chat made for a check is marked a test chat, from its creation or by conversation.test; the mark rides the
// record, is taken off by the same command, survives a restart, and a chat not found is refused.
// testOf is the Test mark of a chat as the list of conversations has it.
func testOf(t *testing.T, r *rig, id string) bool {
	t.Helper()
	snap, err := r.rt.Conversations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range snap.Conversations {
		if c.ID == id {
			return c.Test
		}
	}
	t.Fatalf("no %s", id)
	return false
}

func TestATestChatIsMarkedAndKept(t *testing.T) {
	r := newRig(t)
	made := r.do(wire.ConversationCreateCmd{OpID: "c", Cwd: "/w", Provider: wire.ProviderClaude, Test: true})
	if !testOf(t, r, *made.ID) {
		t.Fatal("made as a test chat")
	}
	plain := r.do(wire.ConversationCreateCmd{OpID: "d", Cwd: "/w", Provider: wire.ProviderClaude})
	if testOf(t, r, *plain.ID) {
		t.Fatal("an ordinary chat is not a test chat")
	}

	if res := r.do(wire.ConversationTestCmd{OpID: "t1", ID: *plain.ID, Test: true}); res.R != wire.ResultAccepted {
		t.Fatalf("mark: %+v", res)
	}
	if !testOf(t, r, *plain.ID) {
		t.Fatal("the mark is not on the record")
	}
	if res := r.do(wire.ConversationTestCmd{OpID: "t2", ID: *made.ID, Test: false}); res.R != wire.ResultAccepted {
		t.Fatalf("unmark: %+v", res)
	}
	if testOf(t, r, *made.ID) {
		t.Fatal("the mark was not taken off")
	}
	if res := r.do(wire.ConversationTestCmd{OpID: "t3", ID: "tab-nobody", Test: true}); res.Code != wire.RefuseNotFound {
		t.Fatalf("a chat that is not there: %+v", res)
	}

	r.rt.Stop()
	r.restart()
	if !testOf(t, r, *plain.ID) {
		t.Fatal("after a restart")
	}
	if testOf(t, r, *made.ID) {
		t.Fatal("after a restart the unmarked chat is a test chat again")
	}
}

func TestTheTestMarkValidates(t *testing.T) {
	if err := (wire.ConversationTestCmd{OpID: "x", ID: "tab-a", Test: true}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (wire.ConversationTestCmd{ID: "tab-a"}).Validate(); err == nil {
		t.Fatal("a command without an opId")
	}
}
