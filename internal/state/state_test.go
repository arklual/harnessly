package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

type rig struct {
	t    *testing.T
	s    *Store
	tm   *fakeTmux
	rec  *recorder
	data string
	home string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	home := t.TempDir()
	data := filepath.Join(home, "data")
	tm := newFakeTmux(home)
	rec := &recorder{}
	s := New(Config{DataDir: data, Tmux: tm, Broadcast: rec, Reconciler: rec, Home: home, MetaKey: "meta",
		AgentTokenHeader: "x-agent-local-token", NewID: sequentialIDs()})
	return &rig{t: t, s: s, tm: tm, rec: rec, data: data, home: home}
}

func (r *rig) writeLayout(wsID, doc string) string {
	r.t.Helper()
	file := filepath.Join(r.data, "workspaces", wsID, "layout.json")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(doc), 0o600); err != nil {
		r.t.Fatal(err)
	}
	return file
}

func (r *rig) readFile(rel string) *jsvalue.Object {
	r.t.Helper()
	raw, err := os.ReadFile(filepath.Join(r.data, rel))
	if err != nil {
		r.t.Fatal(err)
	}
	v, err := jsvalue.Parse(raw)
	if err != nil {
		r.t.Fatal(err)
	}
	return v.(*jsvalue.Object)
}

func mustParse(t *testing.T, doc string) *jsvalue.Object {
	t.Helper()
	v, err := jsvalue.Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return v.(*jsvalue.Object)
}

const threePanes = `{"root":{"type":"split","orientation":"horizontal","ratio":50,"children":[` +
	`{"type":"pane","id":"pane-a","tabs":[{"id":"tab-a","sessionName":"tk-ws-x-pane-a-tab-a","name":"","order":0}],"activeTabId":"tab-a"},` +
	`{"type":"split","orientation":"vertical","ratio":50,"children":[` +
	`{"type":"pane","id":"pane-b","tabs":[{"id":"tab-b","sessionName":"tk-ws-x-pane-b-tab-b","name":"","order":0}],"activeTabId":"tab-b"},` +
	`{"type":"pane","id":"pane-c","tabs":[{"id":"tab-c","sessionName":"tk-ws-x-pane-c-tab-c","name":"","order":0,"panelType":"web-browser"}],"activeTabId":"tab-c"}]}]},` +
	`"activePaneId":"pane-b","updatedAt":"2026-01-01T00:00:00.000Z"}`

func TestCloseMovesFocusToTheNeighbour(t *testing.T) {
	for _, c := range []struct{ close, active, wantActive string }{
		{"pane-a", "pane-a", "pane-b"}, // a left child: the first pane of its right sibling
		{"pane-b", "pane-b", "pane-c"}, // left child of the inner split
		{"pane-c", "pane-c", "pane-b"}, // a right child: the last pane of its left sibling
		{"pane-c", "pane-a", "pane-a"}, // focus elsewhere stays
	} {
		r := newRig(t)
		r.writeLayout("ws-x", strings.Replace(threePanes, `"activePaneId":"pane-b"`, `"activePaneId":"`+c.active+`"`, 1))
		l, err := r.s.ClosePane("ws-x", c.close)
		if err != nil {
			t.Fatal(err)
		}
		if got := l.String("activePaneId"); got != c.wantActive {
			t.Errorf("close %s with %s active: focus %s, want %s", c.close, c.active, got, c.wantActive)
		}
		if findPane(root(l), c.close) != nil || len(panes(root(l))) != 2 {
			t.Errorf("close %s: %s", c.close, jsvalue.Stringify(l, ""))
		}
		var killed []string
		for _, call := range r.tm.calls {
			killed = append(killed, call[1].(string))
		}
		if c.close == "pane-c" && len(killed) != 0 {
			t.Errorf("a web browser has no session to end: %v", killed)
		}
	}
	r := newRig(t)
	r.writeLayout("ws-x", `{"root":{"type":"pane","id":"pane-a","tabs":[],"activeTabId":null},"activePaneId":"pane-a","updatedAt":"x"}`)
	var e *Error
	if _, err := r.s.ClosePane("ws-x", "pane-a"); !errors.As(err, &e) || e.Status != 404 || e.Message != "Pane not found" {
		t.Fatalf("the last pane closed: %v", err)
	}
}

