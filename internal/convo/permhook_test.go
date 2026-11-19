package convo

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

type hookAnswer struct {
	reply PermissionReply
	ok    bool
}

func (r *rig) permissionHook(ctx context.Context, ask PermissionAsk) <-chan hookAnswer {
	out := make(chan hookAnswer, 1)
	go func() {
		reply, ok := r.rt.PermissionRequest(ctx, r.id, ask)
		out <- hookAnswer{reply, ok}
	}()
	return out
}

func working(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	r.user("go")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	return r
}

func (r *rig) waitCards(n int) []wire.Permission {
	r.t.Helper()
	for range 200 {
		if ps := r.snap().Permissions; len(ps) >= n {
			return ps
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.t.Fatalf("no %d cards: %+v", n, r.snap().Permissions)
	return nil
}

func answered(t *testing.T, c <-chan hookAnswer) hookAnswer {
	t.Helper()
	select {
	case a := <-c:
		return a
	case <-time.After(3 * time.Second):
		t.Fatal("the hook waits on")
		return hookAnswer{}
	}
}

var bashAsk = PermissionAsk{Tool: "Bash", Input: json.RawMessage(`{"command":"rm -rf build"}`), Cwd: "/work",
	Suggestions: json.RawMessage(`[{"type":"addRules"}]`)}

// The hook opens the card from the call itself, with nothing on the screen, and takes the answer: no key is pressed.
func TestPermissionHookTakesTheAnswer(t *testing.T) {
	r := working(t)
	got := r.permissionHook(context.Background(), bashAsk)
	p := r.waitCards(1)[0]
	if p.Kind != wire.PermTool || p.Command == nil || *p.Command != "$ rm -rf build" || len(p.Options) != 3 {
		t.Fatalf("card: %+v", p)
	}
	if k := r.state().K; k != wire.StateWaitingPermission {
		t.Fatalf("state: %s", k)
	}
	if res := r.do(wire.DecideCmd{OpID: "d1", ConversationID: r.id, PermissionID: p.ID, OptionID: "2"}); res.R != wire.ResultAccepted {
		t.Fatalf("decide: %+v", res)
	}
	a := answered(t, got)
	if !a.ok || a.reply.Behavior != "allow" || string(a.reply.UpdatedPermissions) != `[{"type":"addRules"}]` {
		t.Fatalf("answer: %+v", a)
	}
	if k := r.snap().Permissions[0].State.K; k != wire.GateStateAnswered {
		t.Fatalf("gate: %s", k)
	}
	if k := r.state().K; k != wire.StateWorking {
		t.Fatalf("state: %s", k)
	}
	for _, call := range r.pane.calls {
		if len(call) > 5 && call[:5] == "keys:" {
			t.Fatalf("a key went in: %v", r.pane.calls)
		}
	}
}

// A plan is approved with its text in the answer and the mode the person chose; changing it stops the turn.
func TestPermissionHookPlan(t *testing.T) {
	input := json.RawMessage(`{"plan":"## Plan","planFilePath":"/p.md"}`)
	for id, want := range map[string]string{"1": "acceptEdits", "2": "default"} {
		r := working(t)
		got := r.permissionHook(context.Background(), PermissionAsk{Tool: planTool, Input: input})
		p := r.waitCards(1)[0]
		if p.Kind != wire.PermPlan || p.PlanMD == nil || *p.PlanMD != "## Plan" {
			t.Fatalf("card: %+v", p)
		}
		r.do(wire.DecideCmd{OpID: "d1", ConversationID: r.id, PermissionID: p.ID, OptionID: id})
		a := answered(t, got)
		var rules []map[string]string
		_ = json.Unmarshal(a.reply.UpdatedPermissions, &rules)
		if a.reply.Behavior != "allow" || string(a.reply.UpdatedInput) != string(input) || len(rules) != 1 || rules[0]["mode"] != want {
			t.Fatalf("option %s: %+v", id, a.reply)
		}
		r.line(`"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_p","name":"ExitPlanMode","input":{"plan":"## Plan"}}]}`)
		r.tick(time.Second)
		if n := len(r.snap().Permissions); n != 1 {
			t.Fatalf("a second card: %d", n)
		}
	}
	r := working(t)
	got := r.permissionHook(context.Background(), PermissionAsk{Tool: planTool, Input: input})
	p := r.waitCards(1)[0]
	r.do(wire.DecideCmd{OpID: "d1", ConversationID: r.id, PermissionID: p.ID, OptionID: "3"})
	if a := answered(t, got); a.reply.Behavior != "deny" || !a.reply.Interrupt {
		t.Fatalf("revise: %+v", a.reply)
	}
}

// Answered in the terminal: the card closes and the hook is let go with no decision.
func TestPermissionHookLetGoWhenTheTerminalAnswers(t *testing.T) {
	r := working(t)
	got := r.permissionHook(context.Background(), bashAsk)
	r.waitCards(1)
	r.busy()
	r.tick(time.Second)
	if a := answered(t, got); a.ok {
		t.Fatalf("a decision nobody took: %+v", a)
	}
	if k := r.snap().Permissions[0].State.K; k == wire.GateStateOpen {
		t.Fatalf("gate: %s", k)
	}
}

// The hook goes while the dialog stands: the card now answers on the screen, with the options there.
func TestPermissionHookGoneLeavesTheDialog(t *testing.T) {
	r := working(t)
	ctx, cancel := context.WithCancel(context.Background())
	got := r.permissionHook(ctx, PermissionAsk{Tool: "Bash", Input: json.RawMessage(`{"command":"rm -rf build"}`)})
	p := r.waitCards(1)[0]
	r.pane.screen = "──────────────────────────────\n Bash command\n ╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌\n │ rm -rf build\n ╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌\n\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n\n Esc to cancel\n"
	cancel()
	if a := answered(t, got); a.ok {
		t.Fatalf("answer: %+v", a)
	}
	r.tick(time.Second)
	if p2 := r.snap().Permissions[0]; p2.ID != p.ID || len(p2.Options) != 2 || p2.Options[1].Label != "No" {
		t.Fatalf("card: %+v", p2)
	}
}

// Two calls ask at once (subagents): two cards, and the chat waits until both are decided.
func TestPermissionHooksSideBySide(t *testing.T) {
	r := working(t)
	first := r.permissionHook(context.Background(), PermissionAsk{Tool: "Bash", Input: json.RawMessage(`{"command":"a"}`)})
	r.waitCards(1)
	second := r.permissionHook(context.Background(), PermissionAsk{Tool: "Bash", Input: json.RawMessage(`{"command":"b"}`)})
	ps := r.waitCards(2)
	r.do(wire.DecideCmd{OpID: "d1", ConversationID: r.id, PermissionID: ps[0].ID, OptionID: "1"})
	if a := answered(t, first); !a.ok {
		t.Fatalf("first: %+v", a)
	}
	if k := r.state().K; k != wire.StateWaitingPermission {
		t.Fatalf("state after the first: %s", k)
	}
	r.do(wire.DecideCmd{OpID: "d2", ConversationID: r.id, PermissionID: ps[1].ID, OptionID: "2"})
	if a := answered(t, second); a.reply.Behavior != "deny" {
		t.Fatalf("second: %+v", a)
	}
	if k := r.state().K; k != wire.StateWorking {
		t.Fatalf("state: %s", k)
	}
}

// A question has its own card: the hook leaves it alone.
func TestPermissionHookLeavesAQuestion(t *testing.T) {
	r := working(t)
	if _, ok := r.rt.PermissionRequest(context.Background(), r.id, PermissionAsk{Tool: questionTool}); ok {
		t.Fatal("a decision")
	}
	if n := len(r.snap().Permissions); n != 0 {
		t.Fatalf("a card: %d", n)
	}
}

// Codex holds its question on the hook and draws only its spinner meanwhile. The spinner is no answer: the
// chat waits on the card until the person decides, and the decision goes to the hook.
func TestCodexHookWaitsThroughItsSpinner(t *testing.T) {
	r := newRig(t)
	r.busy()
	r.rt.Import([]ImportTab{{ID: r.id, Session: "s1", Title: "work", Provider: wire.ProviderCodex, Cwd: "/w",
		TranscriptPath: r.path}}, nil)
	r.settle(wire.StateWorking)
	got := r.permissionHook(context.Background(), PermissionAsk{Tool: "Edit",
		Input: json.RawMessage(`{"file_path":"/h/.codex/skills/a/SKILL.md"}`), Cwd: "/w"})
	p := r.waitCards(1)[0]
	if p.Command == nil || *p.Command != "/h/.codex/skills/a/SKILL.md" {
		t.Fatalf("card: %+v", p)
	}
	for range 10 {
		r.tick(time.Second)
	}
	if st := r.state(); st.K != wire.StateWaitingPermission || st.PermissionID != p.ID {
		t.Fatalf("the spinner ended the wait: %+v", st)
	}
	if res := r.do(wire.DecideCmd{OpID: "d1", ConversationID: r.id, PermissionID: p.ID, OptionID: "1"}); res.R != wire.ResultAccepted {
		t.Fatalf("decide: %+v", res)
	}
	if a := answered(t, got); !a.ok || a.reply.Behavior != "allow" {
		t.Fatalf("answer: %+v", a)
	}
	if k := r.state().K; k != wire.StateWorking {
		t.Fatalf("after the answer: %s", k)
	}
}

// Codex stopped with esc is back at its prompt and leaves its hook running: the card closes and the hook ends,
// or the chat would wait on a question nobody reads.
func TestCodexStoppedLetsItsHookGo(t *testing.T) {
	r := newRig(t)
	r.busy()
	r.rt.Import([]ImportTab{{ID: r.id, Session: "s1", Title: "work", Provider: wire.ProviderCodex, Cwd: "/w",
		TranscriptPath: r.path}}, nil)
	r.settle(wire.StateWorking)
	got := r.permissionHook(context.Background(), PermissionAsk{Tool: "Bash", Input: json.RawMessage(`{"command":"mkdir -p /h/x"}`)})
	p := r.waitCards(1)[0]
	if p.Tool == nil || *p.Tool != "Bash" || p.Command == nil || *p.Command != "$ mkdir -p /h/x" {
		t.Fatalf("card: %+v", p)
	}
	r.pane.screen = tui(t, "codex-idle-placeholder.ansi.txt")
	for range 3 {
		r.tick(time.Second)
	}
	if a := answered(t, got); a.ok {
		t.Fatalf("the hook got an answer: %+v", a)
	}
	if g := r.snap().Permissions[0].State; g.K != wire.GateStateClosed || g.Reason != wire.ClosedAgentMovedOn {
		t.Fatalf("card: %+v", g)
	}
	if k := r.state().K; k == wire.StateWaitingPermission {
		t.Fatalf("still waiting")
	}
}

// a Codex dialog the screen made a card of goes (esc in the terminal): Codex back at its prompt closes the card,
// so an idle chat offers no buttons for a question nobody asks any more.
func TestCodexAtItsPromptClosesTheScreenCard(t *testing.T) {
	r := newRig(t)
	r.busy()
	r.rt.Import([]ImportTab{{ID: r.id, Session: "s1", Title: "work", Provider: wire.ProviderCodex, Cwd: "/w",
		TranscriptPath: r.path}}, nil)
	r.settle(wire.StateWorking)
	r.pane.screen = tui(t, "codex-exec-approval.ansi.txt")
	r.tick(time.Second)
	p := r.waitCards(1)[0]
	if p.State.K != wire.GateStateOpen || len(p.Options) != 3 {
		t.Fatalf("card: %+v", p)
	}
	if k := r.state().K; k != wire.StateWaitingPermission {
		t.Fatalf("state %s", k)
	}
	// esc: Codex aborts the turn and stands at its prompt. A turn's end leaves a screen card open (it may be another
	// agent's question), so the prompt is what closes it.
	r.pane.screen = tui(t, "codex-idle-placeholder.ansi.txt")
	if err := r.rt.Hook(r.id, Hook{Kind: "stop"}); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		r.tick(time.Second)
	}
	if g := r.snap().Permissions[0].State; g.K != wire.GateStateClosed {
		t.Fatalf("card after esc: %+v, state %s", g, r.state().K)
	}
	if k := r.state().K; k == wire.StateWaitingPermission {
		t.Fatalf("still waiting")
	}
}

// a dialog no hook announced stands in a Claude turn. The scrollback keeps the person's earlier prompt behind
// a ❯, which read as the input line, so the chat said «working» and asked nothing; the dialog gets its card.
func TestClaudeUnannouncedDialogGetsACard(t *testing.T) {
	r := working(t)
	r.pane.screen = tui(t, "claude-2.1.310-switch-model-confirm.ansi.txt")
	for range 8 {
		r.tick(time.Second)
	}
	ps := r.snap().Permissions
	if len(ps) != 1 || ps[0].Kind != wire.PermDialog || len(ps[0].Options) != 2 {
		t.Fatalf("cards: %+v", ps)
	}
	if st := r.state(); st.K != wire.StateWaitingPermission || st.PermissionID != ps[0].ID {
		t.Fatalf("state: %+v", st)
	}
}
