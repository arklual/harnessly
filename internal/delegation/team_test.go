package delegation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/chatrole"
	"github.com/ly-solutions/harnessly/internal/peers"
	"github.com/ly-solutions/harnessly/internal/wire"
)

func (r *rig) delegate(caller string, extra map[string]any) string {
	r.t.Helper()
	args := map[string]any{"provider": "claude", "prompt": "do your part"}
	for k, v := range extra {
		args[k] = v
	}
	text, isErr := r.call(caller, "delegate_task", args)
	if isErr {
		r.t.Fatal(text)
	}
	return idOf(r.t, text)
}

// a delegated chat writes to its parent and to a sibling in the middle of its turn; the message goes into the
// running turn (read at the next step), names who wrote it, and leaves the receiver's book alone. A stranger stays out.
func TestTeamMessagesMidTurn(t *testing.T) {
	r := newTestRig(t)
	a := r.delegate("tab-root", map[string]any{"title": "Backend"})
	b := r.delegate("tab-root", map[string]any{"title": "Frontend"})
	before, _ := r.svc.Book.Get(b)

	text, isErr := r.call(a, "send_to_conversation", map[string]any{"id": "parent", "text": "Which port does the API take?"})
	if isErr || !strings.Contains(text, "delivered") && !strings.Contains(text, "queued") {
		t.Fatalf("to parent: %s", text)
	}
	got := r.convs.sent["tab-root"]
	if len(got) != 1 || !r.convs.steers["tab-root"][0] {
		t.Fatalf("parent got %v (steered %v)", got, r.convs.steers["tab-root"])
	}
	env, _ := peers.Parse(got[0])
	if !strings.Contains(env.Text, "Which port") || !strings.Contains(env.Text, a) || env.Chat != "Backend" {
		t.Fatalf("envelope: %+v", env)
	}

	if text, isErr := r.call(a, "send_to_conversation", map[string]any{"id": b, "text": "The contract changed: /api/items returns a page now"}); isErr {
		t.Fatalf("to sibling: %s", text)
	}
	if len(r.convs.sent[b]) != 1 || !strings.Contains(r.convs.sent[b][0], "returns a page") {
		t.Fatalf("sibling got %v", r.convs.sent[b])
	}
	if after, _ := r.svc.Book.Get(b); after.AskedAt != before.AskedAt {
		t.Fatal("a sibling's message counted as work from the parent")
	}

	// list_children of a delegated chat names its parent and the chat beside it.
	if text, _ := r.call(a, "list_children", nil); !strings.Contains(text, "parent (id \"parent\")") || !strings.Contains(text, b) {
		t.Fatalf("team list: %s", text)
	}

	r.convs.add(wire.Conversation{ID: "tab-other", Title: "Someone else", Provider: wire.ProviderClaude})
	if text, isErr := r.call(a, "send_to_conversation", map[string]any{"id": "tab-other", "text": "hi"}); !isErr || !strings.Contains(text, "not-allowed") {
		t.Fatalf("stranger: %s", text)
	}
	if text, isErr := r.call("tab-root", "send_to_conversation", map[string]any{"id": "parent", "text": "hi"}); !isErr || !strings.Contains(text, "no parent") {
		t.Fatalf("root to parent: %s", text)
	}
}

// Two agents answering each other stop at the pace.
func TestTeamMessagesArePaced(t *testing.T) {
	r := newTestRig(t)
	a := r.delegate("tab-root", nil)
	for i := range TeamPairMax {
		if text, isErr := r.call(a, "send_to_conversation", map[string]any{"id": "parent", "text": fmt.Sprint("note ", i)}); isErr {
			t.Fatalf("message %d: %s", i, text)
		}
	}
	if text, isErr := r.call(a, "send_to_conversation", map[string]any{"id": "parent", "text": "one more"}); !isErr || !strings.Contains(text, "too-often") {
		t.Fatalf("past the pace: %s", text)
	}
	r.mu.Lock()
	r.clock = r.clock.Add(TeamWindow)
	r.mu.Unlock()
	if text, isErr := r.call(a, "send_to_conversation", map[string]any{"id": "parent", "text": "later"}); isErr {
		t.Fatalf("after the window: %s", text)
	}
}

