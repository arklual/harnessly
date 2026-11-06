package apps

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

func hostRig(t *testing.T, enabled []string) (HostTools, *[]string) {
	t.Helper()
	data := t.TempDir()
	reg := &Registry{Root: filepath.Join(data, "apps")}
	for _, id := range []string{"acme.cash", "acme.other"} {
		pkg := filepath.Join(t.TempDir(), id)
		if err := Scaffold(pkg, id, id, "pane-chat"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := reg.Install(pkg, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
			t.Fatal(err)
		}
	}
	opened := &[]string{}
	return HostTools{
		Conv: "tab-1", Registry: reg, Storage: &Storage{Root: filepath.Join(data, "storage")}, Asks: NewAsks(),
		Enabled: func() []string { return enabled },
		Open:    func(target string) error { *opened = append(*opened, target); return nil },
	}, opened
}

func call(t *testing.T, h HostTools, name string, kv ...any) (string, bool) {
	t.Helper()
	res := h.Call(context.Background(), name, jsvalue.NewObject(kv...))
	content, _ := res.Get("content")
	text, _ := content.([]any)[0].(*jsvalue.Object).Get("text")
	isErr, _ := res.Get("isError")
	return text.(string), isErr == true
}

func TestHostToolsEnabledOnly(t *testing.T) {
	h, opened := hostRig(t, []string{"acme.cash"})
	if text, bad := call(t, h, "open"); bad || !strings.Contains(text, "acme.cash/main") || len(*opened) != 1 || (*opened)[0] != "acme.cash/main" {
		t.Fatalf("open: %q %v", text, *opened)
	}
	if text, bad := call(t, h, "open", "app", "acme.other"); !bad || !strings.Contains(text, "not on in this chat") {
		t.Fatalf("another app: %q", text)
	}
	if text, bad := call(t, h, "open", "view", "nope"); !bad || !strings.Contains(text, "no such view") {
		t.Fatalf("view: %q", text)
	}
	none, _ := hostRig(t, nil)
	if text, bad := call(t, none, "open"); !bad || !strings.Contains(text, "no app is on") {
		t.Fatalf("none: %q", text)
	}
	two, _ := hostRig(t, []string{"acme.cash", "acme.other"})
	if text, bad := call(t, two, "data_get", "key", "k"); !bad || !strings.Contains(text, "name one in `app`") {
		t.Fatalf("two apps: %q", text)
	}
}

func TestHostToolsData(t *testing.T) {
	h, _ := hostRig(t, []string{"acme.cash"})
	if text, bad := call(t, h, "data_get", "key", "picks"); bad || text != `{"value":null,"version":0}` {
		t.Fatalf("empty: %q", text)
	}
	value := jsvalue.NewObject("a", float64(1))
	if text, bad := call(t, h, "data_set", "key", "picks", "value", value); bad {
		t.Fatalf("set: %q", text)
	}
	if text, _ := call(t, h, "data_get", "key", "picks"); text != `{"value":{"a":1},"version":1}` {
		t.Fatalf("get: %q", text)
	}
	// What the agent wrote is the chat's area, the one the view reads.
	if raw, ok, _ := h.Storage.Get("acme.cash", ScopeChat, "tab-1", "picks"); !ok || string(raw) != `{"a":1}` {
		t.Fatalf("storage: %s %v", raw, ok)
	}
	big := strings.Repeat("x", 300<<10)
	if text, bad := call(t, h, "data_set", "key", "big", "value", big); !bad || !strings.Contains(text, "quota_exceeded") {
		t.Fatalf("quota: %.60q", text)
	}
	if text, bad := call(t, h, "data_get", "key", ""); !bad {
		t.Fatalf("empty key: %q", text)
	}
}

func TestHostToolsAsk(t *testing.T) {
	h, _ := hostRig(t, []string{"acme.cash"})
	type res struct {
		text string
		bad  bool
	}
	run := func(opts []any) chan res {
		out := make(chan res, 1)
		go func() {
			text, bad := call(t, h, "ask", "question", "Какую?", "options", opts)
			out <- res{text, bad}
		}()
		return out
	}
	wait := func() PendingAsk {
		for range 200 {
			if p := h.Asks.Pending("tab-1"); len(p) > 0 {
				return p[0]
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("no ask came")
		return PendingAsk{}
	}
	ans := run([]any{"АЗС", "Кафе"})
	p := wait()
	if p.App != "acme.cash" || p.AppName != "acme.cash" || p.Question != "Какую?" || len(p.Options) != 2 {
		t.Fatalf("pending: %+v", p)
	}
	bogus := "Такси"
	if err := h.Asks.Answer(p.ID, &bogus); err != ErrAskAnswer {
		t.Fatalf("bogus answer: %v", err)
	}
	pick := "Кафе"
	if err := h.Asks.Answer(p.ID, &pick); err != nil {
		t.Fatal(err)
	}
	if r := <-ans; r.bad || r.text != "Кафе" {
		t.Fatalf("answer: %+v", r)
	}
	if err := h.Asks.Answer(p.ID, &pick); err != ErrAskGone {
		t.Fatalf("twice: %v", err)
	}

	cancelled := run([]any{"a", "b"})
	p = wait()
	if err := h.Asks.Answer(p.ID, nil); err != nil {
		t.Fatal(err)
	}
	if r := <-cancelled; r.bad || r.text != "cancelled" {
		t.Fatalf("cancel: %+v", r)
	}

	h.Asks.TTL = 30 * time.Millisecond
	if r := <-run([]any{"a", "b"}); r.bad || r.text != "cancelled" {
		t.Fatalf("timeout: %+v", r)
	}
	if text, bad := call(t, h, "ask", "question", "q", "options", []any{"only"}); !bad {
		t.Fatalf("one option: %q", text)
	}
	if text, bad := call(t, h, "ask", "question", strings.Repeat("q", 301), "options", []any{"a", "b"}); !bad {
		t.Fatalf("long question: %.30q", text)
	}
}

func TestAsksPerChatCap(t *testing.T) {
	a := NewAsks()
	a.TTL = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for range MaxAsksPerChat {
		go func() { _, _, _ = a.Ask(ctx, "c", "acme.cash", "C", "q", []string{"a", "b"}) }()
	}
	for len(a.Pending("c")) < MaxAsksPerChat {
		time.Sleep(5 * time.Millisecond)
	}
	if _, _, err := a.Ask(ctx, "c", "acme.cash", "C", "q", []string{"a", "b"}); err != ErrAskBusy {
		t.Fatalf("cap: %v", err)
	}
	if len(a.Pending("other")) != 0 {
		t.Fatal("asks leak across chats")
	}
}

func TestLauncherHostMCP(t *testing.T) {
	data := t.TempDir()
	reg := &Registry{Root: filepath.Join(data, "apps")}
	pkg := filepath.Join(t.TempDir(), "review")
	if err := Scaffold(pkg, "acme.review", "Ревью", "chat"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.Install(pkg, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(t.TempDir(), "hooks.json")
	_ = os.WriteFile(base, []byte(`{}`), 0o600)
	mcp := filepath.Join(t.TempDir(), "preview-mcp.json")
	_ = os.WriteFile(mcp, []byte(`{"mcpServers":{"host-preview":{"type":"http","url":"http://127.0.0.1:1/api/cli/preview/mcp","headers":{"x-cli-token":"tok"}}}}`), 0o600)
	l := &Launcher{Registry: reg, MCPConfigPath: mcp}
	got, err := l.Claude([]string{"acme.review"}, base)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(got.MCPConfig)
	s := string(raw)
	if !strings.Contains(s, `"harnessly-app"`) || !strings.Contains(s, "preview/mcp?server=harnessly-app") || !strings.Contains(s, `"tok"`) || strings.Contains(s, "host-preview") {
		t.Fatalf("config: %s", s)
	}
	if st, _ := os.Stat(got.MCPConfig); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
}

// The agent's Document API tools: the same calls as a view's, for the document the editor shows beside the chat.
func TestHostToolsSinopia(t *testing.T) {
	data := t.TempDir()
	reg := &Registry{Root: filepath.Join(data, "apps")}
	pkg := t.TempDir()
	for file, body := range map[string]string{"harnessly/app.json": lintManifest, "ui/panel.html": "<p>x</p>"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(pkg, file)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pkg, file), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := reg.Install(pkg, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatal(err)
	}
	docs, state := wordyDeck(), NewSinopiaState()
	h := HostTools{
		Conv: "tab-1", Registry: reg, Storage: &Storage{Root: filepath.Join(data, "storage")}, Asks: NewAsks(),
		Enabled: func() []string { return nil },
		Sinopia: &SinopiaHost{Docs: docs, State: state, Tab: func(string) string { return "tab-x" }},
	}
	if text, bad := call(t, h, "sinopia_document"); !bad || !strings.Contains(text, "no Sinopia document is open") {
		t.Fatalf("no document: %q", text)
	}
	state.Open("tab-1", "p1", "slides")
	text, bad := call(t, h, "sinopia_document")
	if bad || !strings.Contains(text, `"kind":"slides"`) {
		t.Fatalf("document: %q", text)
	}
	// The validator is on in every slides document by itself: the chat need not turn it on.
	if text, bad := call(t, h, "sinopia_query", "selector", "li"); bad || !strings.Contains(text, `"count":3`) {
		t.Fatalf("query: %q", text)
	}
	if text, bad := call(t, h, "sinopia_patch", "ops", []any{jsvalue.NewObject("op", "setText", "node", pg+":1", "value", "New")}); bad || !strings.Contains(text, `"rev":9`) {
		t.Fatalf("patch: %q", text)
	}
	if op := docs.patched[0][0]; op.Op != "setText" || op.Value != "New" {
		t.Errorf("op: %+v", op)
	}
	if text, bad := call(t, h, "sinopia_mark", "nodes", []any{pg + ":2"}, "note", "mind this"); bad {
		t.Fatalf("mark: %q", text)
	}
	if m := state.Marks("p1"); len(m) != 1 || m[0].Source != "app:harnessly.deck-lint" {
		t.Errorf("marks: %+v", m)
	}
	// A site is not a slides document: the validator does not work there.
	docs.kind = "site"
	state.Open("tab-1", "p2", "site")
	if text, bad := call(t, h, "sinopia_document"); !bad || !strings.Contains(text, "no app works") {
		t.Fatalf("site: %q", text)
	}
	if text, bad := call(t, h, "sinopia_document", "app", "acme.other"); !bad {
		t.Fatalf("another app: %q", text)
	}
	if !h.Has("sinopia_set_fields") || h.Has("sinopia_nope") {
		t.Error("Has")
	}
}

func TestHostToolsWidgets(t *testing.T) {
	data := t.TempDir()
	reg := &Registry{Root: filepath.Join(data, "apps")}
	pkg := filepath.Join(t.TempDir(), "acme.meter")
	if err := Scaffold(pkg, "acme.meter", "Meter", "widget"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.Install(pkg, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatal(err)
	}
	changed := 0
	h := HostTools{Conv: "tab-1", Registry: reg, Storage: &Storage{Root: filepath.Join(data, "storage")}, Asks: NewAsks(),
		Widgets: &Widgets{Root: filepath.Join(data, "widgets")}, WidgetChanged: func(string) { changed++ },
		Enabled: func() []string { return []string{"acme.meter"} }}
	text, bad := call(t, h, "widget_show", "view", "card", "data", jsvalue.NewObject("title", "Готово", "value", 40), "id", "progress")
	if bad || text != "widget progress shown" {
		t.Fatalf("show: %q", text)
	}
	if text, bad := call(t, h, "widget_show", "view", "card", "data", jsvalue.NewObject("value", 80), "id", "progress"); bad || text != "widget progress changed" {
		t.Fatalf("change: %q", text)
	}
	got, err := h.Widgets.Get("tab-1", "progress")
	if err != nil || got.Rev != 2 || string(got.Data) != `{"value":80}` || got.App != "acme.meter" {
		t.Fatalf("kept: %v %+v", err, got)
	}
	if text, bad := call(t, h, "widget_show", "view", "nope"); !bad || !strings.Contains(text, "no view nope for the chat") {
		t.Fatalf("no such view: %q", text)
	}
	text, bad = call(t, h, "widget_render", "title", "График", "html", `<div class="chart" id="c"></div>`, "script", "echarts.init(c)")
	if bad || !strings.HasPrefix(text, "widget w") {
		t.Fatalf("render: %q", text)
	}
	if text, bad := call(t, h, "widget_render", "title", "x", "html", `<script>alert(1)</script>`); !bad || !strings.Contains(text, "not in the markup") {
		t.Fatalf("inline script: %q", text)
	}
	if changed != 3 {
		t.Fatalf("changed told %d times", changed)
	}
}

func TestHostToolsDataVersions(t *testing.T) {
	h, _ := hostRig(t, []string{"acme.cash"})
	var changes []Change
	h.Storage.OnChange(func(c Change) { changes = append(changes, c) })
	if text, bad := call(t, h, "data_set", "key", "k", "value", "one", "ifVersion", float64(0)); bad || text != `{"version":1}` {
		t.Fatalf("first: %q", text)
	}
	// A write that read version 0 comes late: refused, the value stays.
	text, bad := call(t, h, "data_set", "key", "k", "value", "two", "ifVersion", float64(0))
	if !bad || !strings.HasPrefix(text, "refused: conflict:") || !strings.Contains(text, "version 1") {
		t.Fatalf("stale: %q", text)
	}
	if text, _ := call(t, h, "data_get", "key", "k"); text != `{"value":"one","version":1}` {
		t.Fatalf("kept: %q", text)
	}
	if text, bad := call(t, h, "data_set", "key", "k", "value", "two", "ifVersion", float64(1)); bad || text != `{"version":2}` {
		t.Fatalf("current: %q", text)
	}
	if _, bad := call(t, h, "data_set", "key", "k", "value", "x", "ifVersion", float64(-1)); !bad {
		t.Fatal("a negative ifVersion is refused")
	}
	// Without ifVersion the write is as it was.
	if text, bad := call(t, h, "data_set", "key", "k", "value", "three"); bad || text != `{"version":3}` {
		t.Fatalf("plain: %q", text)
	}
	// The agent's write reaches whoever listens, and says no value.
	if len(changes) != 3 || changes[0] != (Change{App: "acme.cash", Scope: ScopeChat, Owner: "tab-1", Key: "k"}) {
		t.Fatalf("changes: %+v", changes)
	}
}