func TestEqualize(t *testing.T) {
	r := newRig(t)
	// The inner vertical split has two panes and the outer horizontal one counts it as one unit: 50/50 outside.
	r.writeLayout("ws-x", strings.ReplaceAll(threePanes, `"ratio":50`, `"ratio":10`))
	l, err := r.s.PatchLayout("ws-x", LayoutPatch{Equalize: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := root(l).Number("ratio"); got != 50 {
		t.Fatalf("outer %v", got)
	}
	// Splitting a pane of an equalized row keeps it equal: three panes side by side are 33.33 / 66.67 / 50.
	r2 := newRig(t)
	r2.writeLayout("ws-x", `{"root":{"type":"pane","id":"pane-a","tabs":[{"id":"tab-a","sessionName":"s","name":"","order":0,"cwd":"/w"}],"activeTabId":"tab-a"},"activePaneId":"pane-a","updatedAt":"x"}`)
	l, _ = r2.s.SplitPane("ws-x", "pane-a", "horizontal", "", "")
	l, _ = r2.s.SplitPane("ws-x", l.String("activePaneId"), "horizontal", "", "")
	ra, _ := root(l).Number("ratio")
	_, inner, _ := children(root(l))
	rb, _ := inner.Number("ratio")
	if ra != 33.33 || rb != 50 {
		t.Fatalf("ratios %v %v: %s", ra, rb, jsvalue.Stringify(l, ""))
	}
	_, _ = r2.s.PatchLayout("ws-x", LayoutPatch{RatioUpdate: &RatioUpdate{Path: []int{1}, Ratio: float64(80)}})
	l, _ = r2.s.SplitPane("ws-x", "pane-a", "vertical", "", "")
	if isEqualized(root(l)) {
		t.Fatal("a tree that was not equal must not be equalized by a split")
	}
	if mathRound(2.5) != 3 || mathRound(-2.5) != -2 || mathRound(0.49999999999999994) != 0 {
		t.Fatal("Math.round")
	}
	if got := mathRound(2.0/3*10000) / 100; got != 66.67 {
		t.Fatal(got)
	}
}

func TestVisualOrderOnWrite(t *testing.T) {
	r := newRig(t)
	doc := `{"workspaces":[{"id":"ws-1","name":"a","directories":["/a"]},{"id":"ws-2","name":"b","directories":["/b"],"groupId":"grp-2"},` +
		`{"id":"ws-3","name":"c","directories":["/c"],"groupId":"grp-1"},{"id":"ws-4","name":"d","directories":["/d"],"groupId":"grp-2"}],` +
		`"groups":[{"id":"grp-1","name":"one"},{"id":"grp-2","name":"two"}],"sidebarCollapsed":false,"sidebarWidth":240,"updatedAt":"x"}`
	_ = os.MkdirAll(r.data, 0o755)
	_ = os.WriteFile(filepath.Join(r.data, "workspaces.json"), []byte(doc), 0o600)
	if err := r.s.UpdateActive(jsvalue.NewObject("activeWorkspaceId", "ws-4")); err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, ws := range workspacesOf(r.readFile("workspaces.json")) {
		order = append(order, ws.String("id"))
	}
	if !reflect.DeepEqual(order, []string{"ws-3", "ws-2", "ws-4", "ws-1"}) {
		t.Fatal(order)
	}
	if err := r.s.ReorderWorkspaces([]ReorderItem{{ID: "ws-1"}, {ID: "ws-1"}, {ID: "ws-2"}, {ID: "ws-3"}}); err == nil {
		t.Fatal("a reorder naming a workspace twice drops another")
	}
	if err := r.s.ReorderGroups([]string{"grp-2", "grp-2"}); err == nil {
		t.Fatal("a group named twice")
	}
	if l, err := r.s.ReorderTabs("ws-x", "pane-a", nil); err == nil || l != nil {
		t.Fatal("empty tab order")
	}
}

func TestReorderTabsMustCoverEachTabOnce(t *testing.T) {
	r := newRig(t)
	r.writeLayout("ws-x", `{"root":{"type":"pane","id":"pane-a","tabs":[{"id":"t1","sessionName":"s1","name":"","order":0},{"id":"t2","sessionName":"s2","name":"","order":1},{"id":"t3","sessionName":"s3","name":"","order":2}],"activeTabId":"t1"},"activePaneId":"pane-a","updatedAt":"x"}`)
	if _, err := r.s.ReorderTabs("ws-x", "pane-a", []string{"t1", "t1", "t2"}); err == nil {
		t.Fatal("a duplicate id would drop a tab")
	}
	l, err := r.s.ReorderTabs("ws-x", "pane-a", []string{"t3", "zz", "t1", "t2"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tab := range tabsOf(root(l)) {
		o, _ := tab.Number("order")
		got = append(got, tab.String("id")+":"+strconv.Itoa(int(o)))
	}
	if !reflect.DeepEqual(got, []string{"t3:0", "t1:1", "t2:2"}) {
		t.Fatal(got)
	}
}

// Ported from the previous runtime's layout-state tests (the parts the runtime runs).
func TestTabStateDiffAndOps(t *testing.T) {
	before := mustParse(t, `{"id":"tab-a","sessionName":"s-a","name":"","order":0,"cliState":"idle","lastUserMessage":"старое","agentState":{"providerId":"claude","sessionId":"s1","jsonlPath":null,"summary":null}}`)
	after := jsvalue.Clone(before).(*jsvalue.Object)
	after.Set("cliState", "busy")
	after.Set("agentState", mustParse(t, `{"providerId":"claude","sessionId":"s2","jsonlPath":"/p","summary":null}`))
	after.Delete("lastUserMessage")
	set, unset := tabStateDiff(before, after)
	if string(jsvalue.Stringify(set, "")) != `{"cliState":"busy","agentState":{"providerId":"claude","sessionId":"s2","jsonlPath":"/p","summary":null}}` || !reflect.DeepEqual(unset, []string{"lastUserMessage"}) {
		t.Fatalf("%s %v", jsvalue.Stringify(set, ""), unset)
	}
	if s2, u2 := tabStateDiff(before, jsvalue.Clone(before).(*jsvalue.Object)); s2 != nil || u2 != nil {
		t.Fatal("no change must be nil")
	}
	layout := jsvalue.NewObject("root", jsvalue.NewObject("type", "pane", "id", "pane-p", "tabs", []any{jsvalue.Clone(before)}, "activeTabId", "tab-a"))
	if !applyTabState(layout, "s-a", set, unset) || !jsvalue.Equal(tabsOf(root(layout))[0], after) {
		t.Fatal(jsvalue.Stringify(layout, ""))
	}
	if applyTabState(layout, "s-zz", set, nil) {
		t.Fatal("no such tab")
	}
	// An operation whose target is gone changes nothing.
	one := mustParse(t, `{"root":{"type":"pane","id":"pane-p","tabs":[{"id":"tab-a","sessionName":"s","name":"","order":0}],"activeTabId":"tab-a"},"activePaneId":"pane-p"}`)
	b := "tab-b"
	if patchPaneData(one, "pane-p", &b) || patchTabViewData(one, "pane-p", "tab-b", TabPatch{"name": "x"}) || reorderTabsData(one, "pane-p", []string{"tab-b"}) {
		t.Fatal("applied to a missing target")
	}
	if root(one).String("activeTabId") != "tab-a" {
		t.Fatal("changed")
	}
}

func TestRequestParsing(t *testing.T) {
	for body, want := range map[string]string{
		`{"name":5}`:                            "name must be a string",
		`{"terminalRatio":"x"}`:                 "terminalRatio must be a number",
		`{"terminalCollapsed":1}`:               "terminalCollapsed must be a boolean",
		`{"other":1}`:                           "No fields to update",
		`{"name":"  Имя ","terminalRatio":140}`: "",
	} {
		p, err := ParseTabPatch(mustParse(t, body))
		if want == "" {
			if err != nil || p["name"] != "Имя" || p["terminalRatio"] != float64(100) {
				t.Errorf("%s: %v %v", body, p, err)
			}
			continue
		}
		var e *Error
		if !errors.As(err, &e) || e.Status != 400 || e.Message != want {
			t.Errorf("%s: %v", body, err)
		}
	}
	p := ParseLayoutPatch(mustParse(t, `{"activePaneId":null,"ratioUpdate":{"path":[1,0],"ratio":30},"equalize":1,"diffSettings":{"viewMode":"split"}}`))
	if p.ActivePaneID == nil || *p.ActivePaneID != "" || !reflect.DeepEqual(p.RatioUpdate.Path, []int{1, 0}) || p.RatioUpdate.Ratio != float64(30) || !p.Equalize || p.DiffSettings == nil {
		t.Fatalf("%+v", p)
	}
}

func TestTitleAndTabName(t *testing.T) {
	for raw, want := range map[string]string{
		"bash|/home/u/proj": "proj", "zsh|/": "/", "claude|/x": "claude", "node|/x": "node", "user@host: ~/w/app": "app",
		"/srv/app/": "app", "vim file": "vim", "-zsh": "", "   ": "",
	} {
		if got := formatTabTitle(raw, nil); got != want {
			t.Errorf("formatTabTitle(%q) = %q, want %q", raw, got, want)
		}
	}
	if formatTabTitle("node|/x", "codex-cli") != "Codex" {
		t.Error("codex title")
	}
	for _, c := range []struct {
		name     string
		from, to any
		want     string
	}{
		{"", "terminal", "web-browser", "Web Browser"},
		{"Web Browser", "web-browser", "terminal", ""},
		{"Session List", "agent-sessions", "terminal", ""},
		{"Mine", "web-browser", "terminal", "Mine"},
		{"", "terminal", "claude-code", ""},
	} {
		if got := tabNameForPanelTypeChange(c.name, c.from, c.to); got != c.want {
			t.Errorf("%+v: %q", c, got)
		}
	}
}

// Ported from the previous runtime's tab metadata tests.
func TestMergeMeta(t *testing.T) {
	current := mustParse(t, `{"mode":"automation","launch":{"model":"opus","effort":"high"}}`)
	next, changed := MergeMeta(current, mustParse(t, `{"launch":{"model":"opus","effort":null}}`))
	if !changed || string(jsvalue.Stringify(next, "")) != `{"mode":"automation","launch":{"model":"opus","effort":null}}` {
		t.Fatal(string(jsvalue.Stringify(next, "")))
	}
	if e, _ := current.Object("launch").Get("effort"); e != "high" {
		t.Fatal("the stored metadata changed")
	}
	reordered := mustParse(t, `{"launch":{"model":"opus","effort":"high"},"mode":"automation"}`)
	if _, changed := MergeMeta(reordered, mustParse(t, `{"launch":{"effort":"high","model":"opus"},"mode":"automation"}`)); changed {
		t.Fatal("equal values in another key order")
	}
	if next, _ := MergeMeta(nil, jsvalue.NewObject()); next.Len() != 0 {
		t.Fatal("empty")
	}
	retired := mustParse(t, `{"mode":"design","designId":"ds_1","canvasUrl":"https://example.com/design"}`)
	next, _ = MergeMeta(retired, mustParse(t, `{"launch":{"model":"opus","effort":null},"secret":"no"}`))
	if string(jsvalue.Stringify(next, "")) != `{"mode":"design","designId":"ds_1","canvasUrl":"https://example.com/design","launch":{"model":"opus","effort":null}}` {
		t.Fatal(string(jsvalue.Stringify(next, "")))
	}
}

func TestUpdateTabMeta(t *testing.T) {
	r := newRig(t)
	session := "tk-ws-a1-pane-b2-tab-c3"
	r.writeLayout("ws-a1", `{"root":{"type":"pane","id":"pane-b2","activeTabId":"tab-c3","tabs":[{"id":"tab-c3","sessionName":"`+session+`","name":"Claude","order":0,"panelType":"claude-code"}]},"activePaneId":"pane-b2","updatedAt":"2026-09-25T00:00:00.000Z"}`)
	got, err := r.s.UpdateTabMeta(session, jsvalue.NewObject("mode", "automation"))
	if err != nil || string(jsvalue.Stringify(got, "")) != `{"mode":"automation"}` {
		t.Fatal(got, err)
	}
	written := r.readFile("workspaces/ws-a1/layout.json")
	if string(jsvalue.Stringify(tabsOf(root(written))[0].Object("meta"), "")) != `{"mode":"automation"}` {
		t.Fatal(jsvalue.Stringify(written, ""))
	}
	stamp := written.String("updatedAt")
	if got, _ := r.s.UpdateTabMeta(session, jsvalue.NewObject("mode", "automation")); string(jsvalue.Stringify(got, "")) != `{"mode":"automation"}` {
		t.Fatal(got)
	}
	if r.readFile("workspaces/ws-a1/layout.json").String("updatedAt") != stamp {
		t.Fatal("an unchanged patch touched the file")
	}
	if got, _ := r.s.UpdateTabMeta("tk-ws-a1-pane-b2-tab-gone", jsvalue.NewObject("mode", "automation")); got != nil {
		t.Fatal("missing tab")
	}
	if got, _ := r.s.UpdateTabMeta("not-a-session", jsvalue.NewObject("mode", "automation")); got != nil {
		t.Fatal("not a session")
	}
}

// Ported from the previous runtime's recovered-session tests.
func TestStoreRecoveredSession(t *testing.T) {
	const id = "425055ec-bbc8-4e3c-87d0-36fa74c1cccb"
	session := "tk-ws-a1-pane-b2-tab-c3"
	transcript := "/home/u/.claude/projects/-srv-admin/" + id + ".jsonl"
	setup := func(state string) *rig {
		r := newRig(t)
		r.writeLayout("ws-a1", `{"root":{"type":"pane","id":"pane-b2","activeTabId":"tab-c3","tabs":[{"id":"tab-c3","sessionName":"`+session+`","name":"Claude","order":0,"panelType":"claude-code","agentState":`+state+`}]},"activePaneId":"pane-b2","updatedAt":"2026-10-12T00:00:00.000Z"}`)
		return r
	}
	tab := func(r *rig) *jsvalue.Object { return tabsOf(root(r.readFile("workspaces/ws-a1/layout.json")))[0] }

	lost := setup(`{"providerId":"claude","sessionId":null,"jsonlPath":"` + transcript + `","summary":"x"}`)
	if v, _ := Claude.readField(tab(lost), "sessionId"); v != id {
		t.Fatalf("the id is read back from the transcript path: %v", v)
	}
	if err := lost.s.StoreRecoveredAgentSession(session, Claude, id, transcript); err != nil {
		t.Fatal(err)
	}
	healed := tab(lost)
	if s, _ := healed.Object("agentState").Get("sessionId"); s != id || healed.String("claudeSessionId") != id {
		t.Fatal(jsvalue.Stringify(healed, ""))
	}

	stored := setup(`{"providerId":"claude","sessionId":"` + id + `","jsonlPath":"` + transcript + `","summary":null}`)
	if err := stored.s.StoreRecoveredAgentSession(session, Claude, id, transcript); err != nil {
		t.Fatal(err)
	}
	if got := stored.readFile("workspaces/ws-a1/layout.json").String("updatedAt"); got != "2026-10-12T00:00:00.000Z" {
		t.Fatal("an equal session touched the file")
	}
}

func TestBrokenFiles(t *testing.T) {
	r := newRig(t)
	file := r.writeLayout("ws-x", `{"root": `)
	if r.s.Layout("ws-x") != nil {
		t.Fatal("a broken layout reads as absent")
	}
	if b, err := os.ReadFile(file + ".bak"); err != nil || string(b) != `{"root": ` {
		t.Fatal("no .bak copy")
	}
	r.writeLayout("ws-y", `null`)
	if r.s.Layout("ws-y") != nil {
		t.Fatal("null")
	}
	if _, err := os.Stat(filepath.Join(r.data, "workspaces", "ws-y", "layout.json.bak")); err == nil {
		t.Fatal("valid JSON is not backed up")
	}
	_ = os.WriteFile(filepath.Join(r.data, "workspaces.json"), []byte(`{"workspaces":"no"}`), 0o600)
	if r.s.Workspace("ws-x") != nil {
		t.Fatal("not a list")
	}
	if _, err := os.Stat(filepath.Join(r.data, "workspaces.json.bak")); err != nil {
		t.Fatal("a document that is not a workspace list is backed up")
	}
	if !ValidWorkspaceID("ws-a_b-1") || ValidWorkspaceID("../x") || ValidWorkspaceID("") || ValidWorkspaceID("a/b") {
		t.Fatal("workspace ids")
	}
	if _, err := r.s.GetLayout("../x", ""); err == nil {
		t.Fatal("a workspace id outside the data directory")
	}
}

func TestWritesAreAtomicPrivateAndUnchangedOnesSkipped(t *testing.T) {
	r := newRig(t)
	r.writeLayout("x1", `{"root":{"type":"pane","id":"pane-a","tabs":[{"id":"t1","sessionName":"s1","name":"","order":0}],"activeTabId":"t1"},"activePaneId":"pane-a","updatedAt":"x"}`)
	l, err := r.s.PatchPane("x1", "pane-a", nil)
	if err != nil || l == nil {
		t.Fatal(err)
	}
	file := filepath.Join(r.data, "workspaces", "x1", "layout.json")
	st, _ := os.Stat(file)
	raw, _ := os.ReadFile(file)
	if st.Mode().Perm() != 0o600 || strings.HasSuffix(string(raw), "\n") {
		t.Fatalf("mode %v, newline at end %v", st.Mode(), strings.HasSuffix(string(raw), "\n"))
	}
	entries, _ := os.ReadDir(filepath.Dir(file))
	if len(entries) != 1 {
		t.Fatalf("leftovers: %v", entries)
	}
	// A workspace id without the usual prefix is announced too.
	if len(r.rec.events) != 1 || !reflect.DeepEqual(r.rec.events[0], map[string]any{"type": "layout", "workspaceId": "x1"}) {
		t.Fatalf("events %v", r.rec.events)
	}
	r.rec.events = nil
	if _, err := r.s.PatchPane("x1", "pane-a", nil); err != nil || len(r.rec.events) != 0 {
		t.Fatalf("an unchanged layout was written again: %v", r.rec.events)
	}
	// The cluster's change clears the cache, so the next write goes out.
	r.s.LayoutChangedByCluster("x1")
	r.rec.events = nil
	if _, err := r.s.PatchPane("x1", "pane-a", nil); err != nil || len(r.rec.events) != 1 {
		t.Fatalf("after a cluster change: %v", r.rec.events)
	}
	if err := r.s.StateChanged("workspaces/x1/layout.json"); err != nil {
		t.Fatal(err)
	}
	var e *Error
	if err := r.s.StateChanged("workspaces/../layout.json"); !errors.As(err, &e) || e.Message != "unknown state file" {
		t.Fatal(err)
	}
}

func TestConcurrentTabsOnOneWorkspace(t *testing.T) {
	r := newRig(t)
	r.writeLayout("ws-x", `{"root":{"type":"pane","id":"pane-a","tabs":[],"activeTabId":null},"activePaneId":"pane-a","updatedAt":"x"}`)
	r.s.cfg.NewID = func() string { return nanoid(6) }
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.s.AddTab("ws-x", "pane-a", TabOptions{}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := len(tabsOf(root(r.s.Layout("ws-x")))); n != 20 {
		t.Fatalf("%d tabs", n)
	}
}

// fakeAgent is the machine agent's loopback state routes.
type fakeAgent struct {
	mu       sync.Mutex
	requests []map[string]any
	answer   func(route string, body map[string]any) (int, string)
}

func (a *fakeAgent) start(t *testing.T, data string) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		body["_route"] = r.URL.Path
		body["_token"] = r.Header.Get("x-agent-local-token")
		body["_raw"] = string(raw)
		a.mu.Lock()
		a.requests = append(a.requests, body)
		a.mu.Unlock()
		status, out := a.answer(r.URL.Path, body)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, out)
	}))
	t.Cleanup(srv.Close)
	port := srv.URL[strings.LastIndexByte(srv.URL, ':')+1:]
	_ = os.MkdirAll(data, 0o755)
	_ = os.WriteFile(filepath.Join(data, "agent.json"), []byte(`{"port":`+port+`,"token":"tok"}`), 0o600)
	_ = os.WriteFile(filepath.Join(data, "state-mode.json"), []byte(`{"owned":true,"headless":false,"at":"x"}`+"\n"), 0o600)
}

