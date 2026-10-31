package paneauto

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/paneintents"
	"github.com/ly-solutions/harnessly/internal/panel"
	"github.com/ly-solutions/harnessly/internal/wire"
)

var yes = true
var no = false

type rig struct {
	now     time.Time
	store   *paneintents.Store
	w       *Watcher
	changed []string
}

func newRig(t *testing.T) *rig {
	r := &rig{now: time.UnixMilli(1_760_000_000_000)}
	clock := func() time.Time { return r.now }
	r.store = paneintents.New(filepath.Join(t.TempDir(), "i.json"), clock, false)
	r.w = New(Env{
		Targets: panel.Targets{PortalHost: "a.example.org", ReaderPath: "/agents/docs/"},
		DesignOf: func(p string) string {
			if rest, ok := strings.CutPrefix(p, "/lib/"); ok {
				return strings.Split(rest, "/")[0]
			}
			return ""
		},
		Intents: r.store,
		TabOf: func(conv string) string {
			if conv == "c1" {
				return "tab-1"
			}
			return ""
		},
		Changed: func(tab string) { r.changed = append(r.changed, tab) },
		Now:     clock,
	})
	return r
}

var seq int

func (r *rig) tool(name, summary, output string, ok *bool) wire.Entry {
	seq++
	e := wire.Entry{ID: "c:e" + string(rune('a'+seq%26)) + strings.Repeat("x", seq/26) + "#0", At: wire.Ms(r.now.UnixMilli()), T: wire.EntryTool, Tool: name, Summary: summary, OK: ok}
	if output != "" {
		e.Output = &wire.ToolOutput{Text: output}
	}
	return e
}

func (r *rig) commands() []string {
	var out []string
	for _, it := range r.store.List("tab-1") {
		out = append(out, string(jsvalue.Stringify(it.Object("command"), "")))
	}
	return out
}

func TestDesignOpensOnceForAllEdits(t *testing.T) {
	r := newRig(t)
	if !r.w.Entry("c1", r.tool("Write", "Write /lib/landing/project/Main.dc.html", "", &yes)) {
		t.Fatal("not kept")
	}
	r.w.Entry("c1", r.tool("Edit", "Update /lib/landing/project/Main.dc.html (+3, -1)", "", &yes))
	r.w.Entry("c1", r.tool("Edit", "Update /lib/landing/project/Other.dc.html (+1, -1)", "", &yes))
	got := r.commands()
	if len(got) != 1 || got[0] != `{"kind":"open","tab":"design","target":"landing","auto":true}` {
		t.Fatal(got)
	}
	if len(r.changed) != 1 || r.changed[0] != "tab-1" {
		t.Fatal(r.changed)
	}
}

