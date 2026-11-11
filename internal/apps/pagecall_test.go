package apps

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// formState is a window's state tool of n fields, as the SDK's hl.state registers it: <state>_set takes a patch.
func formState(n int) (PageTool, []string, json.RawMessage) {
	props, values, names := map[string]any{}, map[string]any{}, make([]string, 0, n)
	for i := 1; i <= n; i++ {
		name := fmt.Sprintf("field_%02d", i)
		names = append(names, name)
		props[name] = map[string]any{"type": "string", "description": "A field of the form, described at some length."}
		values[name] = ""
	}
	props["amount"] = map[string]any{"type": "number"}
	schema, _ := json.Marshal(map[string]any{"type": "object", "properties": props, "additionalProperties": false})
	ctx, _ := json.Marshal(map[string]any{
		"screen": map[string]any{"title": "Заявка", "route": "/new"},
		"states": map[string]any{"form": map[string]any{"description": "The request's form", "value": values}},
	})
	return PageTool{Name: "form_set", Description: "Change «form»: the request's form. Pass only the fields to change (a patch).", InputSchema: schema}, names, ctx
}

func toolNames(h HostTools) []string {
	var out []string
	for _, def := range h.Tools() {
		n, _ := def.(*jsvalue.Object).Get("name")
		out = append(out, n.(string))
	}
	return out
}

// page_call is in tools/list from the agent's start, always loaded: in a chat with no app on it is the one tool.
func TestPageCallIsAlwaysThere(t *testing.T) {
	var enabled []string
	h, _ := windowRig(t, &enabled)
	if got := toolNames(h); len(got) != 1 || got[0] != "page_call" {
		t.Fatalf("tools of a chat with no app on: %v", got)
	}
	enabled = []string{"acme.form"}
	def := windowToolDef(h, "page_call")
	if def == nil || !strings.Contains(string(jsvalue.Stringify(def, "")), `"anthropic/alwaysLoad":true`) {
		t.Fatalf("page_call of a chat with an app on: %v", def)
	}
}

// The acceptance of HRN-1113. The agent started before the app was on and never heard tools/list_changed: of the
// server's tools it has page_call alone. The window with a form of twenty fields opens; the block of the agent's turn
// names the tool and its fields, and one call of page_call, by what the block says, fills the form.
func TestFormFilledWithoutListChanged(t *testing.T) {
	var enabled []string
	h, pages := windowRig(t, &enabled)
	known := toolNames(h) // what the agent listed at its start; no list_changed reaches it afterwards
	enabled = []string{"acme.form"}
	tool, fields, ctx := formState(20)
	if err := pages.Report("tab-1", []PageReport{{App: "acme.form", Tools: []PageTool{tool, {Name: "submit", Description: "Send the request", Confirm: true}}, Context: ctx}}); err != nil {
		t.Fatal(err)
	}
	if len(known) != 1 || known[0] != "page_call" {
		t.Fatalf("the agent's tools: %v", known)
	}
	briefs := h.WindowBriefs()
	if len(briefs) != 1 {
		t.Fatalf("blocks: %+v", briefs)
	}
	block := briefs[0].Text
	if len(block) > MaxWindowBrief {
		t.Fatalf("the block takes %d bytes", len(block))
	}
	own := "mcp__harnessly-app__acme_form__form_set"
	for _, want := range append([]string{"mcp__harnessly-app__page_call", `"app": "acme.form"`, own + "(amount, field_01, ", "mcp__harnessly-app__acme_form__submit()", "Send the request", "Screen: Заявка"}, fields...) {
		if !strings.Contains(block, want) {
			t.Fatalf("the block lacks %q:\n%s", want, block)
		}
	}
	// The one call, with the name the block gives.
	done := make(chan string, 1)
	go func() {
		text, bad := call(t, h, "page_call", "app", "acme.form", "tool", own,
			"arguments", jsvalue.NewObject("field_01", "Анна", "field_20", "Москва", "amount", 1500))
		if bad {
			t.Errorf("refused: %s", text)
		}
		done <- text
	}()
	got := answerNext(t, pages, `{"ok":true,"issues":[]}`)
	if got.App != "acme.form" || got.Tool != "form_set" || string(got.Arguments) != `{"field_01":"Анна","field_20":"Москва","amount":1500}` {
		t.Fatalf("the window's call: %+v %s", got, got.Arguments)
	}
	if text := <-done; text != `{"ok":true,"issues":[]}` {
		t.Fatalf("answer: %q", text)
	}
	// The name without the app before it, and the app left out where one is on, do the same.
	go func() {
		text, _ := call(t, h, "page_call", "tool", "form_set", "arguments", jsvalue.NewObject("field_02", "x"))
		done <- text
	}()
	if got := answerNext(t, pages, `{"ok":true}`); got.Tool != "form_set" {
		t.Fatalf("the window's call: %+v", got)
	}
	<-done
}