// one message reaches every chat at work in its running turn; a free one is not woken, and gets it with its
// next turn; a stopped one is skipped. wake starts the free ones too.
func TestBroadcastSkipsStopped(t *testing.T) {
	r := newTestRig(t)
	a := r.delegate("tab-root", nil)
	b := r.delegate("tab-root", nil)
	c := r.delegate("tab-root", nil)
	r.convs.finish(b, "done\n\n"+MarkDone)
	r.convs.mu.Lock()
	rec := r.convs.recs[c]
	rec.State.K = wire.StateStopped
	r.convs.recs[c] = rec
	r.convs.mu.Unlock()

	// An agent that has not loaded the schema says message instead of text.
	text, isErr := r.call("tab-root", "broadcast_children", map[string]any{"message": "Stop: main is frozen", "deliver": "now"})
	if isErr || !strings.Contains(text, "sent to 1 of 3") || !strings.Contains(text, "kept for the next turn of "+b) || !strings.Contains(text, "skipped "+c) {
		t.Fatalf("broadcast: %s", text)
	}
	// «now» cuts the running turn (its command is stopped) and the message starts the next one.
	if len(r.convs.sent[a]) != 1 || !strings.Contains(r.convs.sent[a][0], "main is frozen") || !strings.Contains(r.convs.sent[a][0], "interrupted") || r.convs.steers[a][0] {
		t.Fatalf("the working chat got %v", r.convs.sent[a])
	}
	if len(r.convs.stopped) != 1 || r.convs.stopped[0] != a {
		t.Fatalf("interrupted: %v", r.convs.stopped)
	}
	if len(r.convs.sent[b]) != 0 || len(r.convs.sent[c]) != 0 {
		t.Fatalf("a free or stopped chat was woken: %v %v", r.convs.sent[b], r.convs.sent[c])
	}
	// Still free at the next sweep: nothing goes.
	r.svc.Sweep()
	if len(r.convs.sent[b]) != 0 {
		t.Fatal("the held message woke the free chat")
	}
	// Its next turn starts: the message goes into it, once.
	r.convs.mu.Lock()
	rec = r.convs.recs[b]
	rec.State.K = wire.StateWorking
	r.convs.recs[b] = rec
	r.convs.mu.Unlock()
	r.svc.Sweep()
	r.svc.Sweep()
	if got := r.convs.sent[b]; len(got) != 1 || !strings.Contains(got[0], "main is frozen") || !r.convs.steers[b][0] {
		t.Fatalf("the next turn got %v", got)
	}

	// wake: the free chat is started on it.
	r.convs.finish(b, "done again\n\n"+MarkDone)
	if text, isErr := r.call("tab-root", "broadcast_children", map[string]any{"text": "Report what you have", "wake": true}); isErr || !strings.Contains(text, "sent to 2 of 3") {
		t.Fatalf("wake: %s", text)
	}
	if got := r.convs.sent[b]; len(got) != 2 || !strings.Contains(got[1], "Report what you have") {
		t.Fatalf("woken chat got %v", got)
	}
}

