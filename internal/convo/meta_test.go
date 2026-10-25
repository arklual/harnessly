package convo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/sessionmeta"
	"github.com/ly-solutions/harnessly/internal/shots"
	"github.com/ly-solutions/harnessly/internal/wire"
)

func (r *rig) raw(format string, args ...any) {
	r.t.Helper()
	fh, err := os.OpenFile(r.path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		r.t.Fatal(err)
	}
	defer fh.Close()
	fmt.Fprintf(fh, format+"\n", args...)
}

func (r *rig) record() wire.Conversation {
	r.t.Helper()
	cs, err := r.rt.Conversations(context.Background())
	if err != nil {
		r.t.Fatal(err)
	}
	for _, c := range cs.Conversations {
		if c.ID == r.id {
			return c
		}
	}
	r.t.Fatalf("no conversation %s", r.id)
	return wire.Conversation{}
}

func TestTheEffortAndTheGoalComeFromTheTranscript(t *testing.T) {
	r := newRig(t)
	r.rt.d.Meta = &sessionmeta.Reader{}
	r.open()
	r.settle(wire.StateReady)
	r.user("hi")
	r.line(`"type":"assistant","message":{"role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":3,"output_tokens":1}}`)
	// A model the agent answered with, and an effort nobody has told: none is made up.
	r.tick(metaEvery)
	if m := r.record().Model; m != nil && m.Effort != nil {
		t.Fatalf("an effort out of nowhere: %+v", m)
	}
	r.raw(`{"type":"api-request","querySource":"repl_main_thread","params":{"model":"claude-opus-5-5","output_config":{"effort":"xhigh"}}}`)
	r.raw(`{"type":"attachment","timestamp":"2026-10-23T00:02:09.809Z","attachment":{"type":"goal_status","met":false,"sentinel":true,"condition":"ship it"}}`)
	r.tick(metaEvery)
	rec := r.record()
	if rec.Model == nil || rec.Model.Effort == nil || *rec.Model.Effort != wire.EffortXHigh {
		t.Fatalf("effort: %+v goal %+v state %+v", rec.Model, rec.Goal, rec.State)
	}
	if g := rec.Goal; g == nil || g.Text != "ship it" || g.Status != wire.GoalActive {
		t.Fatalf("goal: %+v", g)
	}
	// The tab was started on ultracode: Claude logs xhigh, the record says ultracode is on beside it.
	r.rt.RetitleImported(ImportTab{ID: r.id, LaunchUltracode: true})
	r.raw(`{"type":"attachment","timestamp":"2026-10-23T05:00:00.000Z","attachment":{"type":"goal_status","met":true,"condition":"ship it"}}`)
	r.tick(metaEvery)
	rec = r.record()
	if *rec.Model.Effort != wire.EffortXHigh || !rec.Model.Ultracode || rec.Goal.Status != wire.GoalMet {
		t.Fatalf("%+v %+v", rec.Model, rec.Goal)
	}
	// Turned off in the session with /effort: the transcript's word goes before the launch's.
	r.raw(`{"type":"user","message":{"role":"user","content":"<local-command-stdout>Ultracode off. Effort stays medium.</local-command-stdout>"},"timestamp":"2026-10-23T05:01:00.000Z"}`)
	r.tick(metaEvery)
	if m := r.record().Model; m.Ultracode || *m.Effort != wire.EffortMedium {
		t.Fatalf("%+v", m)
	}
	// Claude Code's own reminders are its word too: on, then off again.
	r.raw(`{"type":"attachment","timestamp":"2026-10-23T05:02:00.000Z","attachment":{"type":"ultra_effort_enter","reminderType":"sparse"}}`)
	r.tick(metaEvery)
	if m := r.record().Model; !m.Ultracode {
		t.Fatalf("enter: %+v", m)
	}
	r.raw(`{"type":"attachment","timestamp":"2026-10-23T05:03:00.000Z","attachment":{"type":"ultra_effort_exit"}}`)
	r.tick(metaEvery)
	if m := r.record().Model; m.Ultracode {
		t.Fatalf("exit: %+v", m)
	}
	// The tab's process started again: it has said nothing yet, and runs what it was launched with.
	r.raw(`{"type":"attachment","timestamp":"2026-10-23T05:04:00.000Z","attachment":{"type":"hook_success","hookName":"SessionStart:resume","hookEvent":"SessionStart"}}`)
	r.tick(metaEvery)
	if m := r.record().Model; !m.Ultracode {
		t.Fatalf("relaunched: %+v", m)
	}
	// The recap Claude wrote for the person who came back, without its hint about turning recaps off.
	if rec.Recap != nil {
		t.Fatalf("a recap out of nowhere: %+v", rec.Recap)
	}
	r.raw(`{"type":"system","subtype":"away_summary","content":"Shipped the contract. (disable recaps in /config)","timestamp":"2027-01-15T09:00:00.000Z"}`)
	r.tick(metaEvery)
	if rc := r.record().Recap; rc == nil || rc.Text != "Shipped the contract." || rc.At != 1800003600000 {
		t.Fatalf("recap: %+v", rc)
	}
}

// The pictures the agent read or a tool saved are listed in the conversation's snapshot, newest first, and a picture is
// found only by an id of that list.
func TestPicturesTheAgentLookedAtAreListed(t *testing.T) {
	r := newRig(t)
	r.rt.d.Shots = &shots.Reader{}
	r.open()
	r.settle(wire.StateReady)
	r.user("look")
	r.raw(`{"type":"assistant","timestamp":"2026-10-23T05:00:00.000Z","cwd":"/w","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"/w/shots/home.png"}}]}}`)
	r.raw(`{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}]}}`)
	r.tick(metaEvery)
	snap, err := r.rt.Conversation(context.Background(), r.id)
	if err != nil || len(snap.Shots) != 1 || snap.Shots[0].Name != "home.png" || snap.Shots[0].Agent != nil {
		t.Fatalf("shots: %+v %v", snap.Shots, err)
	}
	path, err := r.rt.ShotPath(context.Background(), r.id, snap.Shots[0].ID)
	if err != nil || path != "/w/shots/home.png" {
		t.Fatalf("path: %q %v", path, err)
	}
	for _, id := range []string{"", "nope", "/w/shots/home.png", "/etc/passwd"} {
		if _, err := r.rt.ShotPath(context.Background(), r.id, id); !errors.Is(err, wire.ErrNoShot) {
			t.Fatalf("%q: %v", id, err)
		}
	}
}

// A recap is for the person who comes back: once they write after it, it is not what happens now, and a later read of
// the same transcript does not bring it back. A recap written after their last message stands.
func TestARecapGoesWhenThePersonWritesAfterIt(t *testing.T) {
	r := newRig(t)
	r.rt.d.Meta = &sessionmeta.Reader{}
	r.open()
	r.settle(wire.StateReady)
	r.user("hi")
	r.tick(time.Hour)
	r.raw(`{"type":"system","subtype":"away_summary","content":"Waiting for CI.","timestamp":%q}`, r.clock.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
	r.tick(metaEvery)
	if rc := r.record().Recap; rc == nil || rc.Text != "Waiting for CI." {
		t.Fatalf("a recap after the last message stands: %+v", rc)
	}
	r.tick(time.Hour)
	r.user("what is the state now?")
	r.tick(metaEvery)
	r.tick(metaEvery)
	if rc := r.record().Recap; rc != nil {
		t.Fatalf("the person wrote after the recap, it is stale: %+v", rc)
	}
}