func TestOnlySuccessfulFreshCallsOfKnownConversations(t *testing.T) {
	r := newRig(t)
	r.w.Entry("c1", r.tool("Write", "Write /lib/a/project/M.dc.html", "", nil))
	r.w.Entry("c1", r.tool("Write", "Write /lib/b/project/M.dc.html", "", &no))
	r.w.Entry("c2", r.tool("Write", "Write /lib/c/project/M.dc.html", "", &yes))
	r.w.Entry("c1", r.tool("Write", "Write /work/repo/main.go", "", &yes))
	r.w.Entry("c1", r.tool("Read", "Read /lib/d/project/M.dc.html", "", &yes))
	r.w.Entry("c1", r.tool("Bash", "$ cat > /lib/e/project/M.dc.html", "", &yes))
	old := r.tool("Write", "Write /lib/f/project/M.dc.html", "", &yes)
	old.At = wire.Ms(r.now.Add(-Fresh - time.Second).UnixMilli())
	r.w.Entry("c1", old)
	if got := r.commands(); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestDocumentOpensThenReloads(t *testing.T) {
	r := newRig(t)
	publish := func() wire.Entry {
		return r.tool("Bash", "$ acme-artifact publish /tmp/plan.html --title Plan", "Опубликовано: https://a.example.org/a/plan-x.\n", &yes)
	}
	r.w.Entry("c1", publish())
	r.now = r.now.Add(time.Minute)
	r.w.Entry("c1", publish())
	// A reload not yet applied takes the next ones.
	r.now = r.now.Add(time.Minute)
	r.w.Entry("c1", publish())
	got := r.commands()
	want := []string{
		`{"kind":"open","tab":"artifacts","target":"/agents/docs/plan-x","auto":true}`,
		`{"kind":"open","tab":"artifacts","target":"/agents/docs/plan-x","auto":true,"reload":true}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatal(got)
	}
	list := r.store.List("tab-1")
	r.store.MarkApplied("tab-1", []string{list[1].String("id")}, "desktop")
	r.now = r.now.Add(time.Second)
	r.w.Entry("c1", publish())
	r.now = r.now.Add(time.Second)
	r.w.Entry("c1", publish()) // within ReloadEvery
	if got := r.commands(); len(got) != 3 {
		t.Fatal(got)
	}
}

func TestTheAgentsOwnOpenCounts(t *testing.T) {
	r := newRig(t)
	r.store.Add("tab-1", panel.Command{Kind: "open", Tab: "artifacts", Target: "/agents/docs/plan-x"}.JSON())
	r.w.Entry("c1", r.tool("Bash", "$ acme-artifact publish x.html", "https://a.example.org/a/plan-x", &yes))
	got := r.commands()
	if len(got) != 2 || !strings.Contains(got[1], `"reload":true`) {
		t.Fatal(got)
	}
}

func TestBoards(t *testing.T) {
	r := newRig(t)
	r.w.Entry("c1", r.tool("Bash", `$ acme-board create --title "Путь" --graph /tmp/g.json`, "https://a.example.org/b/path-1?k=Key_1\n", &yes))
	r.w.Entry("c1", r.tool("Bash", `$ acme-board edit 'https://a.example.org/b/path-1?k=Key_1' --graph -`, "ok", &yes))
	r.w.Entry("c1", r.tool("Bash", `$ acme-board read 'https://a.example.org/b/other?k=Key_2'`, "…", &yes))
	r.w.Entry("c1", r.tool("Bash", `$ curl https://a.example.org/a/plan-x`, "…", &yes))
	got := r.commands()
	if len(got) != 1 || got[0] != `{"kind":"open","tab":"artifacts","target":"https://a.example.org/b/path-1?k=Key_1","auto":true}` {
		t.Fatal(got)
	}
}

func TestAnEntryIsLookedAtOnce(t *testing.T) {
	r := newRig(t)
	e := r.tool("Bash", "$ acme-artifact publish x.html", "https://a.example.org/a/plan-x", &yes)
	r.w.Entry("c1", e)
	r.now = r.now.Add(time.Minute)
	e.Rev = 2
	r.w.Entry("c1", e)
	if got := r.commands(); len(got) != 1 {
		t.Fatal(got)
	}
}

func TestAHandedOverFileOpensOnce(t *testing.T) {
	r := newRig(t)
	if !OpenOnce(r.store, "tab-1", "files", "/w/report.zip") {
		t.Fatal("the first delivery asks the pane")
	}
	if OpenOnce(r.store, "tab-1", "files", "/w/report.zip") {
		t.Fatal("the same file does not ask twice")
	}
	if !OpenOnce(r.store, "tab-1", "files", "/w/other.md") || !OpenOnce(r.store, "tab-2", "files", "/w/report.zip") {
		t.Fatal("another file or another chat asks")
	}
	list := r.store.List("tab-1")
	if len(list) != 2 {
		t.Fatalf("%d intents", len(list))
	}
}

func TestAChromeMcpOpensTheChromeTabOnce(t *testing.T) {
	r := newRig(t)
	r.w.Entry("c1", r.tool("mcp__chrome__navigate_page", "chrome · navigate_page", "", &yes))
	if got := r.commands(); len(got) != 0 {
		t.Fatal("opened on a machine without a Chrome to show:", got)
	}
	r.w.env.Chrome = true
	r.w.Entry("c1", r.tool("mcp__chrome__take_snapshot", "chrome · take_snapshot", "", &no))
	r.w.Entry("c1", r.tool("mcp__github__search", "github · search", "", &yes))
	r.w.Entry("c1", r.tool("mcp__chrome-devtools__click", "click", "", &yes))
	r.w.Entry("c1", r.tool("mcp__chrome__fill", "fill", "", &yes))
	got := r.commands()
	if len(got) != 1 || got[0] != `{"kind":"open","tab":"chrome","auto":true}` {
		t.Fatal(got)
	}
	for tool, want := range map[string]bool{"mcp__chrome__click": true, "mcp__Chrome-DevTools__x": true, "mcp__chrome": false,
		"chrome__click": false, "mcp__github__chrome": false} {
		if IsChromeTool(tool) != want {
			t.Error(tool)
		}
	}
}
