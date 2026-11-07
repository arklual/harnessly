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

// The pending call of the main agent is not the one the menu on the screen draws (a subagent asks): the card is the
// screen's own, with the command it draws, and no key goes in by itself.
func TestPermissionOfAnotherCallIsTheScreensOwnCard(t *testing.T) {
	r := newRig(t)
	r.bashPrompt(tui(t, "claude-2.1.311-bash-dangerous-rm-auto-deny.ansi.txt"), "git push --force origin main")
	p := r.snap().Permissions[0]
	if p.ID == "p:toolu_rm" || p.Command == nil || strings.Contains(*p.Command, "git push") || p.State.K != wire.GateStateOpen {
		t.Fatalf("permission: %+v", p)
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

// Claude 2.1.316 writes the ExitPlanMode call only once its menu is answered. The screen opens the plan's card,
// its answer reaches the menu, and the call written after that opens no second card.
func TestPlanCardFromTheScreen(t *testing.T) {
	r := newRig(t)
	r.user("plan it")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.pane.screen = tui(t, "claude-2.1.316-plan.ansi.txt")
	r.tick(time.Second)
	if k := r.state().K; k != wire.StateWaitingPermission {
		t.Fatalf("state: %s", k)
	}
	ps := r.snap().Permissions
	if len(ps) != 1 || ps[0].Kind != wire.PermPlan || len(ps[0].Options) != 3 {
		t.Fatalf("permissions: %+v", ps)
	}
	if ps[0].TurnID == nil {
		t.Fatal("a card outside its turn")
	}
	r.pane.onSend = func(call string) {
		if call == "keys:2" {
			r.busy()
		}
	}
	res := r.do(wire.DecideCmd{OpID: "d1", ConversationID: r.id, PermissionID: ps[0].ID, OptionID: "2"})
	if res.R != wire.ResultAccepted || !strings.Contains(strings.Join(r.pane.calls, ","), "keys:2") {
		t.Fatalf("decide: %+v %v", res, r.pane.calls)
	}
	// The call comes after the answer, and its result with it: no second card, none left open.
	r.line(`"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_plan","name":"ExitPlanMode","input":{"plan":"## Plan"}}]}`)
	r.result("toolu_plan", "approved")
	r.tick(time.Second)
	ps = r.snap().Permissions
	if len(ps) != 1 || ps[0].State.K == wire.GateStateOpen {
		t.Fatalf("after the call: %+v", ps)
	}
	if k := r.state().K; k == wire.StateWaitingPermission {
		t.Fatalf("still waiting: %s", k)
	}
}

// A low window under the chat: the card comes as well, with the option that fits.
func TestCutPermissionOpensACard(t *testing.T) {
	r := newRig(t)
	r.user("clean up")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.pane.screen = " Do you want to proceed?\n ❯ 1. Yes\n\n Esc to cancel · Tab to amend\n"
	for range 8 {
		r.tick(time.Second)
	}
	if k := r.state().K; k != wire.StateWaitingPermission {
		t.Fatalf("state: %s", k)
	}
	ps := r.snap().Permissions
	if len(ps) != 1 || ps[0].Kind != wire.PermDialog || len(ps[0].Options) != 0 || ps[0].State.Refused == nil {
		t.Fatalf("a cut menu is no button to press: %+v", ps)
	}
	// The whole dialog comes into view: the card of the permission takes over, with its options.
	r.pane.screen = " Do you want to proceed?\n ❯ 1. Yes\n   2. No\n\n Esc to cancel\n"
	r.tick(time.Second)
	r.tick(time.Second)
	ps = r.snap().Permissions
	last := ps[len(ps)-1]
	if len(ps) != 2 || last.Kind != wire.PermTool || len(last.Options) != 2 || ps[0].State.K == wire.GateStateOpen {
		t.Fatalf("permissions: %+v", ps)
	}
}

// The menu on the screen is not the one the card shows: no key goes in.
func TestDialogCardDoesNotAnswerAnotherMenu(t *testing.T) {
	r := newRig(t)
	r.user("pick")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.pane.screen = " Which one?\n ❯ 1. Yes\n   2. Later\n\n Esc to cancel\n"
	for range 8 {
		r.tick(time.Second)
	}
	ps := r.snap().Permissions
	if len(ps) != 1 || ps[0].Kind != wire.PermDialog {
		t.Fatalf("permissions: %+v", ps)
	}
	r.pane.screen = " Delete everything?\n ❯ 1. Yes\n   2. Later\n\n Esc to cancel\n"
	r.tick(time.Second)
	if g := r.snap().Permissions[0].State; g.Refused == nil {
		t.Fatalf("gate: %+v", g)
	}
	r.do(wire.DecideCmd{OpID: "d1", ConversationID: r.id, PermissionID: ps[0].ID, OptionID: "1"})
	r.tick(time.Second)
	for _, call := range r.pane.calls {
		if strings.HasPrefix(call, "keys:") {
			t.Fatalf("a key went in: %v", r.pane.calls)
		}
	}
}

// A menu nobody announced: a card once it has stood a while, none for one that goes at once, none between turns.
func TestUnknownMenuOpensACard(t *testing.T) {
	const menu = " Which one?\n ❯ 1. Red\n   2. Blue\n\n Esc to cancel\n"
	r := newRig(t)
	r.user("pick")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.pane.screen = menu
	r.tick(time.Second)
	if n := len(r.snap().Permissions); n != 0 {
		t.Fatalf("a card at once: %d", n)
	}
	for range 8 {
		r.tick(time.Second)
	}
	ps := r.snap().Permissions
	if len(ps) != 1 || ps[0].Kind != wire.PermDialog || len(ps[0].Options) != 2 || ps[0].State.Refused != nil {
		t.Fatalf("permissions: %+v", ps)
	}
	r.pane.onSend = func(call string) {
		if call == "keys:2" {
			r.busy()
		}
	}
	if res := r.do(wire.DecideCmd{OpID: "d1", ConversationID: r.id, PermissionID: ps[0].ID, OptionID: "2"}); res.R != wire.ResultAccepted {
		t.Fatalf("decide: %+v", res)
	}
	r.tick(time.Second)
	if k := r.state().K; k == wire.StateWaitingPermission {
		t.Fatalf("still waiting: %s", k)
	}

	idle := newRig(t)
	idle.user("hi")
	idle.busy()
	idle.open()
	idle.settle(wire.StateWorking)
	idle.say("done")
	idle.turnEnd()
	idle.idle()
	idle.hook("stop")
	idle.pane.screen = menu
	for range 8 {
		idle.tick(time.Second)
	}
	if n := len(idle.snap().Permissions); n != 0 {
		t.Fatalf("a card for a picker between turns: %d", n)
	}
}

// The hook says the agent waits, and the screen is no menu anybody knows: the card comes all the same and sends the
// person to the terminal; no key goes in, and it leaves with the wait.
func TestPromptTheScreenCannotReadStillGetsACard(t *testing.T) {
	r := newRig(t)
	r.user("go")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.pane.screen = " A dialog of a new version\n press y to go on\n"
	if err := r.rt.Hook(r.id, Hook{Kind: "notification", Notification: "permission_prompt"}); err != nil {
		t.Fatal(err)
	}
	r.tick(time.Second)
	if n := len(r.snap().Permissions); n != 0 {
		t.Fatalf("a card at once: %d", n)
	}
	for range 8 {
		r.tick(time.Second)
	}
	ps := r.snap().Permissions
	if len(ps) != 1 || ps[0].Kind != wire.PermDialog || ps[0].State.Refused == nil || *ps[0].State.Refused != wire.GateRefusedMenuUnmatched {
		t.Fatalf("permissions: %+v", ps)
	}
	for _, call := range r.pane.calls {
		if strings.HasPrefix(call, "keys:") {
			t.Fatalf("a key went in: %v", r.pane.calls)
		}
	}
	r.busy()
	r.tick(time.Second)
	if k := r.state().K; k == wire.StateWaitingPermission {
		t.Fatalf("still waiting: %s", k)
	}
}

// The hook came and the agent is back at work by itself: no card.
func TestPromptThatWentByItselfGetsNoCard(t *testing.T) {
	r := newRig(t)
	r.user("go")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	if err := r.rt.Hook(r.id, Hook{Kind: "notification", Notification: "permission_prompt"}); err != nil {
		t.Fatal(err)
	}
	for range 8 {
		r.tick(time.Second)
	}
	if n := len(r.snap().Permissions); n != 0 {
		t.Fatalf("a card: %d", n)
	}
}