func TestOwnedCommitAdoptsTheClustersCopy(t *testing.T) {
	r := newRig(t)
	agent := &fakeAgent{answer: func(route string, body map[string]any) (int, string) {
		content := body["content"].(map[string]any)
		content["stateVersion"] = 7
		content["fromCluster"] = true
		delete(content, "stateDirty")
		c, _ := json.Marshal(content)
		return 200, `{"ok":true,"version":7,"content":` + string(c) + `}`
	}}
	agent.start(t, r.data)
	r.writeLayout("ws-x", `{"root":{"type":"pane","id":"pane-a","tabs":[{"id":"t1","sessionName":"tk-ws-x-pane-a-t1","name":"","order":0}],"activeTabId":"t1"},"activePaneId":"pane-a","updatedAt":"x","stateVersion":5}`)
	l, err := r.s.PatchLayout("ws-x", LayoutPatch{DiffSettings: jsvalue.NewObject("viewMode", "split")})
	if err != nil {
		t.Fatal(err)
	}
	req := agent.requests[0]
	if req["_route"] != "/state/commit" || req["_token"] != "tok" || req["name"] != "workspaces/ws-x/layout.json" || req["base"] != float64(5) {
		t.Fatalf("%v", req)
	}
	if !strings.HasPrefix(req["_raw"].(string), `{"name":"workspaces/ws-x/layout.json","base":5,"content":{"root":`) {
		t.Fatalf("body %s", req["_raw"])
	}
	stored := r.readFile("workspaces/ws-x/layout.json")
	if v, _ := stored.Get("stateVersion"); v != float64(7) || stored.Has("stateDirty") || !stored.Has("fromCluster") || !l.Has("fromCluster") {
		t.Fatalf("%s", jsvalue.Stringify(stored, ""))
	}
	// Unchanged after the adopted copy: nothing goes to the cluster.
	if _, err := r.s.PatchLayout("ws-x", LayoutPatch{DiffSettings: jsvalue.NewObject("viewMode", "split")}); err != nil || len(agent.requests) != 1 {
		t.Fatalf("%d requests", len(agent.requests))
	}
}

