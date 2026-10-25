package convo

import (
	"errors"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

type fakeCloser struct {
	closed []string
	err    error
}

func (f *fakeCloser) Close(c wire.Conversation) error {
	f.closed = append(f.closed, c.Session)
	return f.err
}

func TestDeletingAConversationClosesItsPaneAndTab(t *testing.T) {
	r := newRig(t)
	cl := &fakeCloser{}
	r.rt.d.Closer = cl
	r.open()
	r.settle(wire.StateReady)

	if res := r.do(wire.ConversationDeleteCmd{OpID: "del", ID: r.id}); res.R != wire.ResultAccepted {
		t.Fatalf("%+v", res)
	}
	if len(cl.closed) != 1 || cl.closed[0] != "s1" {
		t.Fatalf("closed %v", cl.closed)
	}
	// Restored within its days, the conversation is one whose tab is gone.
	if res := r.do(wire.ConversationRestoreCmd{OpID: "res", ID: r.id}); res.R != wire.ResultAccepted {
		t.Fatalf("%+v", res)
	}
	if st := r.state(); st.K != wire.StateGone || st.Reason != string(wire.GoneTabClosed) {
		t.Fatalf("%+v", st)
	}
}

func TestAPaneThatCouldNotBeClosedLeavesTheStateAsItWas(t *testing.T) {
	r := newRig(t)
	r.rt.d.Closer = &fakeCloser{err: errors.New("tmux is busy")}
	r.open()
	r.settle(wire.StateReady)
	r.do(wire.ConversationDeleteCmd{OpID: "del", ID: r.id})
	r.do(wire.ConversationRestoreCmd{OpID: "res", ID: r.id})
	if st := r.state(); st.K != wire.StateReady {
		t.Fatalf("%+v", st)
	}
}

func TestAWorkingConversationIsClosedOnlyWhenForced(t *testing.T) {
	r := newRig(t)
	cl := &fakeCloser{}
	r.rt.d.Closer = cl
	r.open()
	r.settle(wire.StateReady)
	r.agentTakes()
	r.send("m1", "work", wire.ModeQueue)
	r.settle(wire.StateWorking)
	if res := r.do(wire.ConversationDeleteCmd{OpID: "d1", ID: r.id}); res.R != wire.ResultRefused || res.Code != wire.RefuseAgentWorking || len(cl.closed) != 0 {
		t.Fatalf("%+v %v", res, cl.closed)
	}
	force := true
	if res := r.do(wire.ConversationDeleteCmd{OpID: "d2", ID: r.id, Force: &force}); res.R != wire.ResultAccepted || len(cl.closed) != 1 {
		t.Fatalf("%+v %v", res, cl.closed)
	}
}
