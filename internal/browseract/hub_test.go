package browseract

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

func fastHub() *Hub {
	h := NewHub()
	h.Hold, h.Timeout, h.AskTimeout, h.AskStale = 80*time.Millisecond, 150*time.Millisecond, 400*time.Millisecond, 300*time.Millisecond
	return h
}

func dispatch(h *Hub, a Action) <-chan Outcome {
	out := make(chan Outcome, 1)
	go func() { out <- h.Dispatch(context.Background(), "c1", a) }()
	return out
}

func TestNoPageNoUI(t *testing.T) {
	h := fastHub()
	if o := h.Dispatch(context.Background(), "c1", Action{Op: "snapshot"}); o.Kind != "no-ui" {
		t.Fatalf("got %+v", o)
	}
}

func TestPollGetsActionAndAnswerReturns(t *testing.T) {
	h := fastHub()
	got := make(chan Action, 1)
	go func() { a, _ := h.Poll(context.Background(), "c1"); got <- a }()
	time.Sleep(20 * time.Millisecond)
	out := dispatch(h, Action{Op: "click", Ref: "e1"})
	a := <-got
	if a.Op != "click" || a.ID == "" {
		t.Fatalf("action %+v", a)
	}
	if !h.Answer("c1", a.ID, "done", "clicked") {
		t.Fatal("answer refused")
	}
	if o := <-out; o.Kind != "answered" || o.Text != "clicked" || o.Failed {
		t.Fatalf("outcome %+v", o)
	}
	if h.Answer("c1", a.ID, "done", "again") {
		t.Fatal("a settled action answered twice")
	}
}

func TestActionWaitsBetweenPolls(t *testing.T) {
	h := fastHub()
	h.Poll(context.Background(), "c1") // returns after Hold with nothing; the page is present for Present after
	out := dispatch(h, Action{Op: "back"})
	time.Sleep(20 * time.Millisecond)
	a, res := h.Poll(context.Background(), "c1")
	if res != PollAction || a.Op != "back" {
		t.Fatalf("poll %+v %d", a, res)
	}
	h.Answer("c1", a.ID, "failed", "no history")
	if o := <-out; !o.Failed || o.Text != "no history" {
		t.Fatalf("outcome %+v", o)
	}
}

func TestBusyAndTimeoutAndAsking(t *testing.T) {
	h := fastHub()
	h.Poll(context.Background(), "c1")
	first := dispatch(h, Action{Op: "snapshot"})
	time.Sleep(20 * time.Millisecond)
	if o := h.Dispatch(context.Background(), "c1", Action{Op: "snapshot"}); o.Kind != "busy" {
		t.Fatalf("second: %+v", o)
	}
	if o := <-first; o.Kind != "timeout" {
		t.Fatalf("first: %+v", o)
	}

	// asking extends the wait past Timeout.
	second := dispatch(h, Action{Op: "snapshot"})
	a, _ := h.Poll(context.Background(), "c1")
	h.Answer("c1", a.ID, "asking", "example.com")
	time.Sleep(250 * time.Millisecond)
	if o := h.Dispatch(context.Background(), "c1", Action{Op: "snapshot"}); o.Kind != "asking" || o.Text != "example.com" {
		t.Fatalf("third, while the person is asked: %+v", o)
	}
	if !h.Answer("c1", a.ID, "done", "ok") {
		t.Fatal("the action timed out while the person was asked")
	}
	if o := <-second; o.Kind != "answered" {
		t.Fatalf("second: %+v", o)
	}
}

