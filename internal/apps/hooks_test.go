package apps

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMergeHooksAnyStopWinsContextsAddUp(t *testing.T) {
	allow := &HookWord{Context: "tests are slow here"}
	deny := &HookWord{Decision: "deny", Reason: "no pushes on Friday"}
	v := MergeHooks([]HookAsk{
		{App: "a.one", Name: "One", Live: true, Word: allow},
		{App: "a.two", Name: "Two", Live: true, Word: deny},
		{App: "a.three", Name: "Three", Live: true},
	})
	if !v.Deny || v.Name != "Two" || v.Reason != "no pushes on Friday" || v.Context != "" {
		t.Fatalf("a stop wins and carries no context: %+v", v)
	}
	if len(v.Silent) != 1 || v.Silent[0] != "a.three" {
		t.Fatalf("the silent app is journaled: %+v", v.Silent)
	}
	v = MergeHooks([]HookAsk{
		{App: "a.one", Name: "One", Live: true, Word: allow},
		{App: "a.two", Name: "Two", Live: true, Word: &HookWord{Context: "branch is main"}},
		{App: "a.three", Name: "Three"},
	})
	if v.Deny || v.Context != "[One] tests are slow here\n[Two] branch is main" || len(v.Silent) != 0 {
		t.Fatalf("contexts add up; an app with no view is not silent: %+v", v)
	}
	v = MergeHooks([]HookAsk{{App: "a.s", Name: "Strict", Strict: true}})
	if !v.Deny || v.App != "a.s" || !strings.Contains(v.Reason, "strict") {
		t.Fatalf("a strict app that did not answer stops the call: %+v", v)
	}
	v = MergeHooks([]HookAsk{{App: "a.one", Name: "One", Live: true, Word: &HookWord{Decision: "deny", Reason: strings.Repeat("я", 2000)}}})
	if len(v.Reason) > HookReasonMax+len("…") {
		t.Fatalf("a long reason is cut: %d", len(v.Reason))
	}
}

func TestPageToolsHookGoesToAViewThatHears(t *testing.T) {
	p := NewPageTools()
	if _, err := p.Hook(context.Background(), "c1", "a.one", json.RawMessage(`{"tool":"Bash"}`), time.Second); err == nil {
		t.Fatal("a hook went to no view")
	}
	if err := p.Report("c1", []PageReport{{App: "a.one", Hooks: true}}); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			for _, c := range p.Take("c1") {
				if c.Hook && c.Tool == "" {
					_ = p.Answer("c1", c.ID, PageAnswer{Result: json.RawMessage(`{"decision":"deny"}`)})
				}
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	got, err := p.Hook(context.Background(), "c1", "a.one", json.RawMessage(`{"tool":"Bash"}`), time.Second)
	if err != nil || string(got.Result) != `{"decision":"deny"}` {
		t.Fatalf("the hook: %+v %v", got, err)
	}
	if !p.Driving("c1", "a.one") {
		t.Fatal("a view deciding the agent's call acts in the agent's turn")
	}
	if PageReportAllowed(&Manifest{Permissions: []string{"tools.page"}}, PageReport{App: "a.one", Hooks: true}).Hooks {
		t.Fatal("hooks without hooks.agent")
	}
}

func TestHooksOffIsKept(t *testing.T) {
	dir := t.TempDir()
	r := Open(dir)
	if r.HooksOff("a.one") {
		t.Fatal("off by default")
	}
	if err := r.SetHooksOff("a.one", true); err != nil {
		t.Fatal(err)
	}
	if !Open(dir).HooksOff("a.one") {
		t.Fatal("the switch is kept on the machine")
	}
	if err := r.SetHooksOff("a.one", false); err != nil || r.HooksOff("a.one") {
		t.Fatal("back on")
	}
}
