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
	h.Hold, h.Timeout, h.AskTimeout = 80*time.Millisecond, 150*time.Millisecond, 400*time.Millisecond
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
	h.Answer("c1", a.ID, "asking", "")
	time.Sleep(250 * time.Millisecond)
	if !h.Answer("c1", a.ID, "done", "ok") {
		t.Fatal("the action timed out while the person was asked")
	}
	if o := <-second; o.Kind != "answered" {
		t.Fatalf("second: %+v", o)
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