func TestOwnedCommitFailureMarksDirty(t *testing.T) {
	for name, answer := range map[string]func(string, map[string]any) (int, string){
		"refused": func(string, map[string]any) (int, string) {
			return 502, `{"ok":false,"error":"the cluster did not answer"}`
		},
		"no content": func(string, map[string]any) (int, string) { return 200, `{"ok":true,"version":3}` },
		"garbage":    func(string, map[string]any) (int, string) { return 200, `nope` },
	} {
		r := newRig(t)
		(&fakeAgent{answer: answer}).start(t, r.data)
		r.writeLayout("ws-x", `{"root":{"type":"pane","id":"pane-a","tabs":[],"activeTabId":null},"activePaneId":"pane-a","updatedAt":"x"}`)
		if _, err := r.s.PatchLayout("ws-x", LayoutPatch{Equalize: true, DiffSettings: jsvalue.NewObject("a", 1)}); err != nil {
			t.Fatal(err)
		}
		if d, _ := r.readFile("workspaces/ws-x/layout.json").Get("stateDirty"); d != true {
			t.Errorf("%s: not marked dirty", name)
		}
	}
	// No agent.json at all.
	r := newRig(t)
	_ = os.MkdirAll(r.data, 0o755)
	_ = os.WriteFile(filepath.Join(r.data, "state-mode.json"), []byte(`{"owned":true}`), 0o600)
	if err := r.s.UpdateActive(jsvalue.NewObject("sidebarWidth", 300)); err != nil {
		t.Fatal(err)
	}
	if d, _ := r.readFile("workspaces.json").Get("stateDirty"); d != true {
		t.Fatal("workspace list not marked dirty")
	}
}

