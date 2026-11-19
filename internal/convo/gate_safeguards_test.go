package convo

import (
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// Claude's dialog when the model refuses a request on its safeguards: the turn cannot go on until the person picks.
const safeguardsDialog = `
 Opus 5.5 flagged this request

 Claude Code applies these safeguards to reduce the amount of incorrectly flagged messages. Edit and retry, or
 continue with Opus 4.8, which has no such safeguards.

 Details: [cyber]

 ❯ 1. Switch to Opus 4.8
   2. Edit prompt and retry with Opus 5.5

 Enter to confirm · Esc to cancel
`

// safeguardsAsked is a chat at work whose agent stands on the safeguards dialog, with its card open.
func safeguardsAsked(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	r.user("check the exploit")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.pane.screen = safeguardsDialog
	for range 8 {
		r.tick(time.Second)
	}
	r.waitsOnOneCard(t)
	return r
}

// waitsOnOneCard: the chat says «waits for you» and one card stands open for the dialog.
func (r *rig) waitsOnOneCard(t *testing.T) wire.Permission {
	t.Helper()
	ps := r.snap().Permissions
	if k := r.state().K; k != wire.StateWaitingPermission || len(ps) != 1 || ps[0].Kind != wire.PermDialog ||
		ps[0].State.K != wire.GateStateOpen {
		t.Fatalf("state %s, permissions %+v", k, ps)
	}
	return ps[0]
}

func TestSafeguardsDialogIsOneCardWithTheTwoChoices(t *testing.T) {
	r := safeguardsAsked(t)
	p := r.waitsOnOneCard(t)
	if len(p.Options) != 2 || p.Options[0].Label != "Switch to Opus 4.8" || p.Options[1].Label != "Edit prompt and retry with Opus 5.5" ||
		p.State.Refused != nil || p.PlanMD == nil || !strings.Contains(*p.PlanMD, "[cyber]") {
		t.Fatalf("card: %+v", p)
	}
	// The choice is the person's: nothing goes into the dialog by itself, however long it stands.
	for range 120 {
		r.tick(time.Second)
	}
	for _, c := range r.pane.calls {
		if strings.HasPrefix(c, "keys:") {
			t.Fatalf("a key went in: %v", r.pane.calls)
		}
	}
	r.waitsOnOneCard(t)
	r.pane.onSend = func(call string) {
		if call == "keys:2" {
			r.busy()
		}
	}
	if res := r.do(wire.DecideCmd{OpID: "d1", ConversationID: r.id, PermissionID: p.ID, OptionID: "2"}); res.R != wire.ResultAccepted ||
		!strings.Contains(strings.Join(r.pane.calls, ","), "keys:2") {
		t.Fatalf("decide: %+v %v", res, r.pane.calls)
	}
	r.tick(time.Second)
	if k := r.state().K; k != wire.StateWorking {
		t.Fatalf("after the choice: %s", k)
	}
}

// a prompt the agent took while its dialog stood (the queue, a steered message) closed the card and left the
// chat at work; the dialog stood on, a second card came, and the chat said «works» for hours with a question open.
func TestDialogThatStandsAfterAPromptIsTheSameCard(t *testing.T) {
	r := safeguardsAsked(t)
	first := r.snap().Permissions[0].ID
	r.hook("prompt-submit")
	for range 20 {
		r.tick(time.Second)
	}
	ps := r.snap().Permissions
	if len(ps) != 1 || ps[0].ID != first || ps[0].State.K != wire.GateStateOpen || r.state().K != wire.StateWaitingPermission {
		t.Fatalf("state %s, permissions %+v", r.state().K, ps)
	}
}

// One frame of the input box over the dialog (a redraw) is no answer: the card and the wait stand.
func TestDialogRedrawIsNoAnswer(t *testing.T) {
	r := safeguardsAsked(t)
	first := r.snap().Permissions[0].ID
	r.idle()
	r.tick(time.Second)
	r.pane.screen = safeguardsDialog
	for range 12 {
		r.tick(time.Second)
	}
	if p := r.waitsOnOneCard(t); p.ID != first {
		t.Fatalf("another card: %s", p.ID)
	}
}

// A chat that works with no turn open in its transcript (the turn ended, its stop never came and a prompt put it back
// at work) is a chat that waits as soon as a dialog stands on its screen.
func TestDialogOfAChatAtWorkWithNoTurnOpen(t *testing.T) {
	r := newRig(t)
	r.user("go")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.say("done")
	r.turnEnd()
	r.idle()
	r.hook("stop")
	r.hook("prompt-submit")
	if k := r.state().K; k != wire.StateWorking {
		t.Fatalf("state: %s", k)
	}
	r.pane.screen = safeguardsDialog
	for range 8 {
		r.tick(time.Second)
	}
	r.waitsOnOneCard(t)
}

// Whatever took the chat out of its wait, a card that stands open with its dialog on the screen puts it back, and a
// card whose dialog has gone is let go: the chat never works with a question open.
func TestOpenCardHoldsTheWait(t *testing.T) {
	r := safeguardsAsked(t)
	first := r.snap().Permissions[0].ID
	leave := func() {
		a := r.rt.actor(r.id)
		if err := r.rt.on(a, func(c *conv, now time.Time) {
			c.rec.State = wire.AgentState{K: wire.StateWorking, Since: ms(now)}
			c.rec.Gate = nil
		}); err != nil {
			t.Fatal(err)
		}
		if k := r.state().K; k != wire.StateWorking {
			t.Fatalf("state: %s", k)
		}
	}
	leave()
	r.tick(time.Second)
	if p := r.waitsOnOneCard(t); p.ID != first {
		t.Fatalf("another card: %s", p.ID)
	}
	leave()
	r.busy()
	for range 4 {
		r.tick(time.Second)
	}
	ps := r.snap().Permissions
	if len(ps) != 1 || ps[0].State.K != wire.GateStateClosed || r.state().K != wire.StateWorking {
		t.Fatalf("state %s, permissions %+v", r.state().K, ps)
	}
}

// safeguardsPaused is the dialog as a real Claude Code drew it (2026-11-19): «Session paused», no footer, and the
// retry spinner of the stopped request still under the menu.
const safeguardsPaused = `
● Opus 5.5's safeguards stopped the response above · continuing once
  with that noted

────────────────────────────────────────────────────────────────────────────────
  Session paused

  Opus 5.5's safeguards flagged this session. You may be seeing this for the
  first time: Opus 5.5 is more capable and has stronger safeguards as a
  result, which can sometimes flag non-cybersecurity work. We're improving
  these safeguards to reduce the amount of incorrectly flagged messages. Edit
  and retry, or continue with Opus 4.8. Send feedback with /feedback or learn
  more: https://support.claude.com/en/articles/8106465

  Details: ` + "`" + `[cyber]` + "`" + `

  ❯ 1. Switch to Opus 4.8
    2. Edit prompt and retry with Opus 5.5

✻ Waiting for API response · will retry in 0s · check your network
`

func TestRealSafeguardsScreenWaitsOnOneCard(t *testing.T) {
	r := newRig(t)
	r.user("draft the post")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.pane.screen = safeguardsPaused
	for range 8 {
		r.tick(time.Second)
	}
	p := r.waitsOnOneCard(t)
	if len(p.Options) != 2 || p.Options[0].Label != "Switch to Opus 4.8" || p.Options[1].Label != "Edit prompt and retry with Opus 5.5" {
		t.Fatalf("card: %+v", p)
	}
	for range 30 {
		r.tick(time.Second)
	}
	r.waitsOnOneCard(t)
}
