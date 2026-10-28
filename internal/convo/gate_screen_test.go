package convo

import (
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

const dangerousRmCommand = "cd /tmp/claude-995/-var-lib-example-ai-profiles-sergey-projects/2f3c3409-3642-4b42-a945-ff3388519f95/scratchpad && rm -f food/* && sed -i \"s/^Q=.*/Q={'tapas':'tapas bar Barcelona counter','vermut':'vermut Barcelona','paella':'paella Barceloneta','xocolata':'xocolata amb melindros','bomba':'bomba Barceloneta tapa','pintxos':'pintxos Barcelona','cova':'La Cova Fumada'}/\" food_fetch.py && timeout 200 python3 -I food_fetch.py" //nolint:misspell // Barceloneta is the district in the captured screen

func (r *rig) bashPrompt(screen, command string) {
	r.user("fetch the food")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.tool("toolu_rm", command)
	r.pane.screen = screen
	if err := r.rt.Hook(r.id, Hook{Kind: "notification", Notification: "permission_prompt"}); err != nil {
		r.t.Fatal(err)
	}
}

// Claude 2.1.311 puts a warning and its auto-deny timer between the command and the question, draws the
// command in a gutter box and wraps it. The card's «Yes» still reaches the menu, and the timer shows in the card.
func TestPermissionWithAWarningAndAnAutoDenyTimer(t *testing.T) {
	r := newRig(t)
	r.bashPrompt(tui(t, "claude-2.1.311-bash-dangerous-rm-auto-deny.ansi.txt"), dangerousRmCommand)
	p := r.snap().Permissions[0]
	if len(p.Options) != 2 || p.State.K != wire.GateStateOpen || p.State.Refused != nil {
		t.Fatalf("permission: %+v", p)
	}
	if p.State.AutoDenyAt == nil || *p.State.AutoDenyAt-ms(r.clock.Now()) != 77_000 {
		t.Fatalf("the timer of the screen: %v", p.State.AutoDenyAt)
	}
	r.pane.onSend = func(call string) {
		if call == "keys:1" {
			r.busy()
		}
	}
	res := r.do(wire.DecideCmd{OpID: "d1", ConversationID: r.id, PermissionID: "p:toolu_rm", OptionID: "1"})
	if res.R != wire.ResultAccepted || !strings.Contains(strings.Join(r.pane.calls, ","), "keys:1") {
		t.Fatalf("decide: %+v %v", res, r.pane.calls)
	}
}

// A menu the machine cannot match is not offered as buttons: the gate says so as soon as the screen is read.
func TestPermissionOfAnotherCallIsRefusedAtOnce(t *testing.T) {
	r := newRig(t)
	r.bashPrompt(tui(t, "claude-2.1.311-bash-dangerous-rm-auto-deny.ansi.txt"), "git push --force origin main")
	g := r.snap().Permissions[0].State
	if g.K != wire.GateStateOpen || g.Refused == nil || *g.Refused != wire.GateRefusedMenuUnmatched {
		t.Fatalf("gate: %+v", g)
	}
	r.tick(time.Second)
	for _, c := range r.pane.calls {
		if strings.HasPrefix(c, "keys:") {
			t.Fatalf("a key went in: %v", r.pane.calls)
		}
	}
}

// a background agent asks after the chat's turn ended (Claude drew «This shell -c script runs rm and could
// not be checked», two options). The chat waits for the answer with a card in its last turn, the card shows the
// command the dialog draws, its «Yes» reaches the menu, and the chat leaves the wait once the menu is gone.
func TestBackgroundAgentAsksAfterTheTurn(t *testing.T) {
	r := newRig(t)
	r.user("ship it in the background")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.say("Started both in the background.")
	r.turnEnd()
	r.idle()
	r.hook("stop")
	if k := r.state().K; k != wire.StateReady {
		t.Fatalf("after stop: %s", k)
	}
	r.pane.screen = tui(t, "claude-bash-shell-c-unchecked.txt")
	r.tick(time.Second)
	st := r.state()
	snap := r.snap()
	if st.K != wire.StateWaitingPermission || len(snap.Permissions) != 1 {
		t.Fatalf("state %s, permissions %+v", st.K, snap.Permissions)
	}
	p := snap.Permissions[0]
	if p.State.K != wire.GateStateOpen || p.State.Refused != nil || len(p.Options) != 2 || st.PermissionID != p.ID {
		t.Fatalf("permission: %+v", p)
	}
	if p.TurnID == nil || len(snap.Turns) == 0 || *p.TurnID != snap.Turns[len(snap.Turns)-1].ID {
		t.Fatalf("the card is not in the last turn: %v", p.TurnID)
	}
	if p.Command == nil || !strings.HasPrefix(*p.Command, "$ S=/tmp/") || !strings.HasSuffix(*p.Command, "; done") {
		t.Fatalf("command: %v", p.Command)
	}
	r.pane.onSend = func(call string) {
		if call == "keys:1" {
			r.idle()
		}
	}
	res := r.do(wire.DecideCmd{OpID: "d1", ConversationID: r.id, PermissionID: p.ID, OptionID: "1"})
	if res.R != wire.ResultAccepted || !strings.Contains(strings.Join(r.pane.calls, ","), "keys:1") {
		t.Fatalf("decide: %+v %v", res, r.pane.calls)
	}
	r.tick(time.Second)
	if k := r.state().K; k == wire.StateWaitingPermission {
		t.Fatalf("still waiting after the menu left: %s", k)
	}
	if g := r.snap().Permissions; len(g) != 1 || g[0].State.K != wire.GateStateAnswered {
		t.Fatalf("gate: %+v", g)
	}
}
