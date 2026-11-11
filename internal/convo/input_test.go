package convo

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// fakeInput plays the agents' structured channel: what it was handed, and how it answers.
type fakeInput struct {
	got []InputMessage
	err error
}

func (f *fakeInput) Send(_ context.Context, _ wire.Conversation, m InputMessage) error {
	f.got = append(f.got, m)
	return f.err
}

// codexRig is a Codex chat at its prompt, with the structured channel in.
func codexRig(t *testing.T, in Input) *rig {
	t.Helper()
	r := newRig(t)
	r.deps = func(d *Deps) { d.Input = in }
	r.rt = r.runtime()
	r.pane.screen = tui(t, "codex-idle-placeholder.ansi.txt")
	r.rt.Import([]ImportTab{{ID: r.id, Session: "s1", Title: "work", Provider: wire.ProviderCodex, Cwd: "/w",
		TranscriptPath: r.path}}, nil)
	r.settle(wire.StateReady)
	return r
}

func (r *rig) typed(text string) int {
	n := 0
	for _, call := range r.pane.calls {
		if strings.Contains(call, text) {
			n++
		}
	}
	return n
}

// A message of a Codex chat with the channel goes over it: nothing is typed, the message is accepted by the channel's
// word, and the turn's end is the channel's to tell — the stop hook beside it does not end the turn a second time.
func TestAMessageGoesOverTheStructuredChannel(t *testing.T) {
	in := &fakeInput{}
	r := codexRig(t, in)
	if res := r.send("m1", "собери проект", wire.ModeQueue); res.R != wire.ResultQueued {
		t.Fatalf("%+v", res)
	}
	r.tick(time.Second)
	if len(in.got) != 1 || in.got[0].ClientID != "m1" || in.got[0].Text != "собери проект" {
		t.Fatalf("handed %+v", in.got)
	}
	if len(r.pane.calls) != 0 {
		t.Fatalf("typed %q", r.pane.calls)
	}
	if k := r.message("m1").State.K; k != wire.MsgAccepted && k != wire.MsgInTurn {
		t.Fatalf("m1 is %s", k)
	}
	r.hook("prompt-submit")
	if k := r.state().K; k != wire.StateWorking {
		t.Fatalf("state %s", k)
	}
	r.hook("stop")
	if k := r.state().K; k != wire.StateWorking {
		t.Fatalf("the hook ended the turn the channel owns: %s", k)
	}
	if err := r.rt.InputTurnEnded(r.id, false, ""); err != nil {
		t.Fatal(err)
	}
	if k := r.state().K; k != wire.StateReady {
		t.Fatalf("state %s after the channel's turn end", k)
	}
	// The channel is gone: the hook tells the ends again.
	r.rt.InputLost(r.id)
	r.hook("prompt-submit")
	r.hook("stop")
	if k := r.state().K; k != wire.StateReady {
		t.Fatalf("state %s after the hook's stop", k)
	}
	if len(in.got) != 1 {
		t.Fatalf("handed again: %+v", in.got)
	}
}

// A channel that fails before the agent took anything loses no message: the same one is typed in the terminal, once,
// and the conversation stays with the terminal.
func TestAFailedChannelFallsBackToTyping(t *testing.T) {
	in := &fakeInput{err: errors.New("no app-server")}
	r := codexRig(t, in)
	r.send("m1", "первое", wire.ModeQueue)
	r.tick(time.Second)
	if len(in.got) != 1 || r.typed("первое") != 1 {
		t.Fatalf("handed %+v, typed %q", in.got, r.pane.calls)
	}
	if k := r.message("m1").State.K; k == wire.MsgFailed || k == wire.MsgCancelled {
		t.Fatalf("m1 is %s", k)
	}
	// The stop hook counts for a conversation that types.
	r.hook("prompt-submit")
	r.hook("stop")
	if k := r.state().K; k != wire.StateReady {
		t.Fatalf("state %s", k)
	}
	r.send("m2", "второе", wire.ModeQueue)
	r.tick(time.Second)
	if len(in.got) != 1 || r.typed("второе") != 1 {
		t.Fatalf("handed %+v, typed %q", in.got, r.pane.calls)
	}
}

// A channel that broke after the message was written does not know whether the agent has it: the message is neither
// typed nor handed over again, and waits for the transcript like a typed one.
func TestAnUncertainHandOverIsNeitherTypedNorRepeated(t *testing.T) {
	in := &fakeInput{err: ErrInputUncertain}
	r := codexRig(t, in)
	r.send("m1", "один раз", wire.ModeQueue)
	for range 5 {
		r.tick(time.Second)
	}
	if len(in.got) != 1 || len(r.pane.calls) != 0 {
		t.Fatalf("handed %+v, typed %q", in.got, r.pane.calls)
	}
	if k := r.message("m1").State.K; k != wire.MsgInInput {
		t.Fatalf("m1 is %s", k)
	}
}

// Without the channel, and for what the terminal reads as its own command, a Codex chat types as before.
func TestCommandsAndChatsWithoutTheChannelAreTyped(t *testing.T) {
	in := &fakeInput{}
	r := codexRig(t, in)
	r.send("m1", "/compact", wire.ModeQueue)
	r.tick(time.Second)
	if len(in.got) != 0 || r.typed("/compact") != 1 {
		t.Fatalf("handed %+v, typed %q", in.got, r.pane.calls)
	}
	plain := codexRig(t, nil)
	plain.send("m1", "как раньше", wire.ModeQueue)
	plain.tick(time.Second)
	if plain.typed("как раньше") != 1 {
		t.Fatalf("typed %q", plain.pane.calls)
	}
}
