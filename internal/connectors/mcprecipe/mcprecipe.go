// Package mcprecipe is the connector of a poll recipe over an MCP server (docs/connectors.md, «MCP
// recipe»): a service that gives no events — only tools — becomes a source anyway. A recipe is data, not code: which
// read-only tool of a connected MCP server to call, with which arguments (the cursor in them), where the list of
// things is in the answer and which fields of a thing are its id, time, author, title and link. An agent writes the
// recipe once, when the service is connected; from then on the runtime calls the tool by itself on a schedule, with no
// model in the loop, and every thing not seen before is an event — journaled by the hub, digested by the inbox, its
// trigger in the catalog like any other.
//
// The first poll only learns what is there (no flood of the past). A tool the server does not mark read-only is
// refused unless the person allowed it; one it marks destructive never runs.
package mcprecipe

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/connectors"
	"github.com/ly-solutions/harnessly/internal/mcpconn"
)

// ID names the connector in every event.
const ID = "mcp-recipe"

// EventItem is the one type of its events; the recipe's own name of what happened is the fact «trigger».
const EventItem = "feed.item"

// MCP is what the connector needs of the runtime's MCP connections (mcpconn.Service).
type MCP interface {
	List() ([]mcpconn.Server, error)
	ToolSpecs(ctx context.Context, id string) ([]mcpconn.ToolSpec, error)
	CallTool(ctx context.Context, id, tool string, args json.RawMessage) (json.RawMessage, error)
}

// Service is the runtime's MCP connections, set at start; nil: the connector cannot run.
var Service MCP

// Recipe is the poll of one kind of thing of an MCP server.
type Recipe struct {
	// Trigger is the recipe's name of what happened: «generation.done», «message.new». Goes to the event as a fact.
	Trigger string         `json:"trigger"`
	Tool    string         `json:"tool"`
	Args    map[string]any `json:"args,omitempty"` // "{{cursor}}" in a string value is the cursor ("" on the first poll)
	// Items is the dotted path to the list in the answer ("" — the answer is the list); the answer is the tool's
	// structuredContent, else its first text content read as JSON.
	Items string `json:"items,omitempty"`
	// The dotted paths of a thing's fields; ID is required.
	ID     string `json:"id"`
	At     string `json:"at,omitempty"`
	Actor  string `json:"actor,omitempty"`
	Title  string `json:"title,omitempty"`
	URL    string `json:"url,omitempty"`
	Thread string `json:"thread,omitempty"`
	// Cursor is the field whose greatest value is passed back as {{cursor}} ("" — none: things are told apart by id).
	Cursor string `json:"cursor,omitempty"`
	// Every is how often to poll (Go duration, 1m to 24h; 10m by default).
	Every string `json:"every,omitempty"`
}

var triggerRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*(\.[a-z0-9_-]+)*$`)

func (r Recipe) check() error {
	switch {
	case !triggerRe.MatchString(r.Trigger) || len(r.Trigger) > 48:
		return connectors.Sayf("The recipe's trigger: lower-case words with dots, like generation.done")
	case r.Tool == "":
		return connectors.Sayf("The recipe names no tool")
	case r.ID == "":
		return connectors.Sayf("The recipe names no id field of a thing")
	}
	if r.Every != "" {
		d, err := time.ParseDuration(r.Every)
		if err != nil || d < time.Minute || d > 24*time.Hour {
			return connectors.Sayf("The recipe's every: from 1m to 24h")
		}
	}
	return nil
}

func (r Recipe) every() time.Duration {
	if d, err := time.ParseDuration(r.Every); err == nil && d >= time.Minute && d <= 24*time.Hour {
		return d
	}
	return 10 * time.Minute
}

type conn struct{}

func init() { connectors.Register(conn{}) }

func (conn) Spec() connectors.Spec {
	return connectors.Spec{
		ID:    ID,
		Kind:  connectors.Feed,
		Inbox: "feed",
		Title: connectors.T("MCP recipe", "Рецепт MCP"),
		Summary: connectors.T("Updates of a service connected through MCP: a read-only tool polled by a recipe",
			"Обновления сервиса, подключённого через MCP: инструмент на чтение, опрашиваемый по рецепту"),
		Fields: []connectors.Field{
			{ID: "server", Type: connectors.FieldChoice, Discover: true, Label: connectors.T("MCP server", "MCP-сервер")},
			{ID: "recipe", Type: connectors.FieldText, Label: connectors.T("Recipe", "Рецепт"),
				Placeholder: `{"trigger":"generation.done","tool":"list_jobs","args":{"since":"{{cursor}}"},"items":"jobs","id":"id","at":"created_at","title":"prompt","cursor":"created_at"}`,
				Hint: connectors.T("JSON: which tool, where the list is, which fields are the id, time, title, link. An agent writes it for you",
					"JSON: какой инструмент, где список, какие поля — id, время, заголовок, ссылка. Его пишет агент")},
			{ID: "unmarked", Type: connectors.FieldChoice, Optional: true, Default: "no",
				Label: connectors.T("A tool not marked read-only", "Инструмент без пометки «только чтение»"),
				Hint: connectors.T("Only if you know the tool only reads: the server did not say so",
					"Только если вы знаете, что инструмент лишь читает: сервер этого не сказал"),
				Choices: []connectors.Choice{
					{Value: "no", Label: connectors.T("Refuse", "Отказать")},
					{Value: "yes", Label: connectors.T("Allow", "Разрешить")},
				}},
		},
		Events: []string{EventItem},
	}
}

func recipeOf(in connectors.Instance) (Recipe, error) {
	var r Recipe
	if err := json.Unmarshal([]byte(in.Config["recipe"]), &r); err != nil {
		return r, connectors.Sayf("The recipe is not JSON: %s", strings.SplitN(err.Error(), ":", 2)[0])
	}
	return r, r.check()
}

// server finds the MCP connection by its id or slug.
func server(ref string) (mcpconn.Server, error) {
	if Service == nil {
		return mcpconn.Server{}, connectors.Sayf("MCP connections are not available on this machine")
	}
	list, err := Service.List()
	if err != nil {
		return mcpconn.Server{}, err
	}
	for _, s := range list {
		if s.ID == ref || s.Slug == ref {
			return s, nil
		}
	}
	return mcpconn.Server{}, connectors.Sayf("No MCP server %q is connected", ref)
}

// Discover lists the connected MCP servers for the field «server».
func (conn) Discover(_ context.Context, _ connectors.Instance, field string) ([]connectors.Choice, error) {
	if field != "server" || Service == nil {
		return nil, nil
	}
	list, err := Service.List()
	if err != nil {
		return nil, err
	}
	var out []connectors.Choice
	for _, s := range list {
		out = append(out, connectors.Choice{Value: s.Slug, Label: connectors.T(s.Name, s.Name)})
	}
	return out, nil
}

// guard refuses a tool that may change something: destructive always, not marked read-only unless allowed.
func guard(ctx context.Context, in connectors.Instance, srv mcpconn.Server, tool string) error {
	specs, err := Service.ToolSpecs(ctx, srv.ID)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(specs, func(t mcpconn.ToolSpec) bool { return t.Name == tool })
	if i < 0 {
		return connectors.Sayf("%s has no tool %q", srv.Name, tool)
	}
	a := specs[i].Annotations
	if a != nil && a.DestructiveHint != nil && *a.DestructiveHint && (a.ReadOnlyHint == nil || !*a.ReadOnlyHint) {
		return connectors.Sayf("%s marks %q as destructive: a recipe never calls it", srv.Name, tool)
	}
	if (a == nil || a.ReadOnlyHint == nil || !*a.ReadOnlyHint) && in.Config["unmarked"] != "yes" {
		return connectors.Sayf("%s does not mark %q as read-only: allow it in the form only if you know it only reads", srv.Name, tool)
	}
	return nil
}

// thing is one item of the answer, mapped by the recipe.
type thing struct {
	id, actor, title, url, thread, cursor string
	at                                    time.Time
}

// run calls the tool once and maps the answer.
func run(ctx context.Context, in connectors.Instance, r Recipe, cursor string) ([]thing, error) {
	srv, err := server(in.Config["server"])
	if err != nil {
		return nil, err
	}
	if err := guard(ctx, in, srv, r.Tool); err != nil {
		return nil, err
	}
	args, err := json.Marshal(withCursor(r.Args, cursor))
	if err != nil {
		return nil, err
	}
	raw, err := Service.CallTool(ctx, srv.ID, r.Tool, args)
	if err != nil {
		return nil, connectors.Sayf("%s did not answer the tool %q", srv.Name, r.Tool)
	}
	body, err := answer(raw)
	if err != nil {
		return nil, err
	}
	got := pick(body, r.Items)
	if got == nil {
		return nil, nil // an empty answer: nothing there now
	}
	list, ok := got.([]any)
	if !ok {
		return nil, connectors.Sayf("The answer has no list at %q", r.Items)
	}
	var out []thing
	for _, v := range list {
		t := thing{id: str(pick(v, r.ID)), actor: str(pick(v, r.Actor)), title: str(pick(v, r.Title)),
			url: str(pick(v, r.URL)), thread: str(pick(v, r.Thread))}
		if t.id == "" {
			continue
		}
		if r.At != "" {
			t.at = when(pick(v, r.At))
		}
		if r.Cursor != "" {
			t.cursor = str(pick(v, r.Cursor))
		}
		out = append(out, t)
	}
	return out, nil
}

// answer is the tool's result as data: its structuredContent, else its first text read as JSON.
func answer(raw json.RawMessage) (any, error) {
	var res struct {
		IsError    bool            `json:"isError"`
		Structured json.RawMessage `json:"structuredContent"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, connectors.Sayf("The tool's answer is not an MCP result")
	}
	if res.IsError {
		return nil, connectors.Sayf("The tool answered with an error")
	}
	var v any
	if len(res.Structured) > 0 && string(res.Structured) != "null" {
		if err := json.Unmarshal(res.Structured, &v); err == nil {
			return v, nil
		}
	}
	for _, c := range res.Content {
		if c.Type == "text" {
			if err := json.Unmarshal([]byte(c.Text), &v); err == nil {
				return v, nil
			}
			return nil, connectors.Sayf("The tool's text is not JSON: a recipe reads JSON only")
		}
	}
	return nil, connectors.Sayf("The tool's answer is empty")
}

