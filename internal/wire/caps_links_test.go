package wire

import (
	"net/http/httptest"
	"testing"
)

func TestLinkOutputsOnlyForAPageThatReadsThem(t *testing.T) {
	turn := Turn{ID: "t1", Outputs: []TurnOutput{
		{Kind: OutputStand, Op: OutputCreated, Title: "stand", Ref: "https://s.preview.example.com"},
		{Kind: OutputTask, Op: OutputCreated, Title: "HRN-1", Ref: "https://tracker.example.com/issue/HRN-1"},
	}}
	ev := TurnFinishedEv{Turn: turn}
	old, changed := eventFor(ev, Caps{Delegation: true, Workflows: true, Profiles: true, MsgStatus: true})
	if !changed || len(old.(TurnFinishedEv).Turn.Outputs) != 1 || old.(TurnFinishedEv).Turn.Outputs[0].Kind != OutputStand {
		t.Fatalf("a page without links: %+v", old)
	}
	if len(turn.Outputs) != 2 {
		t.Fatal("the event's own turn was changed")
	}
	if _, changed := eventFor(ev, AllCaps); changed {
		t.Fatal("a page with links got another event")
	}
	page := TurnsPageFor(TurnsPage{Turns: []Turn{turn}}, Caps{})
	if len(page.Turns[0].Outputs) != 1 {
		t.Fatalf("turns page: %+v", page.Turns[0].Outputs)
	}
	only := Turn{ID: "t2", Outputs: []TurnOutput{{Kind: OutputCommit, Op: OutputCreated, Title: "c", Ref: "https://g.example.com/p/-/commit/abc"}}}
	if got, _ := turnWithoutLinks(only); got.Outputs != nil {
		t.Fatalf("no outputs left must be none: %+v", got.Outputs)
	}
	if !CapsOf(httptest.NewRequest("GET", "/api/v2/socket?caps=profiles,links", nil)).Links {
		t.Fatal("caps=links not read")
	}
}
