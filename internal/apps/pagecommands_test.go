package apps

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// commandRig is the page tools with a journal on the disk, a clock the test moves and a window that offers `fill`.
type commandRig struct {
	t     *testing.T
	path  string
	mu    sync.Mutex
	now   time.Time
	pages *PageTools
}

func newCommandRig(t *testing.T) *commandRig {
	t.Helper()
	r := &commandRig{t: t, path: filepath.Join(t.TempDir(), PageCommandsFile), now: time.UnixMilli(1_800_000_000_000)}
	r.start()
	return r
}

// start is a runtime's start: new page tools over the same journal. The window reports again, as its page does.
func (r *commandRig) start() {
	r.t.Helper()
	if r.pages != nil {
		if err := r.pages.CloseCommands(); err != nil {
			r.t.Fatal(err)
		}
	}
	r.pages = NewPageTools()
	r.pages.Now = r.clock
	if err := r.pages.OpenCommands(r.path); err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = r.pages.CloseCommands() })
	r.screen("/form")
}

// clock is the page tools' clock; calls read it from their own goroutines while the test moves it.
func (r *commandRig) clock() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.now
}

func (r *commandRig) advance(d time.Duration) {
	r.mu.Lock()
	r.now = r.now.Add(d)
	r.mu.Unlock()
}

func (r *commandRig) screen(route string) {
	r.t.Helper()
	ctx := json.RawMessage(`{"screen":{"title":"Форма","route":"` + route + `"}}`)
	if err := r.pages.Report("tab-1", []PageReport{{App: "acme.form", Tools: []PageTool{{Name: "fill"}}, Context: ctx}}); err != nil {
		r.t.Fatal(err)
	}
}

type callEnd struct {
	answer PageAnswer
	err    error
}

// call makes the agent's call in the background; key is its idempotency key ("" for none).
func (r *commandRig) call(ctx context.Context, key string) chan callEnd {
	out := make(chan callEnd, 1)
	pages := r.pages
	go func() {
		a, err := pages.Call(WithCallKey(ctx, key, false), "tab-1", "acme.form", "fill", json.RawMessage(`{"name":"Ann"}`))
		out <- callEnd{a, err}
	}()
	return out
}