// pick follows a dotted path ("data.items", "items.0.id"); "" is the value itself.
func pick(v any, path string) any {
	if path == "" {
		return v
	}
	for _, k := range strings.Split(path, ".") {
		switch x := v.(type) {
		case map[string]any:
			v = x[k]
		case []any:
			i, err := strconv.Atoi(k)
			if err != nil || i < 0 || i >= len(x) {
				return nil
			}
			v = x[i]
		default:
			return nil
		}
	}
	return v
}

func str(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// when reads a time: RFC 3339, or Unix seconds or milliseconds.
func when(v any) time.Time {
	switch x := v.(type) {
	case string:
		if t, err := time.Parse(time.RFC3339, x); err == nil {
			return t
		}
		if n, err := strconv.ParseFloat(x, 64); err == nil {
			return when(n)
		}
	case float64:
		if x > 1e12 {
			return time.UnixMilli(int64(x))
		}
		if x > 1e8 {
			return time.Unix(int64(x), 0)
		}
	}
	return time.Time{}
}

// withCursor puts the cursor where a string argument says {{cursor}}.
func withCursor(args map[string]any, cursor string) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		if s, ok := v.(string); ok {
			out[k] = strings.ReplaceAll(s, "{{cursor}}", cursor)
		} else {
			out[k] = v
		}
	}
	return out
}

// Check is the dry run of the recipe when the person connects it: the tool is called, the answer mapped, and the form
// shows how many things it found and the first one — what the events will look like.
func (conn) Check(ctx context.Context, in connectors.Instance) (connectors.Account, error) {
	r, err := recipeOf(in)
	if err != nil {
		return connectors.Account{}, err
	}
	things, err := run(ctx, in, r, "")
	if err != nil {
		return connectors.Account{}, err
	}
	label := fmt.Sprintf("%s · %s: %d now", in.Config["server"], r.Trigger, len(things))
	if len(things) > 0 && things[0].title != "" {
		label += " · «" + clip(things[0].title, 60) + "»"
	}
	return connectors.Account{Label: label}, nil
}

// Interval is the recipe's every.
func (conn) Interval(in connectors.Instance) time.Duration {
	r, _ := recipeOf(in)
	return r.every()
}

// memo is what the connection keeps between polls.
type memo struct {
	Cursor string   `json:"cursor"`
	Seen   []string `json:"seen"` // the newest ids, to tell new things from old without a cursor
	Primed bool     `json:"primed"`
}

const seenMax = 1000

// Poll calls the tool and gives the things not seen before as events. The first poll gives none: it learns what is
// there already, so connecting a service does not pour its past into the digest.
func (conn) Poll(ctx context.Context, in connectors.Instance, from connectors.Cursor) ([]connectors.Event, connectors.Cursor, error) {
	r, err := recipeOf(in)
	if err != nil {
		return nil, from, err
	}
	var m memo
	if in.Load != nil {
		_ = in.Load(&m)
	}
	things, err := run(ctx, in, r, m.Cursor)
	if err != nil {
		return nil, from, err
	}
	seen := map[string]bool{}
	for _, id := range m.Seen {
		seen[id] = true
	}
	var events []connectors.Event
	for _, t := range things {
		if seen[t.id] {
			continue
		}
		seen[t.id] = true
		m.Seen = append(m.Seen, t.id)
		if t.cursor != "" && later(t.cursor, m.Cursor) {
			m.Cursor = t.cursor
		}
		if !m.Primed {
			continue
		}
		events = append(events, connectors.Event{Type: EventItem, ID: r.Trigger + ":" + t.id, At: t.at, Actor: t.actor,
			Title: t.title, URL: t.url, Thread: t.thread, Facts: map[string]string{"trigger": r.Trigger, "server": in.Config["server"]}})
	}
	m.Primed = true
	if len(m.Seen) > seenMax {
		m.Seen = m.Seen[len(m.Seen)-seenMax:]
	}
	if in.Store != nil {
		if err := in.Store(m); err != nil {
			return nil, from, err
		}
	}
	return events, from, nil
}

// later says a is after b: numbers as numbers, the rest (RFC 3339 times, ids) as text.
func later(a, b string) bool {
	if b == "" {
		return true
	}
	x, e1 := strconv.ParseFloat(a, 64)
	y, e2 := strconv.ParseFloat(b, 64)
	if e1 == nil && e2 == nil {
		return x > y
	}
	return a > b
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
