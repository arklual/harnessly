package delegation

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
	"github.com/ly-solutions/harnessly/internal/peers"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// fakeConvs is the machine's conversations with scripted agents: a created chat starts working on its first message,
// and the test finishes its turns.
type fakeConvs struct {
	mu       sync.Mutex
	recs     map[string]wire.Conversation
	order    []string
	feeds    map[string][]wire.Entry
	turns    map[string][]wire.Turn
	sent     map[string][]string // the texts DeliverPeer queued, by conversation
	projects []wire.Project
	n        int
	now      func() time.Time
}

func newFake(now func() time.Time) *fakeConvs {
	return &fakeConvs{recs: map[string]wire.Conversation{}, feeds: map[string][]wire.Entry{}, turns: map[string][]wire.Turn{},
		sent: map[string][]string{}, now: now}
}

func (f *fakeConvs) add(rec wire.Conversation) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if rec.State.K == "" {
		rec.State.K = wire.StateIdle
	}
	f.recs[rec.ID] = rec
	f.order = append(f.order, rec.ID)
}

func (f *fakeConvs) Record(id string) (wire.Conversation, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.recs[id]
	return r, ok
}

func (f *fakeConvs) Conversations(context.Context) (wire.ConvsSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := wire.ConvsSnapshot{Projects: f.projects}
	for _, id := range f.order {
		if r, ok := f.recs[id]; ok {
			out.Conversations = append(out.Conversations, r)
		}
	}
	return out, nil
}

func (f *fakeConvs) Conversation(_ context.Context, id string) (wire.ConvSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return wire.ConvSnapshot{Entries: f.feeds[id], Turns: f.turns[id]}, nil
}

func (f *fakeConvs) ConversationAfter(_ context.Context, id, after string) (wire.ConvSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	es := f.feeds[id]
	for i, e := range es {
		if e.ID == after {
			return wire.ConvSnapshot{Entries: es[i+1:]}, nil
		}
	}
	return wire.ConvSnapshot{}, context.Canceled
}

func (f *fakeConvs) entry(id string, e wire.Entry) {
	f.n++
	e.ID = "e" + strconv.Itoa(f.n)
	f.feeds[id] = append(f.feeds[id], e)
}

func (f *fakeConvs) Do(_ context.Context, _ string, cmd wire.Command) wire.CmdResult {
	c, ok := cmd.(wire.ConversationCreateCmd)
	if !ok {
		return wire.Refused(wire.RefuseUnsupported)
	}
	if err := c.Validate(); err != nil {
		return wire.Refused(wire.RefuseBadRequest)
	}
	f.mu.Lock()
	f.n++
	id := "tab-c" + strconv.Itoa(f.n)
	parent := c.ParentID
	title := c.Title
	if title == "" {
		title = c.First.Text
	}
	rec := wire.Conversation{ID: id, Cwd: c.Cwd, Provider: c.Provider, Model: c.Model, Title: title, ParentID: &parent,
		CreatedAt: wire.Ms(f.now().UnixMilli()), State: wire.AgentState{K: wire.StateWorking}}
	f.entry(id, wire.Entry{T: wire.EntryUser, Text: c.First.Text})
	f.mu.Unlock()
	f.add(rec)
	return wire.CmdResult{R: wire.ResultAccepted, ID: &id}
}

func (f *fakeConvs) DeliverPeer(_ context.Context, conv, clientID, text string, _ bool) wire.CmdResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.recs[conv]
	if !ok || !strings.HasPrefix(clientID, "peer-") {
		return wire.Refused(wire.RefuseNotFound)
	}
	f.sent[conv] = append(f.sent[conv], text)
	f.entry(conv, wire.Entry{T: wire.EntryUser, Text: text})
	rec.State.K = wire.StateWorking
	f.recs[conv] = rec
	return wire.CmdResult{R: wire.ResultAccepted}
}

// finish ends the chat's turn with an answer, as its agent would.
func (f *fakeConvs) finish(id, answer string) wire.Conversation {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entry(id, wire.Entry{T: wire.EntryTool, Tool: "Bash", Summary: "go test ./...", OK: new(bool),
		Output: &wire.ToolOutput{Text: "FAIL parse_test.go:12"}})
	f.entry(id, wire.Entry{T: wire.EntrySay, MD: answer})
	es := f.feeds[id]
	answerID := es[len(es)-1].ID
	f.n++
	turnID := "t" + strconv.Itoa(f.n)
	f.turns[id] = append(f.turns[id], wire.Turn{ID: turnID, AnswerEntryID: &answerID})
	end := wire.Ms(f.now().UnixMilli())
	rec := f.recs[id]
	rec.State.K = wire.StateIdle
	rec.LastTurn = &wire.TurnSummary{ID: turnID, Status: wire.TurnDone, EndedAt: &end, Snippet: answer}
	f.recs[id] = rec
	return rec
}