// many chats racing for one task: exactly one gets it. A task waits for its dependency.
func TestTaskClaimRaceAndDependencies(t *testing.T) {
	r := newTestRig(t)
	const n = 12
	kids := make([]string, n)
	r.svc.Settings = func() wire.DelegationSettings { return wire.DelegationSettings{MaxChildren: n} }
	for i := range kids {
		kids[i] = r.delegate("tab-root", nil)
	}
	first, err := r.svc.AddTask("tab-root", NewTask{Title: "API"})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := r.svc.AddTask("tab-root", NewTask{Title: "UI", After: []string{first.ID}})

	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for _, k := range kids {
		wg.Go(func() {
			if _, err := r.svc.ClaimTask(k, first.ID, ""); err == nil {
				mu.Lock()
				won++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if won != 1 {
		t.Fatalf("%d chats claimed one task", won)
	}
	idle := ""
	for _, k := range kids {
		if tasks, _ := r.svc.TaskList(k); tasks[0].Owner != k {
			idle = k
			break
		}
	}
	if _, err := r.svc.ClaimTask(idle, second.ID, ""); err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("a task with an open dependency: %v", err)
	}
	// Without id: nothing is free while the first is held.
	if _, err := r.svc.ClaimTask(idle, "", ""); err == nil || !strings.Contains(err.Error(), "none-free") {
		t.Fatalf("next free: %v", err)
	}
	if text, _ := r.call("tab-root", "task_list", nil); !strings.Contains(text, "t2 [waits for t1] UI") || !strings.Contains(text, "t1 [held by") {
		t.Fatalf("list: %s", text)
	}
}

// a check that fails keeps the task with the chat and gives it the output; once it passes the task is done
// and the next one opens. A chat that took part and finished empty-handed gets the next task by itself.
func TestTaskCheckAndNextTask(t *testing.T) {
	r := newTestRig(t)
	pass := false
	r.svc.Check = func(_ context.Context, _, command string) (string, int, error) {
		if pass {
			return "ok", 0, nil
		}
		return "--- FAIL: TestParse\nparse_test.go:12: got 1, want 2", 1, nil
	}
	a := r.delegate("tab-root", map[string]any{"notify": true})
	// A chat that asks before it acts may not set a check: the machine would run it without asking.
	if text, isErr := r.call("tab-root", "task_add", map[string]any{"title": "Parser", "check": "go test ./internal/parse"}); !isErr || !strings.Contains(text, "not-allowed") {
		t.Fatalf("check from an asking chat: %s", text)
	}
	r.svc.Permission = func(string) wire.PermissionMode { return wire.PermissionBypass }
	r.call("tab-root", "task_add", map[string]any{"title": "Parser", "check": "go test ./internal/parse"})
	r.call("tab-root", "task_add", map[string]any{"title": "Docs", "detail": "Write docs/parse.md", "after": []any{"t1"}})

	if text, isErr := r.call(a, "task_claim", nil); isErr || !strings.Contains(text, "claimed t1") {
		t.Fatalf("claim: %s", text)
	}
	text, isErr := r.call(a, "task_done", map[string]any{"id": "t1", "result": "fixed"})
	if isErr || !strings.Contains(text, "failed (exit code 1)") || !strings.Contains(text, "want 2") {
		t.Fatalf("failing check: %s", text)
	}
	if tasks, _ := r.svc.TaskList(a); tasks[0].State != TaskClaimed || tasks[0].Owner != a {
		t.Fatalf("after a failed check: %+v", tasks[0])
	}
	pass = true
	text, _ = r.call(a, "task_done", map[string]any{"id": "t1", "result": "fixed"})
	if !strings.Contains(text, "is done (its check passed)") || !strings.Contains(text, "Next free task: t2") {
		t.Fatalf("passing check: %s", text)
	}

	// The chat ends its turn without taking t2: the platform gives it t2, and the parent is not woken.
	r.convs.finish(a, "Parser fixed.\n\n"+MarkDone)
	r.svc.Observe(mustRecord(t, r.convs, a))
	told := r.convs.told[a]
	if len(told) != 1 || !strings.Contains(told[0], "t2 «Docs»") || !strings.Contains(told[0], "docs/parse.md") {
		t.Fatalf("next task: %v", told)
	}
	if len(r.convs.sent["tab-root"]) != 0 {
		t.Fatalf("the parent was woken while the chat got more work: %v", r.convs.sent["tab-root"])
	}
	if tasks, _ := r.svc.TaskList(a); tasks[1].State != TaskClaimed || tasks[1].Owner != a {
		t.Fatalf("t2: %+v", tasks[1])
	}
	// The plan of the parent's chat shows the team's tasks.
	plan := r.svc.PlanTasks("tab-root", nil)
	if len(plan) != 2 || plan[0].Status != wire.TaskCompleted || plan[1].Status != wire.TaskInProgress {
		t.Fatalf("plan: %+v", plan)
	}
}

// A check slower than the call: task_done answers at once, and the result reaches the chat as a message.
func TestSlowCheckComesAsMessage(t *testing.T) {
	r := newTestRig(t)
	release := make(chan struct{})
	r.svc.Check = func(context.Context, string, string) (string, int, error) {
		<-release
		return "boom", 2, nil
	}
	a := r.delegate("tab-root", nil)
	r.svc.Permission = func(string) wire.PermissionMode { return wire.PermissionBypass }
	if _, err := r.svc.AddTask("tab-root", NewTask{Title: "Slow", Check: "make e2e"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.ClaimTask(a, "t1", ""); err != nil {
		t.Fatal(err)
	}
	o, err := r.svc.DoneTask(context.Background(), a, "t1", "", 10*time.Millisecond)
	if err != nil || !o.Pending {
		t.Fatalf("slow check: %+v %v", o, err)
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.convs.mu.Lock()
		told := r.convs.told[a]
		r.convs.mu.Unlock()
		if len(told) == 1 {
			if !strings.Contains(told[0], "failed (exit code 2)") || !strings.Contains(told[0], "boom") {
				t.Fatalf("message: %s", told[0])
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the result of the slow check never came")
}

// a chat started with plan_first is marked to plan; its plan goes to the parent; approval takes the mark away
// and tells it to go on. HRN-1530: a chat makes itself a coordinator and back.
func TestPlanFirstAndCoordinator(t *testing.T) {
	r := newTestRig(t)
	marks := map[string]chatrole.Role{}
	var mu sync.Mutex
	r.svc.Roles = func(conv string, role chatrole.Role) {
		mu.Lock()
		defer mu.Unlock()
		marks[conv] = role
	}
	a := r.delegate("tab-root", map[string]any{"plan_first": true, "notify": true})
	if marks[a] != chatrole.Plan {
		t.Fatalf("mark: %q", marks[a])
	}
	if first := r.convs.feeds[a][0].Text; !strings.Contains(first, "submit_plan") {
		t.Fatalf("the task does not say how to plan: %s", first)
	}
	text, isErr := r.call(a, "submit_plan", map[string]any{"plan": "1. change parse.go\n2. go test ./internal/parse"})
	if isErr || !strings.Contains(text, MarkQuestion) {
		t.Fatalf("submit: %s", text)
	}
	if got := r.convs.sent["tab-root"]; len(got) != 1 || !strings.Contains(got[0], "change parse.go") || !strings.Contains(got[0], "approve_plan") {
		t.Fatalf("parent got %v", got)
	}
	// Its turn ends on the plan: the parent is not woken a second time with the same plan.
	r.convs.finish(a, "Waiting for the plan's approval.\n\n"+MarkQuestion)
	r.svc.Observe(mustRecord(t, r.convs, a))
	if len(r.convs.sent["tab-root"]) != 1 {
		t.Fatalf("woken twice: %v", r.convs.sent["tab-root"])
	}
	if text, isErr := r.call("tab-root", "approve_plan", map[string]any{"id": a, "approve": false}); !isErr || !strings.Contains(text, "feedback") {
		t.Fatalf("send back without words: %s", text)
	}
	if text, isErr := r.call("tab-root", "approve_plan", map[string]any{"id": a, "approve": true}); isErr {
		t.Fatal(text)
	}
	if marks[a] != chatrole.None || r.svc.RoleOf(a) != chatrole.None {
		t.Fatalf("after approval: %q", marks[a])
	}
	if got := r.convs.sent[a]; len(got) != 1 || !strings.Contains(got[0], "approved") {
		t.Fatalf("child got %v", got)
	}

	if text, isErr := r.call("tab-root", "coordinate_mode", map[string]any{"on": true}); isErr || marks["tab-root"] != chatrole.Coordinate {
		t.Fatalf("coordinate: %s %q", text, marks["tab-root"])
	}
	r.svc.Sweep()
	if text, isErr := r.call("tab-root", "coordinate_mode", map[string]any{"on": false}); isErr || marks["tab-root"] != chatrole.None {
		t.Fatalf("work again: %s %q", text, marks["tab-root"])
	}
}

// wait_conversation id "all" waits for every delegated chat in one call and answers each.
func TestWaitAll(t *testing.T) {
	r := newTestRig(t)
	a := r.delegate("tab-root", nil)
	b := r.delegate("tab-root", nil)
	r.convs.finish(a, "A done\n\n"+MarkDone)
	go func() {
		time.Sleep(50 * time.Millisecond)
		r.convs.finish(b, "B done\n\n"+MarkDone)
	}()
	text, isErr := r.call("tab-root", "wait_conversation", map[string]any{"id": "all", "timeout_s": 5})
	if isErr || !strings.Contains(text, "A done") || !strings.Contains(text, "B done") {
		t.Fatalf("wait all: %s", text)
	}
	if text, isErr := r.call(a, "wait_conversation", map[string]any{"id": "all"}); !isErr {
		t.Fatalf("a chat that delegated nothing: %s", text)
	}
}

// A chat cuts only the turns of chats it delegated: send_to_conversation refuses «interrupt», and a child's message to a
// sibling never interrupts it.
func TestInterruptOnlyDownward(t *testing.T) {
	r := newTestRig(t)
	a := r.delegate("tab-root", nil)
	b := r.delegate("tab-root", nil)
	if text, isErr := r.call("tab-root", "send_to_conversation", map[string]any{"id": a, "text": "x", "deliver": "interrupt"}); !isErr {
		t.Fatalf("interrupt by send: %s", text)
	}
	if _, err := r.svc.send(context.Background(), a, b, "stop", DeliverInterrupt); err != nil {
		t.Fatal(err)
	}
	if len(r.convs.stopped) != 0 {
		t.Fatalf("a sibling was interrupted: %v", r.convs.stopped)
	}
}

// a delegated chat starts lean unless its parent asks for the whole context; other chats start as ever.
func TestLeanStart(t *testing.T) {
	r := newTestRig(t)
	a := r.delegate("tab-root", nil)
	b := r.delegate("tab-root", map[string]any{"full_context": true})
	if !r.svc.Lean(a) || r.svc.Lean(b) || r.svc.Lean("tab-root") {
		t.Fatalf("lean: %v %v %v", r.svc.Lean(a), r.svc.Lean(b), r.svc.Lean("tab-root"))
	}
}

// A chat that claimed its task by name is not handed the others: in the measurement it gave them back again and again.
func TestNamedClaimGetsNoNextTask(t *testing.T) {
	r := newTestRig(t)
	a := r.delegate("tab-root", map[string]any{"notify": true})
	r.call("tab-root", "task_add", map[string]any{"title": "Parser"})
	r.call("tab-root", "task_add", map[string]any{"title": "Docs"})
	if text, isErr := r.call(a, "task_claim", map[string]any{"id": "t1"}); isErr {
		t.Fatal(text)
	}
	if text, isErr := r.call(a, "task_done", map[string]any{"id": "t1", "result": "ok"}); isErr {
		t.Fatal(text)
	}
	r.convs.finish(a, "done\n\n"+MarkDone)
	r.svc.Observe(mustRecord(t, r.convs, a))
	if len(r.convs.told[a]) != 0 {
		t.Fatalf("handed another task: %v", r.convs.told[a])
	}
	if tasks, _ := r.svc.TaskList(a); tasks[1].State != TaskOpen {
		t.Fatalf("t2: %+v", tasks[1])
	}
}

// Codex is told to delegate splits instead of using its own spawn_agent; Claude is not told this.
func TestCodexDelegationLine(t *testing.T) {
	if !strings.Contains(Tools{Provider: wire.ProviderCodex}.Instructions(), "not to spawn_agent") ||
		strings.Contains(Tools{Provider: wire.ProviderClaude}.Instructions(), "spawn_agent") {
		t.Fatal("instructions by provider")
	}
}
