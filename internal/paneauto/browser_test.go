package paneauto

import "testing"

func TestBrowserToolNames(t *testing.T) {
	for _, name := range []string{"mcp__harnessly__chrome_open", "harnessly__chrome_click",
		"functions.mcp__harnessly__chrome_resize"} {
		if !IsBrowserTool(name) {
			t.Error(name)
		}
	}
	for _, name := range []string{"chrome_open", "mcp__other__chrome_open", "mcp__harnessly__open",
		"mcp__harnessly__chrome_"} {
		if IsBrowserTool(name) {
			t.Error(name)
		}
	}
	if !IsBrowserTool("mcp__runtime_tools__chrome_click", "runtime-tools") {
		t.Fatal("normalized server")
	}
}

func TestBrowserOpensBeforeToolFinishesOnce(t *testing.T) {
	r := newRig(t)
	r.w.env.Browser = true
	r.w.Entry("c1", r.tool("mcp__harnessly__chrome_open", "open", "", nil))
	r.w.Entry("c1", r.tool("harnessly__chrome_click", "click", "", &yes))
	got := r.commands()
	if len(got) != 1 || got[0] != `{"kind":"open","tab":"agent-browser","auto":true}` {
		t.Fatal(got)
	}
}