// The person takes longer than one call: the call ends with «asking», the next one waits for the answer, then sends
// its own action (HRN-681).
func TestAskingOutlivesTheCallAndTheNextCallWaits(t *testing.T) {
	h := fastHub()
	h.Poll(context.Background(), "c1")
	first := dispatch(h, Action{Op: "snapshot"})
	a, _ := h.Poll(context.Background(), "c1")
	h.Answer("c1", a.ID, "asking", "html.duckduckgo.com")
	if o := <-first; o.Kind != "asking" || o.Text != "html.duckduckgo.com" {
		t.Fatalf("first: %+v", o)
	}
	// The page repeats «asking» while the question is open; the chat is not free for another action.
	h.Answer("c1", a.ID, "asking", "html.duckduckgo.com")
	second := dispatch(h, Action{Op: "click", Ref: "e1"})
	time.Sleep(50 * time.Millisecond)
	if o := h.Dispatch(context.Background(), "c1", Action{Op: "back"}); o.Kind != "asking" {
		t.Fatalf("a third call while one waits: %+v", o)
	}
	// «Разрешить»: the page runs the first action and answers it; the second call's action goes out next.
	if !h.Answer("c1", a.ID, "done", "url: https://html.duckduckgo.com/") {
		t.Fatal("the first action was forgotten")
	}
	b, res := h.Poll(context.Background(), "c1")
	if res != PollAction || b.Op != "click" {
		t.Fatalf("poll after the answer: %+v %d", b, res)
	}
	h.Answer("c1", b.ID, "done", "clicked")
	if o := <-second; o.Kind != "answered" || o.Text != "clicked" {
		t.Fatalf("second: %+v", o)
	}
}

func TestWaitingCallEndsWithAskingToo(t *testing.T) {
	h := fastHub()
	h.Poll(context.Background(), "c1")
	first := dispatch(h, Action{Op: "snapshot"})
	a, _ := h.Poll(context.Background(), "c1")
	h.Answer("c1", a.ID, "asking", "example.com")
	<-first
	h.Answer("c1", a.ID, "asking", "example.com")
	if o := h.Dispatch(context.Background(), "c1", Action{Op: "snapshot"}); o.Kind != "asking" || o.Text != "example.com" {
		t.Fatalf("second: %+v", o)
	}
	// «Запретить» comes as the page's failed answer; the next call is free to go and hears the refusal from the page.
	h.Answer("c1", a.ID, "failed", "The person declined")
	out := dispatch(h, Action{Op: "snapshot"})
	time.Sleep(20 * time.Millisecond)
	b, res := h.Poll(context.Background(), "c1")
	if res != PollAction {
		t.Fatalf("poll: %d", res)
	}
	h.Answer("c1", b.ID, "failed", "The person does not let agents act on example.com")
	if o := <-out; !o.Failed || o.Kind != "answered" {
		t.Fatalf("outcome %+v", o)
	}
}

// A tab closed without a word stops repeating «asking»: its question no longer holds the chat.
func TestStaleAskingIsDropped(t *testing.T) {
	h := fastHub()
	now := time.Unix(1000, 0)
	h.Now = func() time.Time { return now }
	h.Poll(context.Background(), "c1")
	first := dispatch(h, Action{Op: "snapshot"})
	a, _ := h.Poll(context.Background(), "c1")
	h.Answer("c1", a.ID, "asking", "example.com")
	<-first
	now = now.Add(100 * time.Millisecond)
	if o := h.Dispatch(context.Background(), "c1", Action{Op: "snapshot"}); o.Kind != "asking" {
		t.Fatalf("fresh question: %+v", o)
	}
	now = now.Add(time.Minute)
	if o := h.Dispatch(context.Background(), "c1", Action{Op: "snapshot"}); o.Kind != "no-ui" {
		t.Fatalf("stale question: %+v", o)
	}
	if h.chats["c1"].act != nil {
		t.Fatal("the stale question still holds the chat")
	}
}

func TestPollLimitAndCancel(t *testing.T) {
	h := fastHub()
	h.Hold = 5 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{}, maxPolls)
	for i := 0; i < maxPolls; i++ {
		go func() { h.Poll(ctx, "c1"); done <- struct{}{} }()
	}
	time.Sleep(30 * time.Millisecond)
	if _, res := h.Poll(context.Background(), "c1"); res != PollTooMany {
		t.Fatalf("fifth poll: %d", res)
	}
	cancel()
	for i := 0; i < maxPolls; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("a poll outlived its request")
		}
	}
	if _, res := h.Poll(context.Background(), "c1"); res == PollTooMany {
		t.Fatal("cancelled polls kept their slots")
	}
}