// A block that cannot hold every description keeps the names and the arguments; the limit holds.
func TestWindowBriefKeepsArgumentsUnderTheLimit(t *testing.T) {
	enabled := []string{"acme.form"}
	h, pages := windowRig(t, &enabled)
	form, fields, ctx := formState(20)
	tools := []PageTool{form}
	for i := 0; i < 12; i++ {
		tools = append(tools, PageTool{Name: fmt.Sprintf("action_%02d", i), Description: strings.Repeat("words ", 30),
			InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"note":{"type":"string"}},"required":["id"]}`)})
	}
	if err := pages.Report("tab-1", []PageReport{{App: "acme.form", Tools: tools, Context: ctx}}); err != nil {
		t.Fatal(err)
	}
	block := h.WindowBriefs()[0].Text
	if len(block) > MaxWindowBrief {
		t.Fatalf("the block takes %d bytes", len(block))
	}
	for _, want := range append([]string{"acme_form__action_11(id*, note)", "page_call"}, fields...) {
		if !strings.Contains(block, want) {
			t.Fatalf("the block lacks %q:\n%s", want, block)
		}
	}
	if strings.Contains(block, "words words") {
		t.Fatalf("descriptions were to go first:\n%s", block)
	}
}

// page_call waits for a window that is still loading, as a tool called by its own name does, and says what the window
// offers when it has no such tool.
func TestPageCallWaitsForTheWindow(t *testing.T) {
	enabled := []string{"acme.form"}
	h, pages := windowRig(t, &enabled)
	pages.ToolWait = 2 * time.Second
	done := make(chan string, 1)
	go func() {
		text, bad := call(t, h, "page_call", "tool", "fill", "arguments", jsvalue.NewObject("name", "Ann"))
		if bad {
			t.Errorf("refused: %s", text)
		}
		done <- text
	}()
	time.Sleep(150 * time.Millisecond)
	fill := PageTool{Name: "fill", InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)}
	if err := pages.Report("tab-1", []PageReport{{App: "acme.form", Tools: []PageTool{fill}}}); err != nil {
		t.Fatal(err)
	}
	if got := answerNext(t, pages, `"done"`); got.Tool != "fill" || string(got.Arguments) != `{"name":"Ann"}` {
		t.Fatalf("the window's call: %+v", got)
	}
	if text := <-done; text != `"done"` {
		t.Fatalf("answer: %q", text)
	}
	pages.ToolWait = 200 * time.Millisecond
	started := time.Now()
	text, bad := call(t, h, "page_call", "tool", "save")
	if !bad || !strings.Contains(text, "offers no tool save; it offers: fill(name*)") || time.Since(started) < pages.ToolWait {
		t.Fatalf("a tool the window has not: %q", text)
	}
	// With no tool named: what the window offers.
	if text, bad := call(t, h, "page_call", "app", "acme.form"); bad || !strings.Contains(text, `"name":"fill"`) || !strings.Contains(text, `"inputSchema"`) {
		t.Fatalf("no tool named: %q", text)
	}
}

// Arguments that do not follow the tool's schema do not reach the window: the answer names what is wrong and carries
// the schema, by either way of calling the tool. A window's own error carries the schema too.
func TestWrongArgumentsComeBackWithTheSchema(t *testing.T) {
	enabled := []string{"acme.form"}
	h, pages := windowRig(t, &enabled)
	schema := `{"type":"object","properties":{"amount":{"type":"number"},"kind":{"type":"string","enum":["in","out"]},"note":{"type":"string"}},"required":["amount","kind"],"additionalProperties":false}`
	if err := pages.Report("tab-1", []PageReport{{App: "acme.form", Tools: []PageTool{{Name: "pay", InputSchema: json.RawMessage(schema)}}}}); err != nil {
		t.Fatal(err)
	}
	for _, by := range []func() (string, bool){
		func() (string, bool) {
			return call(t, h, "page_call", "tool", "pay", "arguments", jsvalue.NewObject("amount", "many", "kind", "both", "sum", 1))
		},
		func() (string, bool) { return call(t, h, "acme_form__pay", "amount", "many", "kind", "both", "sum", 1) },
	} {
		text, bad := by()
		for _, want := range []string{"refused: the arguments do not fit pay", "amount is string, the schema asks for number", "kind is not one of the values", "sum is not an argument", schema} {
			if !bad || !strings.Contains(text, want) {
				t.Fatalf("the refusal lacks %q: %q", want, text)
			}
		}
	}
	if text, bad := call(t, h, "page_call", "tool", "pay", "arguments", jsvalue.NewObject("note", "x")); !bad || !strings.Contains(text, "amount is required; kind is required") {
		t.Fatalf("required: %q", text)
	}
	if calls := pages.Take("tab-1"); len(calls) != 0 {
		t.Fatalf("refused calls reached the window: %+v", calls)
	}
	// The window's own refusal.
	done := make(chan string, 1)
	go func() {
		text, _ := call(t, h, "page_call", "tool", "pay", "arguments", jsvalue.NewObject("amount", 5, "kind", "in"))
		done <- text
	}()
	var calls []PageCall
	for i := 0; i < 400 && len(calls) == 0; i++ {
		calls = pages.Take("tab-1")
		time.Sleep(5 * time.Millisecond)
	}
	if len(calls) != 1 {
		t.Fatalf("calls: %+v", calls)
	}
	if err := pages.Answer("tab-1", calls[0].ID, PageAnswer{Error: "amount must be at least 10"}); err != nil {
		t.Fatal(err)
	}
	if text := <-done; !strings.Contains(text, "amount must be at least 10") || !strings.Contains(text, schema) {
		t.Fatalf("the window's error: %q", text)
	}
}

func TestArgIssues(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"},"x":{"type":["string","null"]},"list":{"type":"array"},"any":{}},"required":["n"]}`)
	for args, want := range map[string]string{
		`{"n":1}`:                             "",
		`{"n":1,"x":null,"list":[],"any":{}}`: "",
		`{"n":1,"extra":true}`:                "", // the schema does not close its names
		`{"n":1.5}`:                           "n is number, the schema asks for integer",
		`{"n":1,"x":2}`:                       "x is integer, the schema asks for string null",
		`{}`:                                  "n is required",
		`[1]`:                                 "the arguments are one JSON object",
	} {
		if got := strings.Join(argIssues(schema, json.RawMessage(args)), "; "); got != want {
			t.Errorf("%s: %q, want %q", args, got, want)
		}
	}
	if got := argIssues(nil, json.RawMessage(`{"a":1}`)); got != nil {
		t.Errorf("a tool with no schema takes anything: %v", got)
	}
}

