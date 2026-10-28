package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

// fakeTmux is tmux as a set of session names, as testdata/gen/stub-tmux.mjs is for the previous runtime.
type fakeTmux struct {
	mu       sync.Mutex
	names    *tmux.Tmux
	home     string
	sessions map[string]bool
	cwd      *string
	command  string
	calls    [][]any
	// createErr: CreateSession fails with it, the session made anyway (a tmux that answered too late).
	createErr error
}

func newFakeTmux(home string) *fakeTmux {
	cwd := "/live/cwd"
	return &fakeTmux{
		names:    tmux.New(tmux.Names{Socket: "unused", Prefix: "tk-", LegacyPrefix: "pt-"}),
		home:     home,
		sessions: map[string]bool{},
		cwd:      &cwd,
		command:  "bash",
	}
}

func (f *fakeTmux) CreateSession(name string, cols, rows int, cwd string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, []any{"create", name, cols, rows, cwd})
	f.sessions[name] = true
	return f.createErr
}
func (f *fakeTmux) KillSession(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, []any{"kill", name})
	delete(f.sessions, name)
}
func (f *fakeTmux) HasSession(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessions[name]
}
func (f *fakeTmux) SendKeys(name, line string) error {
	f.calls = append(f.calls, []any{"send", name, line})
	return nil
}
func (f *fakeTmux) ListSessions() []string {
	var out []string
	for s := range f.sessions {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
func (f *fakeTmux) Names() tmux.Names { return f.names.Names() }
func (f *fakeTmux) SessionName(ws, pane, tab string) string {
	return f.names.SessionName(ws, pane, tab)
}
func (f *fakeTmux) ParseSessionName(n string) (string, string, string, bool) {
	return f.names.ParseSessionName(n)
}
func (f *fakeTmux) ResolveExistingDir(p string) string {
	if p == "" {
		return f.home
	}
	for c := p; ; {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
		parent := filepath.Dir(c)
		if parent == c {
			return f.home
		}
		c = parent
	}
}
func (f *fakeTmux) SessionCwd(string) string {
	if f.cwd == nil {
		return ""
	}
	return *f.cwd
}
func (f *fakeTmux) PaneCurrentCommand(string) string { return f.command }

type recorder struct {
	events     []any
	reconciles []any
}

func (r *recorder) Layout(id string) {
	r.events = append(r.events, map[string]any{"type": "layout", "workspaceId": id})
}
func (r *recorder) Workspaces() { r.events = append(r.events, map[string]any{"type": "workspace"}) }
func (r *recorder) ReconcileWorkspaceTabs(id string, tabs []string) {
	if tabs == nil {
		tabs = []string{}
	}
	r.reconciles = append(r.reconciles, []any{"tabs", id, tabs})
}
func (r *recorder) RemoveWorkspaceTabs(id string) {
	r.reconciles = append(r.reconciles, []any{"removed", id})
}

// sequentialIDs is testdata/gen/stub-nanoid.mjs: id0001, id0002, …
func sequentialIDs() func() string {
	n := 0
	return func() string {
		n++
		s := "id" + strings.Repeat("0", max(0, 4-len(strconv.FormatInt(int64(n), 36)))) + strconv.FormatInt(int64(n), 36)
		return s[len(s)-6:]
	}
}

type goldenStep struct {
	Name       string
	Result     string
	Events     json.RawMessage
	Reconciles json.RawMessage
	Calls      json.RawMessage
	Files      map[string]string
}

// canon re-encodes JSON so two encoders' output compares as data.
func canon(t *testing.T, v any) string {
	t.Helper()
	var raw []byte
	switch x := v.(type) {
	case json.RawMessage:
		raw = x
	default:
		var err error
		if raw, err = json.Marshal(v); err != nil {
			t.Fatal(err)
		}
	}
	var back any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(back)
	return string(out)
}

// The same steps as testdata/gen/gen.mjs, through this package; every step must leave the same bytes on disk, return
// the same value, broadcast the same events and ask the same of tmux as the previous runtime did (testdata/golden.json).
func TestGoldenAgainstPreviousRuntime(t *testing.T) {
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct{ Steps []goldenStep }
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}

	rootDir, _ := filepath.EvalSymlinks(t.TempDir())
	home := filepath.Join(rootDir, "home")
	data := filepath.Join(home, "data")
	A, B, C := filepath.Join(rootDir, "proj-a"), filepath.Join(rootDir, "proj-b"), filepath.Join(rootDir, "proj-c")
	for _, d := range []string{home, A, B, C, filepath.Join(A, "sub")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	tm := newFakeTmux(home)
	rec := &recorder{}
	s := New(Config{
		DataDir: data, Tmux: tm, Broadcast: rec, Reconciler: rec, Home: home, MetaKey: "meta",
		Now: func() time.Time { return time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC) }, NewID: sequentialIDs(),
	})
	scrub := func(x string) string { return strings.ReplaceAll(x, rootDir, "$ROOT") }

	type result = any
	layoutOrNull := func(l *jsvalue.Object, err error) result {
		var e *Error
		if errors.As(err, &e) && e.Status == 404 {
			return nil
		}
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	okOr := func(err error) result {
		var e *Error
		if errors.As(err, &e) && (e.Status == 404 || e.Status == 400) {
			return false
		}
		if err != nil {
			t.Fatal(err)
		}
		return true
	}
	errOr := func(v any, err error) result {
		if err != nil {
			return jsvalue.NewObject("error", err.Error())
		}
		return v
	}
	none := func(err error) result {
		if err != nil {
			t.Fatal(err)
		}
		return nil
	}
	ids := map[string]string{}
	str := func(v string) *string { return &v }
	layoutOf := func(id string) *jsvalue.Object { return s.readLayout(id) }

	steps := []struct {
		name string
		run  func() result
	}{
		{"init empty", func() result { return none(s.Init()) }},
		{"create workspace", func() result {
			w, err := s.CreateWorkspace(A, " Alpha ", "")
			ids["w1"] = w.String("id")
			return errOr(w, err)
		}},
		{"create second workspace", func() result {
			w, err := s.CreateWorkspace(B, "", "")
			ids["w2"] = w.String("id")
			return errOr(w, err)
		}},
		{"create in a missing folder", func() result { return errOr(s.CreateWorkspace(filepath.Join(A, "missing"), "", "")) }},
		{"create on a file", func() result {
			_ = os.WriteFile(filepath.Join(rootDir, "file"), []byte("x"), 0o644)
			return errOr(s.CreateWorkspace(filepath.Join(rootDir, "file"), "", ""))
		}},
		{"get existing layout", func() result {
			l, err := s.GetLayout(ids["w1"], "")
			ids["p1"] = root(l).String("id")
			ids["t1"] = tabsOf(root(l))[0].String("id")
			return errOr(l, err)
		}},
		{"split", func() result {
			l, err := s.SplitPane(ids["w1"], ids["p1"], "horizontal", filepath.Join(A, "sub"), "")
			ids["p2"] = l.String("activePaneId")
			return errOr(l, err)
		}},
		{"split a browser", func() result {
			l, err := s.SplitPane(ids["w1"], ids["p2"], "vertical", "", "web-browser")
			ids["p3"] = l.String("activePaneId")
			return errOr(l, err)
		}},
		{"split a missing pane", func() result { return layoutOrNull(s.SplitPane(ids["w1"], "pane-nope", "vertical", "", "")) }},
		{"add agent tab", func() result {
			tab, err := s.AddTab(ids["w1"], ids["p1"], TabOptions{Name: "  Named  ", Cwd: A, PanelType: "claude-code"})
			ids["tc"], ids["sc"] = tab.String("id"), tab.String("sessionName")
			return errOr(tab, err)
		}},
		{"add tab with command", func() result {
			tab, err := s.AddTab(ids["w1"], ids["p1"], TabOptions{Command: "echo hi"})
			ids["t3"] = tab.String("id")
			return errOr(tab, err)
		}},
		{"add tab to a missing pane", func() result { return layoutOrNull(s.AddTab(ids["w1"], "pane-nope", TabOptions{})) }},
		{"panel type rename to browser", func() result {
			return layoutOrNull(s.PatchTab(ids["w1"], ids["p1"], ids["t3"], TabPatch{"panelType": "web-browser"}))
		}},
		{"panel type rename back", func() result {
			return layoutOrNull(s.PatchTab(ids["w1"], ids["p1"], ids["t3"], TabPatch{"panelType": "terminal"}))
		}},
		{"patch tab view and live fields", func() result {
			return layoutOrNull(s.PatchTab(ids["w1"], ids["p1"], ids["t1"], TabPatch{
				"cwd": "/body/cwd", "title": "body title", "terminalRatio": float64(40), "terminalCollapsed": true,
				"webUrl": "http://localhost:3000", "lastCommand": "npm test", "name": "Main",
			}))
		}},
		{"patch missing tab", func() result {
			return layoutOrNull(s.PatchTab(ids["w1"], ids["p1"], "tab-nope", TabPatch{"name": "x"}))
		}},
		{"ratio", func() result {
			return layoutOrNull(s.PatchLayout(ids["w1"], LayoutPatch{RatioUpdate: &RatioUpdate{Path: []int{}, Ratio: float64(70)}}))
		}},
		{"equalize", func() result { return layoutOrNull(s.PatchLayout(ids["w1"], LayoutPatch{Equalize: true})) }},
		{"equalize again is no write", func() result { return layoutOrNull(s.PatchLayout(ids["w1"], LayoutPatch{Equalize: true})) }},
		{"diff settings and active pane", func() result {
			return layoutOrNull(s.PatchLayout(ids["w1"], LayoutPatch{DiffSettings: jsvalue.NewObject("viewMode", "unified", "panelOpen", true), ActivePaneID: str(ids["p1"])}))
		}},
		{"active pane missing", func() result {
			return layoutOrNull(s.PatchLayout(ids["w1"], LayoutPatch{ActivePaneID: str("pane-nope")}))
		}},
		{"pane active tab", func() result { return layoutOrNull(s.PatchPane(ids["w1"], ids["p1"], str(ids["t1"]))) }},
		{"pane active tab missing", func() result { return layoutOrNull(s.PatchPane(ids["w1"], ids["p1"], str("tab-nope"))) }},
		{"reorder tabs", func() result {
			return layoutOrNull(s.ReorderTabs(ids["w1"], ids["p1"], []string{ids["t3"], ids["tc"], ids["t1"]}))
		}},
		{"reorder tabs not covering", func() result {
			return layoutOrNull(s.ReorderTabs(ids["w1"], ids["p1"], []string{ids["t3"], "tab-x"}))
		}},
		{"move tab", func() result { return layoutOrNull(s.MoveTab(ids["w1"], ids["t3"], ids["p1"], ids["p2"], 0)) }},
		{"cli status", func() result { return none(s.UpdateTabCliStatus(ids["sc"], "busy", nil, "prompt-submit")) }},
		{"cli status unchanged", func() result { return none(s.UpdateTabCliStatus(ids["sc"], "busy", nil, "prompt-submit")) }},
		{"cli status dismissed", func() result {
			return none(s.UpdateTabCliStatus(ids["sc"], "idle", Value(float64(1760000000000)), nil))
		}},
		{"last user message", func() result { return none(s.UpdateTabLastUserMessage(ids["sc"], "привет \"мир\"\n")) }},
		{"claude agent state", func() result {
			return none(s.UpdateTabAgentState(ids["sc"], Claude, AgentState{SessionID: Value("sid-1"), JsonlPath: Value("/t/x.jsonl"), Summary: Value("sum"), LastUserMessage: Value("q")}))
		}},
		{"claude session cleared", func() result { return none(s.UpdateTabAgentSessionID(ids["sc"], Claude, nil)) }},
		{"claude summary", func() result { return none(s.UpdateTabAgentSummary(ids["sc"], Claude, "new sum")) }},
		{"recovered session", func() result {
			return none(s.StoreRecoveredAgentSession(ids["sc"], Claude, "425055ec-bbc8-4e3c-87d0-36fa74c1cccb", "/p/425055ec-bbc8-4e3c-87d0-36fa74c1cccb.jsonl"))
		}},
		{"recovered session again", func() result {
			return none(s.StoreRecoveredAgentSession(ids["sc"], Claude, "425055ec-bbc8-4e3c-87d0-36fa74c1cccb", "/p/425055ec-bbc8-4e3c-87d0-36fa74c1cccb.jsonl"))
		}},
		{"codex replaces a foreign agent state", func() result {
			return none(s.UpdateTabAgentState(ids["sc"], Codex, AgentState{SessionID: Value("c-1"), Summary: Value(nil)}))
		}},
		{"writer for a missing session", func() result { return none(s.UpdateTabLastUserMessage("tk-ws-nope-pane-a-tab-b", "x")) }},
		{"close pane with focus", func() result { return layoutOrNull(s.ClosePane(ids["w1"], ids["p2"])) }},
		{"close missing pane", func() result { return layoutOrNull(s.ClosePane(ids["w1"], "pane-nope")) }},
		{"remove tab", func() result { return okOr(s.RemoveTab(ids["w1"], ids["p1"], ids["t1"])) }},
		{"remove missing tab", func() result { return okOr(s.RemoveTab(ids["w1"], ids["p1"], "tab-nope")) }},
		{"restart a live tab", func() result { return okOr(s.RestartTab(ids["w1"], ids["p1"], ids["tc"], "claude")) }},
		{"restart with a lost folder", func() result {
			tm.cwd = nil
			if _, err := s.PatchTab(ids["w1"], ids["p1"], ids["tc"], TabPatch{"cwd": filepath.Join(A, "gone", "deeper"), "lastCommand": "npm start"}); err != nil {
				t.Fatal(err)
			}
			cwd := "/live/cwd"
			tm.cwd = &cwd
			delete(tm.sessions, ids["sc"])
			return okOr(s.RestartTab(ids["w1"], ids["p1"], ids["tc"], "npm start"))
		}},
		{"restart with its folder", func() result {
			delete(tm.sessions, ids["sc"])
			return okOr(s.RestartTab(ids["w1"], ids["p1"], ids["tc"], "claude --resume"))
		}},
		{"first pane tabs", func() result {
			tabs, active := s.FirstPaneTabs(ids["w1"])
			arr := []any{}
			for _, tab := range tabs {
				arr = append(arr, tab)
			}
			return jsvalue.NewObject("tabs", arr, "activeTabId", active)
		}},
		{"group", func() result {
			g, err := s.CreateGroup(" Team ")
			ids["g1"] = g.String("id")
			return errOr(g, err)
		}},
		{"group without a name", func() result {
			g, err := s.CreateGroup("  ")
			ids["g2"] = g.String("id")
			return errOr(g, err)
		}},
		{"workspace into a group", func() result { return okOr(s.SetWorkspaceGroup(ids["w2"], str(ids["g1"]))) }},
		{"reorder workspaces", func() result {
			return okOr(s.ReorderWorkspaces([]ReorderItem{{ID: ids["w2"]}, {ID: ids["w1"], GroupID: str(ids["g2"]), HasGroup: true}}))
		}},
		{"reorder workspaces bad", func() result { return okOr(s.ReorderWorkspaces([]ReorderItem{{ID: ids["w2"]}})) }},
		{"rename workspace", func() result { return errOr(s.RenameWorkspace(ids["w1"], "Alpha 2")) }},
		{"active and sidebar", func() result {
			return none(s.UpdateActive(jsvalue.NewObject("activeWorkspaceId", ids["w2"], "sidebarWidth", 300, "sidebarCollapsed", true)))
		}},
		{"active unchanged", func() result { return none(s.UpdateActive(jsvalue.NewObject("activeWorkspaceId", ids["w2"]))) }},
		{"reorder groups", func() result { return okOr(s.ReorderGroups([]string{ids["g2"], ids["g1"]})) }},
		{"collapse group", func() result { return okOr(s.SetGroupCollapsed(ids["g1"], true)) }},
		{"rename group", func() result { return errOr(s.RenameGroup(ids["g2"], " Ops ")) }},
		{"ungroup", func() result { return okOr(s.UngroupGroup(ids["g1"])) }},
		{"workspaces view", func() result { return s.Workspaces() }},
		{"third workspace", func() result {
			w, err := s.CreateWorkspace(C, "Gamma", "")
			ids["w3"] = w.String("id")
			return errOr(w, err)
		}},
		{"delete workspace", func() result { return okOr(s.DeleteWorkspace(ids["w2"])) }},
		{"delete missing workspace", func() result { return okOr(s.DeleteWorkspace("ws-nope")) }},
		{"legacy workspaces file", func() result {
			file := filepath.Join(data, "workspaces.json")
			raw, _ := os.ReadFile(file)
			v, _ := jsvalue.Parse(raw)
			d := v.(*jsvalue.Object)
			list := workspacesOf(d)
			list[0].Set("directory", "/legacy")
			list[0].Delete("directories")
			list[0].Set("order", 3)
			list[1].Set("groupId", "grp-gone")
			list[1].Set("extra", jsvalue.NewObject("kept", []any{float64(1), "two"}))
			groupsOf(d)[0].Set("order", 1)
			d.Set("custom", "kept")
			_ = os.WriteFile(file, jsvalue.Stringify(d, "  "), 0o600)
			s.WorkspacesChangedByCluster()
			return none(s.UpdateActive(jsvalue.NewObject("sidebarWidth", 250)))
		}},
		{"layout with unknown fields", func() result {
			file := filepath.Join(data, "workspaces", ids["w1"], "layout.json")
			d := layoutOf(ids["w1"])
			d.Set("custom", jsvalue.NewObject("b", 1, "a", []any{true, nil}))
			a, _, _ := children(root(d))
			tabsOf(a)[0].Set("extraField", "kept")
			d.Set("10", "ten")
			d.Set("2", "two")
			_ = os.WriteFile(file, jsvalue.Stringify(d, ""), 0o600)
			s.LayoutChangedByCluster(ids["w1"])
			return layoutOrNull(s.PatchPane(ids["w1"], ids["p1"], str(ids["tc"])))
		}},
		{"garbled layout", func() result {
			_ = os.WriteFile(filepath.Join(data, "workspaces", ids["w3"], "layout.json"), []byte(`{"root": `), 0o600)
			return errOr(s.GetLayout(ids["w3"], C))
		}},
		{"cross-check at start", func() result {
			l := layoutOf(ids["w1"])
			delete(tm.sessions, ids["sc"])
			tm.sessions["tk-"+ids["w1"]+"-pane-zz-tab-orphan"] = true
			tm.sessions["pt-"+ids["w1"]+"-old"] = true
			tm.sessions["tk-shell-tab-x"] = true
			tm.sessions["tk-"+ids["w3"]+"-pane-q-tab-q"] = true
			return len(panes(root(l)))
		}},
		{"init with tmux", func() result { return none(s.Init()) }},
		{"remove layout", func() result { return none(s.RemoveLayout(ids["w3"])) }},
	}
	if len(steps) != len(golden.Steps) {
		t.Fatalf("%d steps here, %d in golden.json", len(steps), len(golden.Steps))
	}
	for i, st := range steps {
		want := golden.Steps[i]
		if want.Name != st.name {
			t.Fatalf("step %d is %q here, %q in golden.json", i, st.name, want.Name)
		}
		rec.events, rec.reconciles, tm.calls = nil, nil, nil
		res := st.run()
		got := "null"
		switch r := res.(type) {
		case nil:
		case *jsvalue.Object:
			if r != nil {
				got = string(jsvalue.Stringify(r, ""))
			}
		case bool, int:
			b, _ := json.Marshal(r)
			got = string(b)
		}
		if scrub(got) != want.Result {
			t.Errorf("%s: result\n got %s\nwant %s", st.name, scrub(got), want.Result)
		}
		// The previous runtime brought a workspace's folders in line asynchronously, so events of one step may interleave.
		if g, w := sortedJSONList(t, canon(t, orEmpty(rec.events))), sortedJSONList(t, canon(t, want.Events)); g != w {
			t.Errorf("%s: events\n got %s\nwant %s", st.name, g, w)
		}
		if g, w := canon(t, orEmpty(rec.reconciles)), canon(t, want.Reconciles); g != w {
			t.Errorf("%s: reconciles\n got %s\nwant %s", st.name, g, w)
		}
		var calls []any
		for _, c := range tm.calls {
			cc := make([]any, len(c))
			for j, x := range c {
				if s, ok := x.(string); ok {
					x = scrub(s)
				}
				cc[j] = x
			}
			calls = append(calls, cc)
		}
		g, w := canon(t, orEmpty(calls)), canon(t, want.Calls)
		if st.name == "close pane with focus" { // its sessions end in parallel
			g, w = sortedJSONList(t, g), sortedJSONList(t, w)
		}
		if g != w {
			t.Errorf("%s: tmux\n got %s\nwant %s", st.name, g, w)
		}
		files := snapshot(t, data, scrub)
		var names []string
		for n := range want.Files {
			names = append(names, n)
		}
		for n := range files {
			if _, ok := want.Files[n]; !ok {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		for _, n := range names {
			if files[n] != want.Files[n] {
				t.Errorf("%s: %s\n got %q\nwant %q", st.name, n, files[n], want.Files[n])
			}
		}
		if t.Failed() {
			t.FailNow()
		}
	}
}

func sortedJSONList(t *testing.T, s string) string {
	var list []json.RawMessage
	if err := json.Unmarshal([]byte(s), &list); err != nil {
		t.Fatal(err)
	}
	sort.Slice(list, func(i, j int) bool { return string(list[i]) < string(list[j]) })
	out, _ := json.Marshal(list)
	return string(out)
}

func orEmpty(v []any) []any {
	if v == nil {
		return []any{}
	}
	return v
}

func snapshot(t *testing.T, dir string, scrub func(string) string) map[string]string {
	out := map[string]string{}
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		raw, _ := os.ReadFile(p)
		out[rel] = scrub(string(raw))
		return nil
	})
	return out
}
