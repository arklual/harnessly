package paneauto

import "testing"

func TestBrowserToolNames(t *testing.T) {
	for _, name := range []string{"mcp__harnessly__browser_open", "harnessly__browser_click",
		"functions.mcp__harnessly__browser_resize"} {
		if !IsBrowserTool(name) {
			t.Error(name)
		}
	}
	for _, name := range []string{"browser_open", "mcp__other__browser_open", "mcp__harnessly__open",
		"mcp__harnessly__browser_"} {
		if IsBrowserTool(name) {
			t.Error(name)
		}
	}
	if !IsBrowserTool("mcp__runtime_tools__browser_click", "runtime-tools") {
		t.Fatal("normalized server")
	}
}

func TestBrowserOpensBeforeToolFinishesOnce(t *testing.T) {
	r := newRig(t)
	r.w.env.Browser = true
	r.w.Entry("c1", r.tool("mcp__harnessly__browser_open", "open", "", nil))
	r.w.Entry("c1", r.tool("harnessly__browser_click", "click", "", &yes))
	got := r.commands()
	if len(got) != 1 || got[0] != `{"kind":"open","tab":"agent-browser","auto":true}` {
		t.Fatal(got)
	}
}