// taken waits for the page's next take to bring a command.
func (r *commandRig) taken(page string) PageCall {
	r.t.Helper()
	for i := 0; i < 400; i++ {
		if calls := r.pages.TakeBy("tab-1", page, page != ""); len(calls) == 1 {
			return calls[0]
		} else if len(calls) > 1 {
			r.t.Fatalf("calls: %+v", calls)
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.t.Fatal("the page was given no command")
	return PageCall{}
}

func (r *commandRig) none(page string) {
	r.t.Helper()
	if calls := r.pages.TakeBy("tab-1", page, page != ""); len(calls) != 0 {
		r.t.Fatalf("the page was given %+v", calls)
	}
}

func (r *commandRig) status(id string) string {
	r.t.Helper()
	c, ok := r.pages.Command("tab-1", id)
	if !ok {
		r.t.Fatalf("no command %s", id)
	}
	return c.Status
}

// made waits until the page tools hold n commands.
func made(t *testing.T, pages *PageTools, n int) {
	t.Helper()
	for i := 0; i < 1000; i++ {
		pages.mu.Lock()
		have := len(pages.cmds)
		pages.mu.Unlock()
		if have == n {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("the page tools do not hold %d commands", n)
}

func ended(t *testing.T, ch chan callEnd) callEnd {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("the call did not end")
		return callEnd{}
	}
}

// The runtime restarts in the middle of a call. The command is in the journal: the new runtime hands it to the page
// again under the same id, the agent's call, made again by its proxy with the same key, waits for that command and
// gets the window's answer, and no second command is made.
func TestCommandSurvivesRestart(t *testing.T) {
	r := newCommandRig(t)
	ctx, cut := context.WithCancel(context.Background())
	first := r.call(ctx, "key-1")
	got := r.taken("page-a")
	if got.Tool != "fill" || string(got.Arguments) != `{"name":"Ann"}` || got.Attempt != 1 || got.ExpiresAt == 0 {
		t.Fatalf("the command: %+v", got)
	}
	if r.status(got.ID) != CommandSent {
		t.Fatalf("status: %s", r.status(got.ID))
	}
	// The runtime goes down: the agent's connection ends, the page's acknowledgement never arrives.
	cut()
	if e := ended(t, first); !errors.Is(e.err, context.Canceled) {
		t.Fatalf("the first call: %+v", e)
	}
	r.advance(8 * time.Second)
	r.start()
	if r.status(got.ID) != CommandSent {
		t.Fatalf("after the restart: %s", r.status(got.ID))
	}
	again := make(chan callEnd, 1)
	go func() {
		a, _, err, found := r.pages.Resume(WithCallKey(context.Background(), "key-1", true), "tab-1")
		if !found {
			err = errors.New("the command of the key was not found")
		}
		again <- callEnd{a, err}
	}()
	back := r.taken("page-a")
	if back.ID != got.ID || back.Attempt != 2 {
		t.Fatalf("handed out again: %+v, was %s", back, got.ID)
	}
	if status, dead, err := r.pages.Ack("tab-1", back.ID, "page-a"); err != nil || dead || status != CommandAcked {
		t.Fatalf("ack: %s %v %v", status, dead, err)
	}
	if err := r.pages.Answer("tab-1", back.ID, PageAnswer{Result: json.RawMessage(`{"ok":true}`)}); err != nil {
		t.Fatal(err)
	}
	if e := ended(t, again); e.err != nil || string(e.answer.Result) != `{"ok":true}` {
		t.Fatalf("the repeated call: %+v", e)
	}
	// Done once: nothing more for the page, and the key answers from the journal, after one more restart too.
	r.advance(time.Minute)
	r.none("page-a")
	r.start()
	a, c, err, found := r.pages.Resume(WithCallKey(context.Background(), "key-1", true), "tab-1")
	if !found || err != nil || string(a.Result) != `{"ok":true}` || c.Status != CommandDone || c.ID != got.ID || c.Deliveries != 2 {
		t.Fatalf("from the journal: %+v %+v %v %v", a, c, err, found)
	}
	r.none("page-a")
}

// A command the page took and did not acknowledge is handed out again with the same id after PageCallResend; one it
// acknowledged, after PageCallReack. Another page of the chat gets it only when the first is silent. A page that does
// not acknowledge (an older one) takes a command once, and nobody is given it again.
func TestCommandHandedOutAgainWithoutAck(t *testing.T) {
	r := newCommandRig(t)
	end := r.call(context.Background(), "")
	got := r.taken("page-a")
	r.advance(PageCallResend - time.Second)
	r.none("page-a")
	r.advance(2 * time.Second)
	// Page b is not given what page a holds while a still asks.
	r.none("page-b")
	if back := r.taken("page-a"); back.ID != got.ID || back.Attempt != 2 {
		t.Fatalf("again: %+v", back)
	}
	if _, dead, err := r.pages.Ack("tab-1", got.ID, "page-a"); err != nil || dead {
		t.Fatal(dead, err)
	}
	r.advance(PageCallResend + time.Second)
	r.none("page-a")
	// Page a is gone (reloaded): after PageCallReack page b takes the command over.
	r.advance(PageCallReack)
	if back := r.taken("page-b"); back.ID != got.ID || back.Attempt != 3 {
		t.Fatalf("taken over: %+v", back)
	}
	if err := r.pages.Answer("tab-1", got.ID, PageAnswer{Result: json.RawMessage(`1`)}); err != nil {
		t.Fatal(err)
	}
	if e := ended(t, end); e.err != nil || string(e.answer.Result) != `1` {
		t.Fatalf("the call: %+v", e)
	}
	// A second answer of the same command changes nothing and is no error.
	if err := r.pages.Answer("tab-1", got.ID, PageAnswer{Result: json.RawMessage(`2`)}); err != nil {
		t.Fatal(err)
	}
	if c, _ := r.pages.Command("tab-1", got.ID); string(c.Result) != `1` {
		t.Fatalf("the result: %s", c.Result)
	}

	// An older page.
	r.screen("/form")
	old := r.call(context.Background(), "")
	var legacy PageCall
	for i := 0; i < 400 && legacy.ID == ""; i++ {
		if calls := r.pages.Take("tab-1"); len(calls) == 1 {
			legacy = calls[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	if legacy.ID == "" {
		t.Fatal("the older page was given no command")
	}
	r.advance(PageCallReack + PageCallResend)
	r.none("")
	r.none("page-b")
	if err := r.pages.Answer("tab-1", legacy.ID, PageAnswer{Error: "boom"}); err != nil {
		t.Fatal(err)
	}
	if e := ended(t, old); e.err != nil || e.answer.Error != "boom" || r.status(legacy.ID) != CommandFailed {
		t.Fatalf("the older page's call: %+v %s", e, r.status(legacy.ID))
	}
}

// A command whose term ran out is expired and never run: it is not handed out, the page that held it is told it is
// dead, and an answer that comes afterwards is dropped.
func TestCommandExpires(t *testing.T) {
	r := newCommandRig(t)
	end := r.call(context.Background(), "")
	got := r.taken("page-a")
	r.advance(PageCallTTL + time.Second)
	r.none("page-a")
	if e := ended(t, end); !errors.Is(e.err, ErrPageCallTimeout) {
		t.Fatalf("the call: %+v", e)
	}
	if status, dead, err := r.pages.Ack("tab-1", got.ID, "page-a"); err != nil || !dead || status != CommandExpired {
		t.Fatalf("ack of an expired command: %s %v %v", status, dead, err)
	}
	if err := r.pages.Answer("tab-1", got.ID, PageAnswer{Result: json.RawMessage(`1`)}); !errors.Is(err, ErrPageCallDead) {
		t.Fatalf("an answer after the end: %v", err)
	}
	if c, _ := r.pages.Command("tab-1", got.ID); c.Status != CommandExpired || len(c.Result) != 0 {
		t.Fatalf("the command: %+v", c)
	}
	// A command that expired while the runtime was away is expired at the start, not delivered.
	r.screen("/form")
	late := r.call(context.Background(), "key-late")
	lateCall := r.taken("page-a")
	_ = late
	r.advance(PageCallTTL + time.Second)
	r.start()
	r.none("page-a")
	if r.status(lateCall.ID) != CommandExpired {
		t.Fatalf("after the restart: %s", r.status(lateCall.ID))
	}
}

// The agent was told the call failed (the term ran out with the page holding the command, the person looking at a
// confirm dialog): the command is dead. The page's late acknowledgement and answer do not bring it back.
func TestCommandDeadAfterRefusal(t *testing.T) {
	enabled := []string{"acme.form"}
	h, pages := windowRig(t, &enabled)
	// Long enough for the call to be taken before it expires on a loaded runner under -race (120 ms was not, GitHub CI).
	pages.CallTTL = 600 * time.Millisecond
	if err := pages.OpenCommands(filepath.Join(t.TempDir(), PageCommandsFile)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pages.CloseCommands() })
	if err := pages.Report("tab-1", []PageReport{{App: "acme.form", Tools: []PageTool{{Name: "fill"}}}}); err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() {
		text, _ := call(t, h, "page_call", "tool", "fill")
		done <- text
	}()
	var got []PageCall
	for i := 0; i < 1000 && len(got) == 0; i++ {
		got = pages.TakeBy("tab-1", "page-a", true)
		time.Sleep(2 * time.Millisecond)
	}
	if len(got) != 1 {
		t.Fatalf("calls: %+v", got)
	}
	if _, dead, _ := pages.Ack("tab-1", got[0].ID, "page-a"); dead {
		t.Fatal("dead too early")
	}
	text := <-done
	for _, want := range []string{"refused: expired:", "will not run later"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the agent's refusal lacks %q: %s", want, text)
		}
	}
	if _, dead, _ := pages.Ack("tab-1", got[0].ID, "page-a"); !dead {
		t.Fatal("the page was not told the command is dead")
	}
	if err := pages.Answer("tab-1", got[0].ID, PageAnswer{Result: json.RawMessage(`{"ok":true}`)}); !errors.Is(err, ErrPageCallDead) {
		t.Fatalf("a late answer: %v", err)
	}
	if c, _ := pages.Command("tab-1", got[0].ID); c.Status != CommandExpired {
		t.Fatalf("status: %s", c.Status)
	}
}

// The window moved to another screen before it took the command: the command is rejected, not run, and the agent is
// told which screen is open.
func TestCommandRejectedOnAnotherScreen(t *testing.T) {
	r := newCommandRig(t)
	end := r.call(context.Background(), "")
	made(t, r.pages, 1)
	r.screen("/list")
	r.none("page-a")
	e := ended(t, end)
	var rejected ErrPageCallRejected
	if !errors.As(e.err, &rejected) || !strings.Contains(rejected.Why, "shows the screen /list now") || !strings.Contains(rejected.Why, "made for the screen /form") {
		t.Fatalf("the call: %+v", e)
	}
	if text := jsvalue.Stringify(pageCallResult("acme.form", PageTool{Name: "fill"}, e.answer, e.err), ""); !strings.Contains(string(text), "refused: rejected: the window of acme.form shows the screen /list now") {
		t.Fatalf("the agent's words: %s", text)
	}
}

// The agent's proxy makes a call again with the same key: the host's tools find the command and answer what it
// answered, and make no second command. A repeated call nothing remembers is not run: its answer is lost.
func TestRepeatedCallFindsItsCommand(t *testing.T) {
	enabled := []string{"acme.form"}
	h, pages := windowRig(t, &enabled)
	if err := pages.Report("tab-1", []PageReport{{App: "acme.form", Tools: []PageTool{{Name: "fill"}}}}); err != nil {
		t.Fatal(err)
	}
	by := func(ctx context.Context) chan string {
		out := make(chan string, 1)
		go func() {
			res := h.Call(ctx, "acme_form__fill", jsvalue.NewObject("name", "Ann"))
			content, _ := res.Get("content")
			text, _ := content.([]any)[0].(*jsvalue.Object).Get("text")
			out <- text.(string)
		}()
		return out
	}
	first := by(WithCallKey(context.Background(), "key-7", false))
	// The repeat comes after the first call reached the runtime.
	made(t, pages, 1)
	second := by(WithCallKey(context.Background(), "key-7", true))
	got := answerNext(t, pages, `{"ok":true}`)
	if a, b := <-first, <-second; a != `{"ok":true}` || b != a {
		t.Fatalf("answers: %q %q", a, b)
	}
	if calls := pages.Take("tab-1"); len(calls) != 0 {
		t.Fatalf("a second command was made: %+v", calls)
	}
	if third := <-by(WithCallKey(context.Background(), "key-7", true)); third != `{"ok":true}` {
		t.Fatalf("after the end: %q", third)
	}
	if c, _ := pages.Command("tab-1", got.ID); c.Key != "key-7" || c.Status != CommandDone {
		t.Fatalf("the command: %+v", c)
	}
	if lost := <-by(WithCallKey(context.Background(), "key-unknown", true)); !strings.Contains(lost, "its answer is lost") {
		t.Fatalf("a repeat nothing remembers: %q", lost)
	}
	if calls := pages.Take("tab-1"); len(calls) != 0 {
		t.Fatalf("a lost repeat made a command: %+v", calls)
	}
}

// A command whose caller is gone is not handed to a page; the caller has PageCallOrphan to come back with its key.
func TestCommandOfACallerThatLeftDoesNotRun(t *testing.T) {
	r := newCommandRig(t)
	ctx, cut := context.WithCancel(context.Background())
	first := r.call(ctx, "key-9")
	got := r.taken("page-a")
	cut()
	if e := ended(t, first); !errors.Is(e.err, context.Canceled) {
		t.Fatalf("the call: %+v", e)
	}
	// The page asks to run it: not now.
	if _, dead, err := r.pages.Ack("tab-1", got.ID, "page-a"); err != nil || !dead {
		t.Fatalf("ack of a command nobody waits for: dead=%v %v", dead, err)
	}
	r.advance(PageCallResend + time.Second)
	r.none("page-a")
	if s := r.status(got.ID); s != CommandSent {
		t.Fatalf("within the grace: %s", s)
	}
	r.advance(PageCallOrphan)
	r.none("page-a")
	if s := r.status(got.ID); s != CommandCanceled {
		t.Fatalf("after the grace: %s", s)
	}
	if _, dead, _ := r.pages.Ack("tab-1", got.ID, "page-a"); !dead {
		t.Fatal("a canceled command was acknowledged as live")
	}
	if err := r.pages.Answer("tab-1", got.ID, PageAnswer{Result: json.RawMessage(`{}`)}); !errors.Is(err, ErrPageCallDead) {
		t.Fatalf("an answer to a canceled command: %v", err)
	}
}

func TestJournalIsTheOwnersAlone(t *testing.T) {
	r := newCommandRig(t)
	ctx, cut := context.WithCancel(context.Background())
	defer cut()
	r.call(ctx, "key-m")
	r.taken("page-a")
	for _, name := range []string{r.path, r.path + "-wal", r.path + "-shm"} {
		st, err := os.Stat(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", name, st.Mode().Perm(), err)
		}
	}
}
