package convo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// a background subagent asks while the main agent writes on. Its question is the screen's, not the main
// agent's pending call: no transcript movement of the main agent and no Stop of its own turn closes it.

// backgroundAsks: the main agent runs its own call (toolu_main) and the screen shows another agent's dangerous rm.
func (r *rig) backgroundAsks() wire.Permission {
	r.t.Helper()
	r.tool("toolu_main", "ls -la")
	r.pane.screen = tui(r.t, "claude-2.1.311-bash-dangerous-rm-auto-deny.ansi.txt")
	r.tick(time.Second)
	ps := r.snap().Permissions
	if len(ps) != 1 {
		r.t.Fatalf("cards: %+v", ps)
	}
	return ps[0]
}

func TestSubagentQuestionIsNotTheMainAgentsPendingCall(t *testing.T) {
	r := working(t)
	p := r.backgroundAsks()
	if strings.Contains(p.ID, "toolu_main") || p.Command == nil || strings.Contains(*p.Command, "ls -la") {
		t.Fatalf("the card is the main agent's call: %+v", p)
	}
	if !strings.HasPrefix(*p.Command, "$ cd /tmp/claude-995") {
		t.Fatalf("the card shows the command on the screen: %q", *p.Command)
	}
}

func TestSubagentQuestionSurvivesTheMainAgentWriting(t *testing.T) {
	r := working(t)
	p := r.backgroundAsks()
	r.result("toolu_main", "total 0")
	r.say("the next step")
	r.tool("toolu_next", "pwd")
	r.result("toolu_next", "/")
	for range 4 {
		r.tick(time.Second)
	}
	ps := r.snap().Permissions
	if len(ps) != 1 || ps[0].ID != p.ID || ps[0].State.K != wire.GateStateOpen {
		t.Fatalf("the card flickered: %+v", ps)
	}
	if k := r.state().K; k != wire.StateWaitingPermission {
		t.Fatalf("state: %s", k)
	}
}

func TestSubagentQuestionSurvivesTheMainAgentsStop(t *testing.T) {
	r := working(t)
	p := r.backgroundAsks()
	r.result("toolu_main", "total 0")
	r.say("done")
	r.turnEnd()
	r.hook("stop")
	for range 3 {
		r.tick(time.Second)
	}
	ps := r.snap().Permissions
	if len(ps) != 1 || ps[0].ID != p.ID || ps[0].State.K != wire.GateStateOpen {
		t.Fatalf("the card closed with the turn: %+v", ps)
	}
	if k := r.state().K; k != wire.StateWaitingPermission {
		t.Fatalf("the chat does not wait for the question: %s", k)
	}
}

func TestSubagentHookCardSurvivesTheMainAgentsStop(t *testing.T) {
	r := working(t)
	got := r.permissionHook(context.Background(), bashAsk)
	p := r.waitCards(1)[0]
	// Claude's dialog of the subagent stands on the screen all the while.
	r.pane.screen = "──────────────────────────────\n Bash command\n ╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌\n │ rm -rf build\n ╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌\n\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n\n Esc to cancel\n"
	r.say("done")
	r.turnEnd()
	r.hook("stop")
	r.tick(time.Second)
	ps := r.snap().Permissions
	if len(ps) != 1 || ps[0].State.K != wire.GateStateOpen {
		t.Fatalf("the card closed with the turn: %+v", ps)
	}
	select {
	case a := <-got:
		t.Fatalf("the hook was let go: %+v", a)
	default:
	}
	if res := r.do(wire.DecideCmd{OpID: "d1", ConversationID: r.id, PermissionID: p.ID, OptionID: "1"}); res.R != wire.ResultAccepted {
		t.Fatalf("decide: %+v", res)
	}
	if a := answered(t, got); !a.ok || a.reply.Behavior != "allow" {
		t.Fatalf("answer: %+v", a)
	}
}

func TestHookCardDoesNotTakeTheMainAgentsPendingCall(t *testing.T) {
	r := working(t)
	r.tool("toolu_main", "ls -la")
	r.permissionHook(context.Background(), PermissionAsk{Tool: "Bash", Input: json.RawMessage(`{"command":"rm -rf build"}`)})
	p := r.waitCards(1)[0]
	if strings.Contains(p.ID, "toolu_main") {
		t.Fatalf("card: %+v", p)
	}
}

func TestOneCommandOneCard(t *testing.T) {
	r := working(t)
	cmd := "rm -rf build"
	got := r.permissionHook(context.Background(), PermissionAsk{Tool: "Bash", Input: json.RawMessage(`{"command":"` + cmd + `"}`)})
	p := r.waitCards(1)[0]
	// The dialog stands on the screen as the hook answers; the agent has not drawn past it yet.
	r.pane.screen = "──────────────────────────────\n Bash command\n ╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌\n │ rm -rf build\n ╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌\n\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n\n Esc to cancel\n"
	if res := r.do(wire.DecideCmd{OpID: "d1", ConversationID: r.id, PermissionID: p.ID, OptionID: "1"}); res.R != wire.ResultAccepted {
		t.Fatalf("decide: %+v", res)
	}
	answered(t, got)
	for range 3 {
		r.tick(time.Second)
	}
	if ps := r.snap().Permissions; len(ps) != 1 {
		t.Fatalf("two cards for one command: %+v", ps)
	}
}

// One frame of a redraw without the menu does not close the subagent's question; the menu gone for good does.
func TestSubagentQuestionSurvivesOneBlankFrame(t *testing.T) {
	r := working(t)
	p := r.backgroundAsks()
	menu := r.pane.screen
	r.busy()
	r.tick(time.Second)
	r.pane.screen = menu
	r.tick(time.Second)
	if ps := r.snap().Permissions; len(ps) != 1 || ps[0].ID != p.ID || ps[0].State.K != wire.GateStateOpen {
		t.Fatalf("the card flickered: %+v", ps)
	}
	r.busy()
	for range 3 {
		r.tick(time.Second)
	}
	if ps := r.snap().Permissions; ps[0].State.K == wire.GateStateOpen {
		t.Fatalf("the card outlived its menu: %+v", ps)
	}
}