type rig struct {
	t     *testing.T
	convs *fakeConvs
	svc   *Service
	clock time.Time
	mu    sync.Mutex
	rpcID int
}

func newTestRig(t *testing.T) *rig {
	r := &rig{t: t, clock: time.UnixMilli(1_700_000_000_000)}
	now := func() time.Time {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.clock = r.clock.Add(time.Millisecond)
		return r.clock
	}
	r.convs = newFake(now)
	r.convs.add(wire.Conversation{ID: "tab-root", Title: "Plan the release", Cwd: "/w", Provider: wire.ProviderClaude})
	r.svc = &Service{Convs: r.convs, Book: &Book{Path: filepath.Join(t.TempDir(), "delegations.json")}, Now: now,
		Self: peers.Sender{Machine: "dev-box", PersonName: "Ann"}, Poll: time.Millisecond,
		Models: func(p wire.Provider) []wire.AgentModel {
			if p == wire.ProviderCodex {
				return []wire.AgentModel{{ID: "gpt-5.5", Efforts: []wire.Effort{wire.EffortLow, wire.EffortHigh}}}
			}
			return []wire.AgentModel{{ID: "opus"}, {ID: "sonnet"}}
		}}
	return r
}

// call is one tools/call through the MCP server as the agent of caller makes it: the text of its answer and isError.
func (r *rig) call(caller, tool string, args map[string]any) (string, bool) {
	r.t.Helper()
	r.rpcID++
	a := jsvalue.NewObject()
	for k, v := range args {
		a.Set(k, v)
	}
	msg := jsvalue.NewObject("jsonrpc", "2.0", "id", float64(r.rpcID), "method", "tools/call",
		"params", jsvalue.NewObject("name", tool, "arguments", a))
	srv := &mcpserver.Server{Name: "harnessly", Sets: []mcpserver.Toolset{Tools{S: r.svc, Caller: caller, Provider: wire.ProviderClaude}}}
	status, body := srv.Handle(context.Background(), msg)
	if status != 200 {
		r.t.Fatalf("%s: status %d", tool, status)
	}
	v, err := jsvalue.Parse(body)
	if err != nil {
		r.t.Fatal(err)
	}
	res, ok := v.(*jsvalue.Object).Get("result")
	if !ok {
		r.t.Fatalf("%s: %s", tool, body)
	}
	o := res.(*jsvalue.Object)
	content := o.Array("content")
	text := content[0].(*jsvalue.Object).String("text")
	isErr, _ := o.Get("isError")
	return text, isErr == true
}

func idOf(t *testing.T, text string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if id, ok := strings.CutPrefix(line, "conversationId: "); ok {
			return id
		}
	}
	t.Fatalf("no conversation id in %q", text)
	return ""
}