func TestOwnedTabChangeGoesAsAPatch(t *testing.T) {
	r := newRig(t)
	refuse := false
	agent := &fakeAgent{}
	agent.answer = func(route string, body map[string]any) (int, string) {
		if refuse {
			return 502, `{"ok":false,"error":"no such tab in the stored layout"}`
		}
		return 200, `{"ok":true,"version":9}`
	}
	agent.start(t, r.data)
	session := "tk-ws-x-pane-a-tab-1"
	r.writeLayout("ws-x", `{"root":{"type":"pane","id":"pane-a","tabs":[{"id":"tab-1","sessionName":"`+session+`","name":"","order":0,"lastUserMessage":"old"}],"activeTabId":"tab-1"},"activePaneId":"pane-a","updatedAt":"x","stateVersion":4}`)
	if err := r.s.UpdateTabCliStatus(session, "busy", nil, nil); err != nil {
		t.Fatal(err)
	}
	req := agent.requests[0]
	if req["_route"] != "/state/patch" || req["sessionName"] != session || !reflect.DeepEqual(req["set"], map[string]any{"cliState": "busy", "lastEventName": nil}) || !reflect.DeepEqual(req["unset"], []any{}) {
		t.Fatalf("%v", req)
	}
	stored := r.readFile("workspaces/ws-x/layout.json")
	if v, _ := stored.Get("stateVersion"); v != float64(4) || stored.Has("stateDirty") {
		t.Fatalf("the file keeps its version until the cluster's copy arrives: %s", jsvalue.Stringify(stored, ""))
	}
	refuse = true
	if err := r.s.UpdateTabLastUserMessage(session, nil); err != nil {
		t.Fatal(err)
	}
	if d, _ := r.readFile("workspaces/ws-x/layout.json").Get("stateDirty"); d != true {
		t.Fatal("a refused patch marks the file dirty")
	}
	// The tab path always writes, so the same change written twice goes out twice.
	n := len(agent.requests)
	if err := r.s.UpdateTabLastUserMessage(session, "again"); err != nil || len(agent.requests) != n+1 {
		t.Fatal(err)
	}
}