func TestPresenceExpires(t *testing.T) {
	h := fastHub()
	now := time.Unix(1000, 0)
	h.Now = func() time.Time { return now }
	h.Poll(context.Background(), "c1")
	now = now.Add(34 * time.Second)
	if c := h.chats["c1"]; !c.present(h.Now(), h.Present) {
		t.Fatal("gone before 35 s")
	}
	now = now.Add(2 * time.Second)
	if o := h.Dispatch(context.Background(), "c1", Action{Op: "snapshot"}); o.Kind != "no-ui" {
		t.Fatalf("got %+v", o)
	}
}

func TestRateLimit(t *testing.T) {
	h := fastHub()
	h.Timeout = time.Millisecond
	now := time.Unix(1000, 0)
	h.Now = func() time.Time { return now }
	h.Poll(context.Background(), "c1")
	for i := 0; i < rateLimit; i++ {
		if o := h.Dispatch(context.Background(), "c1", Action{Op: "back"}); o.Kind != "timeout" {
			t.Fatalf("%d: %+v", i, o)
		}
	}
	if o := h.Dispatch(context.Background(), "c1", Action{Op: "back"}); o.Kind != "rate-limited" {
		t.Fatalf("got %+v", o)
	}
}

func TestHTTP(t *testing.T) {
	h := fastHub()
	do := func(method, target, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, target, strings.NewReader(body)))
		return w
	}
	if w := do("GET", "/api/v2/pane-act?conversation=c1", ""); w.Code != http.StatusNoContent {
		t.Fatalf("empty poll: %d", w.Code)
	}
	out := dispatch(h, Action{Op: "navigate", URL: "https://example.com/"})
	time.Sleep(10 * time.Millisecond)
	w := do("GET", "/api/v2/pane-act?conversation=c1", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"op":"navigate"`) || !strings.Contains(w.Body.String(), `"dy":0`) {
		t.Fatalf("poll: %d %s", w.Code, w.Body)
	}
	id := h.chats["c1"].act.action.ID
	if w := do("POST", "/api/v2/pane-act", `{"conversation":"c1","id":"nope","state":"done"}`); w.Code != 404 {
		t.Fatalf("unknown id: %d", w.Code)
	}
	if w := do("POST", "/api/v2/pane-act", `{"conversation":"c1","id":"`+id+`","state":"maybe"}`); w.Code != 400 {
		t.Fatalf("bad state: %d", w.Code)
	}
	if w := do("POST", "/api/v2/pane-act", `{"conversation":"c1","id":"`+id+`","state":"done","text":"ok"}`); w.Code != 204 {
		t.Fatalf("answer: %d", w.Code)
	}
	if o := <-out; o.Text != "ok" {
		t.Fatalf("outcome %+v", o)
	}
	if w := do("PUT", "/api/v2/pane-act", ""); w.Code != 405 {
		t.Fatalf("method: %d", w.Code)
	}
}

func TestToolValidation(t *testing.T) {
	tl := Tools{Hub: fastHub(), Chat: "c1"}
	args := func(kv ...any) *jsvalue.Object { return jsvalue.NewObject(kv...) }
	for name, c := range map[string]struct {
		tool string
		args *jsvalue.Object
	}{
		"bad ref":      {ToolClick, args("ref", "12", "element", "x")},
		"no element":   {ToolClick, args("ref", "e1")},
		"user info":    {ToolNavigate, args("url", "https://a:b@example.com/")},
		"scheme":       {ToolNavigate, args("url", "javascript:alert(1)")},
		"dy range":     {ToolScroll, args("dy", float64(30000))},
		"scroll empty": {ToolScroll, args()},
		"wait empty":   {ToolWait, args()},
		"wait event":   {ToolWait, args("event", "stopped")},
		"wait long":    {ToolWait, args("text", "x", "ms", float64(20000))},
		"event long":   {ToolWait, args("event", "ended", "ms", float64(30000))},
		"read ref":     {ToolRead, args("ref", "body")},
		"no key":       {ToolPress, args()},
	} {
		res := tl.Call(context.Background(), c.tool, c.args)
		if v, _ := res.Get("isError"); v != true {
			t.Errorf("%s: not refused", name)
		}
	}
	if res := (Tools{Hub: fastHub(), Why: "no chat"}).Call(context.Background(), ToolBack, nil); !strings.Contains(textOf(res), "refused: no chat") {
		t.Error("empty chat")
	}
	if res := tl.Call(context.Background(), ToolSnapshot, nil); !strings.HasPrefix(textOf(res), "No browser tab") {
		t.Errorf("no page: %s", textOf(res))
	}
}

func TestSnapshotResultIsMarkedAsData(t *testing.T) {
	h := fastHub()
	tl := Tools{Hub: h, Chat: "c1"}
	h.Poll(context.Background(), "c1")
	res := make(chan *jsvalue.Object, 1)
	go func() { res <- tl.Call(context.Background(), ToolSnapshot, nil) }()
	time.Sleep(20 * time.Millisecond)
	a, _ := h.Poll(context.Background(), "c1")
	h.Answer("c1", a.ID, "done", "url: https://example.com/")
	if got := textOf(<-res); !strings.HasPrefix(got, dataNotice) || !strings.Contains(got, "url: https://example.com/") {
		t.Errorf("got %q", got)
	}
}

func textOf(o *jsvalue.Object) string {
	c := o.Array("content")
	if len(c) == 0 {
		return ""
	}
	return c[0].(*jsvalue.Object).String("text")
}

func TestCallSiteToolSendsNameAndArguments(t *testing.T) {
	h := fastHub()
	tl := Tools{Hub: h, Chat: "c1"}
	h.Poll(context.Background(), "c1")
	res := make(chan *jsvalue.Object, 1)
	go func() {
		res <- tl.Call(context.Background(), ToolCallSiteTool,
			jsvalue.NewObject("name", "add_to_cart", "arguments", jsvalue.NewObject("id", "e-12", "count", float64(2))))
	}()
	time.Sleep(20 * time.Millisecond)
	a, _ := h.Poll(context.Background(), "c1")
	if a.Op != "call" || a.Tool != "add_to_cart" || a.Element != "add_to_cart" || a.Input != `{"id":"e-12","count":2}` {
		t.Fatalf("action %+v", a)
	}
	h.Answer("c1", a.ID, "done", "add_to_cart returned:\nok")
	if got := textOf(<-res); !strings.HasPrefix(got, dataNotice) {
		t.Errorf("got %q", got)
	}
}

func TestCallSiteToolRefusals(t *testing.T) {
	tl := Tools{Hub: fastHub(), Chat: "c1"}
	long := strings.Repeat("x", maxInput)
	for name, args := range map[string]*jsvalue.Object{
		"no name":    jsvalue.NewObject(),
		"bad name":   jsvalue.NewObject("name", "add to cart"),
		"not object": jsvalue.NewObject("name", "a", "arguments", "x"),
		"long":       jsvalue.NewObject("name", "a", "arguments", jsvalue.NewObject("q", long)),
	} {
		res := tl.Call(context.Background(), ToolCallSiteTool, args)
		if v, _ := res.Get("isError"); v != true || !strings.HasPrefix(textOf(res), "refused:") {
			t.Errorf("%s: %s", name, textOf(res))
		}
	}
}

// A wait for a video's event has up to 25 s; read takes an optional ref.
func TestWaitEventAndReadActions(t *testing.T) {
	a, why := action("wait", jsvalue.NewObject("event", "ended", "ref", "e3", "ms", float64(20000)))
	if why != "" || a.Event != "ended" || a.Ref != "e3" || a.MS != 20000 {
		t.Fatalf("%+v %q", a, why)
	}
	if a, why := action("wait", jsvalue.NewObject("event", "playing")); why != "" || a.Event != "playing" || a.MS != 0 {
		t.Errorf("%+v %q", a, why)
	}
	for _, ref := range []string{"", "e2"} {
		if a, why := action("read", jsvalue.NewObject("ref", ref)); why != "" || a.Op != "read" || a.Ref != ref {
			t.Errorf("read %q: %+v %q", ref, a, why)
		}
	}
}