// The whole round: an agent delegates to Codex, lists, waits for the answer, reads the chat, gives more work and is
// woken with the result.
func TestDelegateWaitReadSendWake(t *testing.T) {
	r := newTestRig(t)
	initialized, body := (&mcpserver.Server{Name: "harnessly", Sets: []mcpserver.Toolset{Tools{S: r.svc}}}).Handle(context.Background(),
		jsvalue.NewObject("jsonrpc", "2.0", "id", 1.0, "method", "tools/list"))
	if initialized != 200 || !strings.Contains(string(body), `"delegate_task"`) || !strings.Contains(string(body), `"list_children"`) {
		t.Fatalf("tools/list: %s", body)
	}

	text, isErr := r.call("tab-root", "delegate_task", map[string]any{"provider": "codex", "model": "gpt-5.5", "effort": "high",
		"prompt": "Find why parse_test fails", "title": "Parser tests", "notify": true})
	if isErr {
		t.Fatal(text)
	}
	child := idOf(t, text)
	rec, _ := r.convs.Record(child)
	if rec.ParentID == nil || *rec.ParentID != "tab-root" || rec.Provider != wire.ProviderCodex || rec.Model.ID != "gpt-5.5" ||
		*rec.Model.Effort != wire.EffortHigh || rec.Title != "Parser tests" || rec.Cwd != "/w" {
		t.Fatalf("child: %+v", rec)
	}

	if text, _ := r.call("tab-root", "list_children", nil); !strings.Contains(text, child) || !strings.Contains(text, "working") {
		t.Fatalf("list: %s", text)
	}

	// Still working: the wait times out.
	if text, _ := r.call("tab-root", "wait_conversation", map[string]any{"id": child, "timeout_s": 1.0}); !strings.Contains(text, "outcome: timeout") {
		t.Fatalf("wait while working: %s", text)
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		r.convs.finish(child, "The test compares maps in order; fixed with sorted keys.")
	}()
	text, _ = r.call("tab-root", "wait_conversation", map[string]any{"id": child, "timeout_s": 5.0})
	if !strings.Contains(text, "outcome: done") || !strings.Contains(text, "sorted keys") {
		t.Fatalf("wait: %s", text)
	}

	text, _ = r.call("tab-root", "read_conversation", map[string]any{"id": child})
	for _, want := range []string{"[person] Find why parse_test fails", "[tool Bash] go test ./... (failed)", "FAIL parse_test.go:12", "[agent] The test compares", "next: "} {
		if !strings.Contains(text, want) {
			t.Fatalf("read lacks %q:\n%s", want, text)
		}
	}

	// The finished turn wakes the parent once, with the answer, as a message of the child's agent.
	r.svc.Observe(mustRecord(t, r.convs, child))
	r.svc.Observe(mustRecord(t, r.convs, child))
	woken := r.convs.sent["tab-root"]
	if len(woken) != 1 {
		t.Fatalf("wake-ups: %d", len(woken))
	}
	env, ok := peers.Parse(woken[0])
	if !ok || env.Ref != "dev-box/"+child || env.Chat != "Parser tests" || !strings.Contains(env.Text, "sorted keys") {
		t.Fatalf("wake-up: %+v", env)
	}

	// More work: queued as the parent's agent's message, and the next wait waits for the turn after it.
	if text, isErr := r.call("tab-root", "send_to_conversation", map[string]any{"id": child, "text": "Now run the whole suite"}); isErr {
		t.Fatal(text)
	}
	env, ok = peers.Parse(r.convs.sent[child][0])
	if !ok || env.Ref != "dev-box/tab-root" || env.Text != "Now run the whole suite" || env.Chat != "Plan the release" {
		t.Fatalf("sent: %+v", env)
	}
	if text, _ := r.call("tab-root", "wait_conversation", map[string]any{"id": child, "timeout_s": 1.0}); !strings.Contains(text, "outcome: timeout") {
		t.Fatalf("the old turn answered the new message: %s", text)
	}
}

func mustRecord(t *testing.T, f *fakeConvs, id string) wire.Conversation {
	t.Helper()
	rec, ok := f.Record(id)
	if !ok {
		t.Fatalf("no %s", id)
	}
	return rec
}

// Limits: depth, the number of chats, whose chats an agent may touch, an unknown model, an unknown caller.
func TestDelegationLimits(t *testing.T) {
	r := newTestRig(t)
	delegate := func(caller string) (string, bool) {
		return r.call(caller, "delegate_task", map[string]any{"provider": "claude", "prompt": "do it"})
	}
	text, _ := delegate("tab-root")
	child := idOf(t, text)
	text, _ = delegate(child)
	grandchild := idOf(t, text)
	if text, isErr := delegate(grandchild); !isErr || !strings.Contains(text, "too-deep") {
		t.Fatalf("third level: %s", text)
	}
	// The root reads its grandchild; the grandchild cannot read the root, nor a sibling's chat.
	if text, isErr := r.call("tab-root", "read_conversation", map[string]any{"id": grandchild}); isErr {
		t.Fatalf("grandchild: %s", text)
	}
	r.convs.add(wire.Conversation{ID: "tab-other", Title: "The person's own", Provider: wire.ProviderClaude})
	for _, target := range []string{"tab-root", "tab-other"} {
		if text, isErr := r.call(child, "send_to_conversation", map[string]any{"id": target, "text": "hi"}); !isErr || !strings.Contains(text, "not-allowed") {
			t.Fatalf("send to %s: %s", target, text)
		}
	}
	r.svc.Settings = func() wire.DelegationSettings { return wire.DelegationSettings{MaxChildren: 2, AnyChat: true} }
	if text, isErr := r.call(child, "send_to_conversation", map[string]any{"id": "tab-other", "text": "hi"}); isErr {
		t.Fatalf("the person let agents write anywhere: %s", text)
	}
	if text, isErr := r.call(child, "send_to_conversation", map[string]any{"id": child, "text": "hi"}); !isErr || !strings.Contains(text, "own chat") {
		t.Fatalf("to itself: %s", text)
	}
	delegate("tab-root")
	if text, isErr := delegate("tab-root"); !isErr || !strings.Contains(text, "too-many") {
		t.Fatalf("third child of two allowed: %s", text)
	}
	r.svc.Settings = nil
	if text, isErr := r.call("tab-root", "delegate_task", map[string]any{"provider": "codex", "model": "gpt-9", "prompt": "x"}); !isErr || !strings.Contains(text, "gpt-5.5") {
		t.Fatalf("unknown model: %s", text)
	}
	if text, isErr := r.call("tab-root", "delegate_task", map[string]any{"provider": "codex", "model": "gpt-5.5", "effort": "max", "prompt": "x"}); !isErr || !strings.Contains(text, "low, high") {
		t.Fatalf("unknown effort: %s", text)
	}
	if text, isErr := r.call("", "list_children", nil); !isErr || !strings.Contains(text, "unknown-caller") {
		t.Fatalf("no caller: %s", text)
	}
}