func TestRemoveLayoutCommitsDeletion(t *testing.T) {
	r := newRig(t)
	agent := &fakeAgent{answer: func(string, map[string]any) (int, string) { return 200, `{"ok":true,"version":0,"content":null}` }}
	agent.start(t, r.data)
	r.writeLayout("ws-x", `{"root":{"type":"pane","id":"p","tabs":[],"activeTabId":null},"activePaneId":"p","updatedAt":"x"}`)
	if err := r.s.RemoveLayout("ws-x"); err != nil {
		t.Fatal(err)
	}
	if req := agent.requests[0]; req["_raw"] != `{"name":"workspaces/ws-x/layout.json","base":0,"content":null}` {
		t.Fatalf("%v", req["_raw"])
	}
	if _, err := os.Stat(filepath.Join(r.data, "workspaces", "ws-x")); err == nil {
		t.Fatal("folder kept")
	}
	if !reflect.DeepEqual(r.rec.reconciles, []any{[]any{"removed", "ws-x"}}) {
		t.Fatal(r.rec.reconciles)
	}
}

func TestMigrationFromTheSingleLayout(t *testing.T) {
	r := newRig(t)
	_ = os.MkdirAll(r.data, 0o755)
	_ = os.WriteFile(filepath.Join(r.data, "tabs.json"), []byte(`{"tabs":[{"id":"tab-1","sessionName":"tk-old","name":"","order":0}],"activeTabId":"tab-1"}`), 0o600)
	r.tm.sessions["tk-old"] = true
	if err := r.s.Init(); err != nil {
		t.Fatal(err)
	}
	list := r.readFile("workspaces.json")
	ws := workspacesOf(list)
	if len(ws) != 1 || ws[0].String("id") != "ws-default" || ws[0].String("name") != "default" || ws[0].Array("directories")[0] != r.home {
		t.Fatal(string(jsvalue.Stringify(list, "")))
	}
	layout := r.readFile("workspaces/ws-default/layout.json")
	if tabsOf(root(layout))[0].String("sessionName") != "tk-old" {
		t.Fatal(string(jsvalue.Stringify(layout, "")))
	}
}

