package apps

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// A view's tool, reported by the page, is called by the agent; the page takes the call once and its answer reaches the
// agent. A tool nobody offers, or one whose report ran out, is refused at once.
func TestPageToolCallReachesTheViewAndBack(t *testing.T) {
	p := NewPageTools()
	now := time.Unix(1000, 0)
	p.Now = func() time.Time { return now }
	if err := p.Report("tab-c1", []PageReport{{App: "acme.form", Tools: []PageTool{{Name: "fill", Description: "fill the form", InputSchema: json.RawMessage(`{"type":"object"}`)}}, Context: json.RawMessage(`{"step":2}`)}}); err != nil {
		t.Fatal(err)
	}
	if got := p.Offered("tab-c1"); len(got) != 1 || got[0].Tools[0].Name != "fill" || string(got[0].Context) != `{"step":2}` {
		t.Fatalf("offered: %+v", got)
	}
	if len(p.Offered("tab-other")) != 0 {
		t.Fatal("another chat sees nothing")
	}
	done := make(chan PageAnswer, 1)
	go func() {
		a, err := p.Call(context.Background(), "tab-c1", "acme.form", "fill", json.RawMessage(`{"name":"Ann"}`))
		if err != nil {
			t.Error(err)
		}
		done <- a
	}()
	var calls []PageCall
	for i := 0; i < 200 && len(calls) == 0; i++ {
		calls = p.Take("tab-c1")
		time.Sleep(5 * time.Millisecond)
	}
	if len(calls) != 1 || calls[0].Tool != "fill" || string(calls[0].Arguments) != `{"name":"Ann"}` {
		t.Fatalf("calls: %+v", calls)
	}
	if again := p.Take("tab-c1"); len(again) != 0 {
		t.Fatal("a call goes out once")
	}
	if err := p.Answer("tab-other", calls[0].ID, PageAnswer{Result: json.RawMessage(`1`)}); !errors.Is(err, ErrPageCallGone) {
		t.Fatalf("another chat's page cannot answer: %v", err)
	}
	if err := p.Answer("tab-c1", calls[0].ID, PageAnswer{Result: json.RawMessage(`{"ok":true}`)}); err != nil {
		t.Fatal(err)
	}
	if a := <-done; string(a.Result) != `{"ok":true}` {
		t.Fatalf("answer: %+v", a)
	}
	if _, err := p.Call(context.Background(), "tab-c1", "acme.form", "nope", nil); !errors.Is(err, ErrPageToolGone) {
		t.Fatalf("unknown tool: %v", err)
	}
	now = now.Add(PageToolsTTL + time.Second)
	if _, err := p.Call(context.Background(), "tab-c1", "acme.form", "fill", nil); !errors.Is(err, ErrPageToolGone) {
		t.Fatalf("a closed view's tool: %v", err)
	}
}

func TestPageCallTimesOutAndReportsAreChecked(t *testing.T) {
	p := NewPageTools()
	p.CallTTL = 20 * time.Millisecond
	if err := p.Report("tab-c1", []PageReport{{App: "acme.run", Tools: []PageTool{{Name: "go"}}}}); err != nil {
		t.Fatal(err)
	}
	_ = p.Report("tab-c1", []PageReport{{App: "acme.form", Tools: []PageTool{{Name: "fill"}}}})
	if _, err := p.Call(context.Background(), "tab-c1", "acme.form", "fill", nil); !errors.Is(err, ErrPageCallTimeout) {
		t.Fatalf("timeout: %v", err)
	}
	for _, r := range []PageReport{
		{App: "BAD"},
		{App: "acme.form", Tools: []PageTool{{Name: "1bad"}}},
		{App: "acme.form", Tools: []PageTool{{Name: "a"}, {Name: "a"}}},
		{App: "acme.form", Context: json.RawMessage(`{`)},
	} {
		if err := p.Report("tab-c1", []PageReport{r}); !errors.Is(err, ErrPageToolArgs) {
			t.Errorf("%+v: %v", r, err)
		}
	}
}

// What an app may offer follows its rights.
func TestPageReportFollowsRights(t *testing.T) {
	r := PageReport{App: "acme.form", Tools: []PageTool{{Name: "fill"}}, Context: json.RawMessage(`{}`)}
	none := PageReportAllowed(&Manifest{}, r)
	if none.Tools != nil || none.Context != nil {
		t.Fatalf("no rights: %+v", none)
	}
	both := PageReportAllowed(&Manifest{Permissions: []string{"tools.page", "context.publish"}}, r)
	if len(both.Tools) != 1 || both.Context == nil {
		t.Fatalf("rights: %+v", both)
	}
}

