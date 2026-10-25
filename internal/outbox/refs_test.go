package outbox

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// The queue carries a message's references with it: on the event the page draws, on the disk across a restart, and a
// repeat of its client id changes nothing.
func TestTheQueueCarriesTheReferences(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), wire.StateWorking)
	refs := []wire.MessageRef{{Kind: wire.RefBoard, ID: "flow", Title: "Flow", Target: "https://boards.example/b/flow"}}
	text := "look\n\n<attached>\n…\n</attached>"
	ch, code := r.box.Send(Draft{ClientID: "m1", Text: text, Refs: refs, Mode: wire.ModeQueue, Kind: wire.KindPrompt},
		r.agent.State, r.clock.Now())
	if code != "" {
		t.Fatal(code)
	}
	up, ok := ch.Events[0].(wire.MessageUpsertEv)
	if !ok || !slices.Equal(up.Message.Refs, refs) || up.Message.Text != text {
		t.Fatalf("event: %+v", ch.Events)
	}
	again, code := r.box.Send(Draft{ClientID: "m1", Text: text, Refs: refs, Mode: wire.ModeQueue, Kind: wire.KindPrompt},
		r.agent.State, r.clock.Now())
	if code != "" || len(again.Events) != 0 || len(r.box.Queue().Items) != 1 {
		t.Fatalf("repeat: %+v %q", again, code)
	}
	raw, err := json.Marshal(r.box.State())
	if err != nil {
		t.Fatal(err)
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	b := New("tab-1", wire.DefaultLimits, nil)
	b.Restore(st)
	m, ok := b.Message("m1")
	if !ok || !slices.Equal(m.Refs, refs) || m.Text != text {
		t.Fatalf("restored: %+v", m)
	}
}
