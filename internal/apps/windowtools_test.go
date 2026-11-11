package apps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// windowRig is a chat with acme.form installed (tools.page, context.publish) and whatever of it is on.
func windowRig(t *testing.T, enabled *[]string) (HostTools, *PageTools) {
	t.Helper()
	data := t.TempDir()
	reg := &Registry{Root: filepath.Join(data, "apps")}
	pkg := filepath.Join(t.TempDir(), "acme.form")
	if err := Scaffold(pkg, "acme.form", "Форма", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(pkg, "harnessly", "app.json")
	raw, _ := os.ReadFile(manifest)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["permissions"] = append(m["permissions"].([]any), "tools.page", "context.publish")
	raw, _ = json.Marshal(m)
	if err := os.WriteFile(manifest, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.Install(pkg, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatal(err)
	}
	pages := NewPageTools()
	return HostTools{Conv: "tab-1", Registry: reg, Pages: pages, Enabled: func() []string { return *enabled }}, pages
}

func windowToolDef(h HostTools, name string) *jsvalue.Object {
	for _, def := range h.Tools() {
		o := def.(*jsvalue.Object)
		if n, _ := o.Get("name"); n == name {
			return o
		}
	}
	return nil
}

// A window's tool is named <app with '.' as '_'>__<tool>; a name that does not fit, or that two tools would share, is
// shortened the same way at every listing, whatever the order.
func TestWindowToolNames(t *testing.T) {
	valid := regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	long := strings.Repeat("x", 60)
	reports := []PageReport{
		{App: "acme.form", Tools: []PageTool{{Name: "fill"}, {Name: "a.b"}, {Name: "a_b"}, {Name: long}}},
		{App: "acme.other", Tools: []PageTool{{Name: "fill"}}},
	}
	got := map[string]string{}
	for _, w := range WindowTools(reports) {
		if !valid.MatchString(HostToolName(w.Name)) {
			t.Fatalf("%s does not fit a tool name", HostToolName(w.Name))
		}
		if _, twice := got[w.Name]; twice {
			t.Fatalf("%s is two tools", w.Name)
		}
		got[w.Name] = w.App + "/" + w.Tool()
	}
	if got["acme_form__fill"] != "acme.form/fill" || got["acme_other__fill"] != "acme.other/fill" {
		t.Fatalf("plain names: %v", got)
	}
	if _, plain := got["acme_form__a_b"]; plain || len(got) != 5 {
		t.Fatalf("two tools of one long name are both shortened: %v", got)
	}
	back := []PageReport{reports[1], {App: "acme.form", Tools: []PageTool{{Name: long}, {Name: "a_b"}, {Name: "a.b"}, {Name: "fill"}}}}
	for _, w := range WindowTools(back) {
		if got[w.Name] != w.App+"/"+w.Tool() {
			t.Fatalf("%s names another tool in another order", w.Name)
		}
	}
}

// The tool is in tools/list with the window's description and schema, and the offering the stream watches changes only
// when the set of tools does: not when the page renews its report.
func TestWindowToolsAreListedAndOfferingFollowsTheSet(t *testing.T) {
	enabled := []string{"acme.form"}
	h, pages := windowRig(t, &enabled)
	now := time.Unix(1000, 0)
	pages.Now = func() time.Time { return now }
	if windowToolDef(h, "acme_form__fill") != nil {
		t.Fatal("no window, no tool")
	}
	empty := h.Offering()
	fill := PageTool{Name: "fill", Description: "Fill the form", InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`), Confirm: true}
	report := func(tools ...PageTool) {
		t.Helper()
		if err := pages.Report("tab-1", []PageReport{{App: "acme.form", Tools: tools}}); err != nil {
			t.Fatal(err)
		}
	}
	report(fill)
	def := windowToolDef(h, "acme_form__fill")
	if def == nil {
		t.Fatalf("tools: %s", jsvalue.Stringify(h.Tools(), ""))
	}
	text := string(jsvalue.Stringify(def, ""))
	for _, want := range []string{"Fill the form.", "Форма", "asked before it runs", `"name":{"type":"string"}`, `"anthropic/alwaysLoad":true`} {
		if !strings.Contains(text, want) {
			t.Fatalf("the definition lacks %q: %s", want, text)
		}
	}
	if !h.Has("acme_form__fill") || !h.Has("page_call") {
		t.Fatal("the server has the window's tool and still page_call")
	}
	first := h.Offering()
	if first == empty {
		t.Fatal("a registered tool changes the offering")
	}
	now = now.Add(5 * time.Second)
	report(fill)
	if h.Offering() != first {
		t.Fatal("a renewed report of the same tools is no change")
	}
	report(fill, PageTool{Name: "save"})
	second := h.Offering()
	if second == first {
		t.Fatal("another tool is a change")
	}
	report(fill)
	if h.Offering() != first {
		t.Fatal("a tool taken away is a change back")
	}
	now = now.Add(PageToolsTTL + time.Second)
	if h.Offering() != empty || windowToolDef(h, "acme_form__fill") != nil {
		t.Fatal("a closed window's tools go when its report runs out")
	}
	// An app that is off in the chat offers nothing, whatever its page reports.
	report(fill)
	enabled = nil
	if len(h.WindowTools()) != 0 {
		t.Fatal("an app that is off has no window tools")
	}
}

func answerNext(t *testing.T, pages *PageTools, result string) PageCall {
	t.Helper()
	var calls []PageCall
	for i := 0; i < 400 && len(calls) == 0; i++ {
		calls = pages.Take("tab-1")
		time.Sleep(5 * time.Millisecond)
	}
	if len(calls) != 1 {
		t.Fatalf("calls: %+v", calls)
	}
	if err := pages.Answer("tab-1", calls[0].ID, PageAnswer{Result: json.RawMessage(result)}); err != nil {
		t.Fatal(err)
	}
	return calls[0]
}

// A call by the tool's own name goes the way page_call goes, and one that comes before the window registered the tool
// waits for it; past the wait the answer says what is missing.
func TestWindowToolCallWaitsForTheWindow(t *testing.T) {
	enabled := []string{"acme.form"}
	h, pages := windowRig(t, &enabled)
	pages.ToolWait = 2 * time.Second
	done := make(chan string, 1)
	go func() {
		text, bad := call(t, h, "acme_form__fill", "name", "Ann")
		if bad {
			t.Errorf("refused: %s", text)
		}
		done <- text
	}()
	// The window finishes loading after the call came.
	time.Sleep(150 * time.Millisecond)
	if err := pages.Report("tab-1", []PageReport{{App: "acme.form", Tools: []PageTool{{Name: "fill"}}}}); err != nil {
		t.Fatal(err)
	}
	got := answerNext(t, pages, `{"ok":true}`)
	if got.App != "acme.form" || got.Tool != "fill" || string(got.Arguments) != `{"name":"Ann"}` {
		t.Fatalf("the view's call: %+v", got)
	}
	if text := <-done; text != `{"ok":true}` {
		t.Fatalf("answer: %q", text)
	}

	pages.ToolWait = 250 * time.Millisecond
	started := time.Now()
	text, bad := call(t, h, "acme_form__save")
	if !bad || !strings.Contains(text, "the window of acme.form is open, but the tool save is not registered; it offers: acme_form__fill") {
		t.Fatalf("a tool the window has not: %q", text)
	}
	if time.Since(started) < pages.ToolWait {
		t.Fatal("the call waits for the tool before it refuses")
	}
	other, otherPages := windowRig(t, &enabled)
	otherPages.ToolWait = 50 * time.Millisecond
	if text, bad := call(t, other, "acme_form__fill"); !bad || !strings.Contains(text, "the window of acme.form is not open: open it with the `open` tool") {
		t.Fatalf("no window: %q", text)
	}
	if text, bad := call(t, other, "nobody_here__x"); !bad || !strings.Contains(text, "no open window offers nobody_here__x") {
		t.Fatalf("no such app: %q", text)
	}
	// An app that waits for the agent's next start refuses with the reason, as its other tools do.
	h.Live = func(string) string { return "waits for a new start" }
	if text, bad := call(t, h, "acme_form__fill"); !bad || !strings.Contains(text, "waits for a new start") {
		t.Fatalf("an app that is not live: %q", text)
	}
}

// The block of an open window: the screen, its hint, the states' values and the tools by their full names, within the
// limit however much the window published.
func TestWindowBrief(t *testing.T) {
	enabled := []string{"acme.form"}
	h, pages := windowRig(t, &enabled)
	ctx := `{"screen":{"title":"Новое мероприятие","route":"/events/new","prompt":"Сначала уточни дату и площадку."},
"states":{"form":{"description":"Поля формы","schema":{"type":"object"},"value":{"title":"Концерт","price":500}},"step":{"value":2}}}`
	if err := pages.Report("tab-1", []PageReport{{App: "acme.form", Context: json.RawMessage(ctx),
		Tools: []PageTool{{Name: "fill", Description: "Fill the form"}, {Name: "save", Description: "Save it", Confirm: true}}}}); err != nil {
		t.Fatal(err)
	}
	briefs := h.WindowBriefs()
	if len(briefs) != 1 || briefs[0].App != "acme.form" {
		t.Fatalf("briefs: %+v", briefs)
	}
	text := briefs[0].Text
	for _, want := range []string{
		"## Open window: Форма (acme.form)", "Screen: Новое мероприятие (/events/new)", "On this screen: Сначала уточни дату и площадку.",
		`- form = {"title":"Концерт","price":500} (Поля формы)`, "- step = 2",
		"- mcp__harnessly-app__acme_form__fill(): Fill the form", "- mcp__harnessly-app__acme_form__save(): Save it (the person is asked first)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the block lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"schema"`) {
		t.Fatalf("the schema of a state is not printed:\n%s", text)
	}

	// Any other context is printed as it is, cut; the tools' names are kept whatever the context's size.
	big := `{"rows":"` + strings.Repeat("я", 4000) + `"}`
	other := WindowBrief("Форма", PageReport{App: "acme.form", Context: json.RawMessage(big)}, WindowTools([]PageReport{{App: "acme.form", Tools: []PageTool{{Name: "fill"}}}}))
	if len(other) > MaxWindowBrief || !strings.Contains(other, `Published by the window: {"rows":"яяя`) || !strings.Contains(other, "mcp__harnessly-app__acme_form__fill") {
		t.Fatalf("another context (%d bytes):\n%s", len(other), other)
	}
	states := map[string]any{}
	for i := range 40 {
		states[strings.Repeat("s", i+1)] = map[string]any{"value": strings.Repeat("v", 300)}
	}
	raw, _ := json.Marshal(map[string]any{"states": states})
	many := WindowBrief("Форма", PageReport{App: "acme.form", Context: raw}, nil)
	if len(many) > MaxWindowBrief || !strings.Contains(many, "more states") {
		t.Fatalf("many states (%d bytes):\n%s", len(many), many)
	}
	if got := WindowScreen(json.RawMessage(ctx)); got != "Новое мероприятие" {
		t.Fatalf("screen: %q", got)
	}
	if notice := WindowNotice([]string{text}, []string{"Старое"}); !strings.Contains(notice, "# Open app windows") || !strings.Contains(notice, "Старое: the window is closed") {
		t.Fatalf("notice: %q", notice)
	}
	if WindowNotice(nil, nil) != "" {
		t.Fatal("nothing changed, nothing said")
	}
}

// The first opening of an app's window in a chat is claimed once and stays claimed after a restart.
func TestWindowOpensClaimOnce(t *testing.T) {
	dir := t.TempDir()
	o := &WindowOpens{path: filepath.Join(dir, "apps", "window-opens.json")}
	if !o.Claim("c1", "acme.form") || o.Claim("c1", "acme.form") {
		t.Fatal("the first claim wins, the second does not")
	}
	if !o.Claim("c2", "acme.form") {
		t.Fatal("another chat is another first time")
	}
	again := &WindowOpens{path: o.path}
	if again.Claim("c1", "acme.form") || !again.Claim("c1", "acme.other") {
		t.Fatal("a restart keeps what was claimed")
	}
	event := OpenEvent("acme.form", `Фор<ма>`, "Новое мероприятие")
	if !strings.HasPrefix(event, `<app-event app="acme.form" event="opened">`) || !strings.Contains(event, "the app Форма in the right pane") ||
		!strings.Contains(event, `on the screen "Новое мероприятие"`) || !strings.Contains(event, "not the person's words") {
		t.Fatalf("event: %q", event)
	}
}