// while the agent's call of a view's tool waits, and for AgentAfter after the view answers it, the app is the
// agent's doing in that chat; another app or another chat is not.
func TestPageToolsSayWhileTheAgentDrivesAnApp(t *testing.T) {
	p := NewPageTools()
	var mu sync.Mutex
	now := time.Unix(1000, 0)
	p.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	if err := p.Report("tab-c1", []PageReport{{App: "acme.run", Tools: []PageTool{{Name: "go"}}}}); err != nil {
		t.Fatal(err)
	}
	if p.Driving("tab-c1", "acme.run") {
		t.Fatal("no call yet")
	}
	done := make(chan struct{})
	go func() {
		_, _ = p.Call(context.Background(), "tab-c1", "acme.run", "go", nil)
		close(done)
	}()
	var calls []PageCall
	for i := 0; i < 200 && len(calls) == 0; i++ {
		calls = p.Take("tab-c1")
		time.Sleep(5 * time.Millisecond)
	}
	if len(calls) != 1 || !p.Driving("tab-c1", "acme.run") || p.Driving("tab-c1", "acme.other") || p.Driving("tab-c2", "acme.run") {
		t.Fatal("a waiting call drives its app in its chat only")
	}
	if err := p.Answer("tab-c1", calls[0].ID, PageAnswer{Result: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	<-done
	if !p.Driving("tab-c1", "acme.run") {
		t.Fatal("just answered: still the agent's")
	}
	mu.Lock()
	now = now.Add(AgentAfter)
	mu.Unlock()
	if p.Driving("tab-c1", "acme.run") {
		t.Fatal("long after the answer")
	}
	// A call that times out unanswered starts the window as well.
	p.CallTTL = 20 * time.Millisecond
	if err := p.Report("tab-c1", []PageReport{{App: "acme.run", Tools: []PageTool{{Name: "go"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Call(context.Background(), "tab-c1", "acme.run", "go", nil); err == nil {
		t.Fatal("no answer, no result")
	}
	if !p.Driving("tab-c1", "acme.run") {
		t.Fatal("a call that timed out is still the agent's for a while")
	}
}

// Sinopia is the host's own: its page tools and its screen reach the agent of the chat the document is open
// beside, with no install of it in the chat; an app that is off is still left out.
func TestBuiltinPageToolsNeedNoInstall(t *testing.T) {
	pages := NewPageTools()
	h := HostTools{Conv: "tab-1", Pages: pages, Registry: &Registry{}, Enabled: func() []string { return nil }}
	if err := pages.Report("tab-1", []PageReport{
		{App: "harnessly.sinopia", Tools: []PageTool{{Name: "show_board", Description: "Show a board"}}, Context: json.RawMessage(`{"board":{"number":1}}`)},
		{App: "acme.off", Tools: []PageTool{{Name: "x"}}},
	}); err != nil {
		t.Fatal(err)
	}
	text, bad := call(t, h, "page_tools")
	if bad || !strings.Contains(text, `"app":"harnessly.sinopia"`) || !strings.Contains(text, `"number":1`) || strings.Contains(text, "acme.off") {
		t.Fatalf("page_tools: %q", text)
	}
	done := make(chan string, 1)
	go func() {
		text, _ := call(t, h, "page_call", "app", "harnessly.sinopia", "tool", "show_board", "arguments", jsvalue.NewObject("board", 2))
		done <- text
	}()
	var calls []PageCall
	for range 200 {
		if calls = pages.Take("tab-1"); len(calls) == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(calls) != 1 || calls[0].App != "harnessly.sinopia" || string(calls[0].Arguments) != `{"board":2}` {
		t.Fatalf("calls: %+v", calls)
	}
	if err := pages.Answer("tab-1", calls[0].ID, PageAnswer{Result: json.RawMessage(`{"shown":"b.html"}`)}); err != nil {
		t.Fatal(err)
	}
	if got := <-done; !strings.Contains(got, "b.html") {
		t.Fatalf("answer: %q", got)
	}
	if text, bad := call(t, h, "page_call", "app", "acme.off", "tool", "x"); !bad || !strings.Contains(text, "no app is on") {
		t.Fatalf("an app that is off: %q", text)
	}
}