// A chat wakes its parent at most MaxWakes times, and a chat whose parent did not ask never.
func TestDelegatedFolderIsChecked(t *testing.T) {
	r := newTestRig(t)
	base := t.TempDir()
	mk := func(name string) string {
		dir := filepath.Join(base, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	own, project, outside := mk("own"), mk("project"), mk("outside")
	sub := mk("own/sub")
	if err := os.Symlink(outside, filepath.Join(own, "escape")); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(own, "notes.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r.convs.add(wire.Conversation{ID: "tab-here", Title: "Here", Cwd: own, Provider: wire.ProviderClaude})
	r.convs.projects = []wire.Project{{ID: "p1", Path: project}}
	delegate := func(cwd string) (string, bool) {
		return r.call("tab-here", "delegate_task", map[string]any{"provider": "claude", "prompt": "do it", "cwd": cwd})
	}
	for _, cwd := range []string{sub, own + "/sub/../sub", project, filepath.Join(project)} {
		if text, isErr := delegate(cwd); isErr {
			t.Fatalf("%s: %s", cwd, text)
		}
	}
	for cwd, want := range map[string]string{
		"sub":                               "absolute",
		outside:                             "outside",
		filepath.Join(own, "escape"):        "outside", // a link out of the folder is where it leads
		filepath.Join(own, "missing"):       "does not exist",
		file:                                "not a folder",
		filepath.Join(own, "..", "outside"): "outside",
	} {
		if text, isErr := delegate(cwd); !isErr || !strings.Contains(text, "bad-cwd") || !strings.Contains(text, want) {
			t.Fatalf("%s: %s", cwd, text)
		}
	}
	r.svc.Settings = func() wire.DelegationSettings { return wire.DelegationSettings{MaxChildren: 10, AnyChat: true} }
	if text, isErr := delegate(outside); isErr {
		t.Fatalf("the person let agents anywhere: %s", text)
	}
	if text, isErr := delegate("relative"); !isErr || !strings.Contains(text, "absolute") {
		t.Fatalf("relative even so: %s", text)
	}
}

func TestWakeUpsAreBounded(t *testing.T) {
	r := newTestRig(t)
	text, _ := r.call("tab-root", "delegate_task", map[string]any{"provider": "claude", "prompt": "a", "notify": true})
	loud := idOf(t, text)
	text, _ = r.call("tab-root", "delegate_task", map[string]any{"provider": "claude", "prompt": "b"})
	quiet := idOf(t, text)
	r.svc.Observe(r.convs.finish(quiet, "done quietly"))
	for i := range MaxWakes + 3 {
		r.svc.Observe(r.convs.finish(loud, "answer "+strconv.Itoa(i)))
	}
	woken := r.convs.sent["tab-root"]
	if len(woken) != MaxWakes {
		t.Fatalf("wake-ups: %d", len(woken))
	}
	if last := woken[len(woken)-1]; !strings.Contains(last, "will not wake you again") {
		t.Fatalf("the last wake-up says it is the last: %s", last)
	}
	// The book survives a restart.
	again := &Book{Path: r.svc.Book.Path}
	if p, ok := again.Get(loud); !ok || p.Wakes != MaxWakes || !p.Notify {
		t.Fatalf("book: %+v", p)
	}
}
