package convo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// The agent state of a record is written in one function of the package, the actor's setState: any other write would
// change the state without a revision of its own and without telling the conversation's channel.
func TestAgentStateIsWrittenOnlyBySetState(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	writes := 0
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				as, ok := n.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for _, lhs := range as.Lhs {
					if !isRecState(lhs) {
						continue
					}
					writes++
					if fn.Name.Name != "setState" {
						t.Errorf("%s: %s writes the agent state of a record past setState", fset.Position(as.Pos()), fn.Name.Name)
					}
				}
				return true
			})
		}
	}
	if writes != 1 {
		t.Fatalf("%d writes of the agent state in setState, want the one", writes)
	}
}

// isRecState: the expression is `<x>.rec.State`, the agent state of an actor's record.
func isRecState(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "State" {
		return false
	}
	rec, ok := sel.X.(*ast.SelectorExpr)
	return ok && rec.Sel.Name == "rec"
}

// stamps are the agent states the conversation's own channel carried, in the order they went.
func (r *rig) stamps() []wire.AgentStamp {
	var out []wire.AgentStamp
	for _, p := range r.events(wire.EvQueueChanged) {
		if q := p.e.(wire.QueueChangedEv); q.Agent != nil && p.ch == wire.ConvChannel(r.id) {
			out = append(out, *q.Agent)
		}
	}
	return out
}

// Every change of the state goes to the conversation's channel with the record's revision and the runtime's epoch:
// the revisions only grow, the convs channel names the same state at the same revision, and a snapshot carries the
// state the record holds.
func TestAgentStateStampGrowsAndAgreesWithTheList(t *testing.T) {
	r := newRig(t)
	r.user("do it")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.say("done")
	r.turnEnd()
	r.idle()
	r.hook("stop")
	r.settle(wire.StateReady)
	r.tick(2 * StateCoalesce)

	stamps := r.stamps()
	if len(stamps) < 2 {
		t.Fatalf("%d stamps for a chat that worked and finished", len(stamps))
	}
	byRev := map[int64]wire.AgentState{}
	var last int64
	for _, s := range stamps {
		if s.Rev <= last {
			t.Fatalf("rev %d after %d", s.Rev, last)
		}
		if s.Epoch != r.rt.Epoch() {
			t.Fatalf("epoch %q, the runtime's is %q", s.Epoch, r.rt.Epoch())
		}
		last, byRev[s.Rev] = s.Rev, s.State
	}
	if got := stamps[len(stamps)-1].State.K; got != wire.StateReady {
		t.Fatalf("the last stamp is %s", got)
	}
	// What the list was told at a revision is what the stamp of the latest change up to it said.
	at := func(rev int64) (wire.AgentState, bool) {
		var best int64
		for stamped := range byRev {
			if stamped <= rev && stamped > best {
				best = stamped
			}
		}
		return byRev[best], best != 0
	}
	checked := 0
	for _, p := range r.pub.all {
		var rev int64
		var state wire.AgentState
		switch e := p.e.(type) {
		case wire.ConversationStateEv:
			rev, state = e.Rev, e.State
		case wire.ConversationUpsertEv:
			rev, state = e.Conv.Rev, e.Conv.State
		default:
			continue
		}
		want, ok := at(rev)
		if !ok {
			continue // the record before its first change of state
		}
		checked++
		if !reflect.DeepEqual(state, want) {
			t.Fatalf("convs at rev %d says %+v, the conversation's channel said %+v", rev, state, want)
		}
	}
	if checked == 0 {
		t.Fatal("no event of the list to compare")
	}

	rec, snap := r.record(), r.snap()
	if snap.Agent == nil {
		t.Fatal("a snapshot without the agent state")
	}
	if snap.Agent.Rev != rec.Rev || snap.Agent.Epoch != r.rt.Epoch() || !reflect.DeepEqual(snap.Agent.State, rec.State) {
		t.Fatalf("snapshot %+v, record rev %d state %+v", *snap.Agent, rec.Rev, rec.State)
	}
}

// A runtime that started again counts in another epoch: the page takes its states whatever revisions it held.
func TestAgentStateStampHasANewEpochAfterARestart(t *testing.T) {
	r := newRig(t)
	r.user("do it")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	before := r.rt.Epoch()
	if s := r.snap().Agent; s == nil || s.Epoch != before {
		t.Fatalf("stamp before the restart: %+v", s)
	}
	r.rt.Advance() // the record reaches the store
	seen := len(r.stamps())

	r.restart()
	r.tick(0)
	if r.rt.Epoch() == before {
		t.Fatal("the same epoch after a restart")
	}
	s := r.snap().Agent
	if s == nil || s.Epoch != r.rt.Epoch() {
		t.Fatalf("stamp after the restart: %+v, epoch %q", s, r.rt.Epoch())
	}
	for _, st := range r.stamps()[seen:] {
		if st.Epoch != r.rt.Epoch() {
			t.Fatalf("a stamp of the old epoch after the restart: %+v", st)
		}
	}
}