// With tmux on the machine, the stores drive a private tmux server of this test only.
func TestWithRealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	socket := fmt.Sprintf("hs-test-%d", rand.IntN(1_000_000_000))
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", socket, "kill-server").Run()
		dir := os.Getenv("TMUX_TMPDIR")
		if dir == "" {
			dir = "/tmp"
		}
		_ = os.Remove(filepath.Join(dir, "tmux-"+strconv.Itoa(os.Getuid()), socket))
	})
	home := t.TempDir()
	tm := tmux.NewConfig(tmux.Config{Names: tmux.Names{Socket: socket, Prefix: "hs-"}, ConfigFile: "/dev/null", Home: home,
		Env: []string{"HOME=" + home, "SHELL=/bin/sh"}})
	s := New(Config{DataDir: filepath.Join(home, "data"), Tmux: FromTmux(tm), Home: home})
	ws, err := s.CreateWorkspace(home, "", "")
	if err != nil {
		t.Fatal(err)
	}
	l, _ := s.GetLayout(ws.String("id"), "")
	session := tabsOf(root(l))[0].String("sessionName")
	if !tm.HasSession(session) || !strings.HasPrefix(session, "hs-"+ws.String("id")+"-pane-") {
		t.Fatalf("session %s", session)
	}
	deadline := time.Now().Add(5 * time.Second)
	for s.cfg.Tmux.SessionCwd(session) != home && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if got := s.cfg.Tmux.SessionCwd(session); got != home {
		t.Fatalf("live folder %q", got)
	}
	tab, err := s.AddTab(ws.String("id"), root(l).String("id"), TabOptions{Cwd: home})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveTab(ws.String("id"), root(l).String("id"), tab.String("id")); err != nil || tm.HasSession(tab.String("sessionName")) {
		t.Fatalf("remove: %v", err)
	}
	if err := s.DeleteWorkspace(ws.String("id")); err != nil || tm.HasSession(session) {
		t.Fatalf("delete: %v", err)
	}
}