// With no window of the app connected, page_call says so in words and what to do, with a tool named or not: an empty
// list would be a refusal that says nothing.
func TestPageCallSaysTheWindowIsNotConnected(t *testing.T) {
	enabled := []string{"acme.form"}
	h, pages := windowRig(t, &enabled)
	pages.ToolWait = 50 * time.Millisecond
	for _, kv := range [][]any{{"app", "acme.form"}, {}, {"app", "acme.form", "tool", "fill"}} {
		text, bad := call(t, h, "page_call", kv...)
		for _, want := range []string{"the window of acme.form is not connected", "not open in the right pane", "not open in a browser", "`open` tool"} {
			if !bad || !strings.Contains(text, want) {
				t.Fatalf("%v: the answer lacks %q: %q", kv, want, text)
			}
		}
	}
	// A window that is connected and offers nothing yet is another matter: the list, not the refusal.
	if err := pages.Report("tab-1", []PageReport{{App: "acme.form", Context: json.RawMessage(`{"screen":{"title":"Пусто"}}`)}}); err != nil {
		t.Fatal(err)
	}
	if text, bad := call(t, h, "page_call", "app", "acme.form"); bad || !strings.Contains(text, `"app":"acme.form"`) {
		t.Fatalf("a connected window: %q", text)
	}
}
