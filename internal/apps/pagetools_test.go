package apps

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
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
