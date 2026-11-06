package apps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
)

// HostServer is the name of the MCP server of the host's tools for the agent of a chat with apps. Claude names its tools
// mcp__harnessly-app__<tool> with '.' written '_', and the policy reads them as app.open, app.data.get, app.data.set and
// app.ask (policy.go).
const HostServer = hostServer

// HostTools is the agent-side host tools of one chat: it answers for the apps on in that chat and nothing else.
type HostTools struct {
	Conv     string
	Registry *Registry
	Storage  *Storage
	Asks     *Asks
	// Enabled are the apps on in the chat now.
	Enabled func() []string
	// Open asks the chat's pane to open an app's tab (target is <id>[/<view>]).
	Open func(target string) error
	// Sinopia serves the Document API to the agent; nil where the machine has no library.
	Sinopia *SinopiaHost
	// Widgets keep what the agent shows in the chat's feed; nil refuses the widget tools.
	Widgets *Widgets
	// WidgetChanged tells the chat's pages that a widget of the chat was shown or changed; may be nil.
	WidgetChanged func(conv string)
}

var _ mcpserver.Toolset = HostTools{}

func (HostTools) Instructions() string {
	return "Tools of the apps turned on in this chat: app_open shows an app's view to the person in the right pane; " +
		"app_data_get and app_data_set keep the app's data of this chat; app_ask puts a choice to the person in the app and waits for it. " +
		"widget_show draws an app's chat view in the feed with the data you give; widget_render draws a widget you write yourself " +
		"(HTML, CSS and a classic script, no network; window.echarts and window.widgetData are there). The same id changes a widget where it stands. " +
		"The sinopia_* tools read and change the Sinopia document the editor shows beside this chat, for the apps that work in it."
}

func str(desc string) *jsvalue.Object {
	return jsvalue.NewObject("type", "string", "description", desc)
}

func (HostTools) Tools() []any {
	app := str("The app's id; omit when only one app is on in this chat.")
	tool := func(name, desc string, props *jsvalue.Object, required ...any) any {
		return jsvalue.NewObject("name", name, "description", desc,
			"inputSchema", jsvalue.NewObject("type", "object", "properties", props, "required", required))
	}
	return append([]any{
		tool("open", "Open an app's view in the person's right pane.",
			jsvalue.NewObject("app", app, "view", str("A view of the app for the pane; omit for its first one."))),
		tool("data_get", "Read what the app keeps for this chat under a key (the same data its view reads with hl.storage.chat). "+
			`Answers {"value": <the value, null if none>, "version": <number>}; the version goes up with every write of the key (0: never written).`,
			jsvalue.NewObject("app", app, "key", str("The key.")), "key"),
		tool("data_set", "Write what the app keeps for this chat under a key; the value is any JSON. The area is small (256 KB). "+
			"Pass ifVersion (the version data_get gave) to write only if nobody changed the key since: otherwise the answer is `conflict` with the current version and nothing is written. "+
			"A full area is `quota_exceeded`. Answers {\"version\": <the new version>}.",
			jsvalue.NewObject("app", app, "key", str("The key."), "value", jsvalue.NewObject("description", "Any JSON value."),
				"ifVersion", jsvalue.NewObject("type", "integer", "minimum", 0, "description", "Write only if the key is at this version (0: not written yet).")), "key", "value"),
		tool("settings_get", "Read the app's settings: the values the person saved in its form over the defaults (the same a view reads with hl.settings.get()). Secrets are not among them and cannot be read.",
			jsvalue.NewObject("app", app)),
		tool("ask", "Put a choice to the person in the app's view and wait for the answer (up to ten minutes). Returns the option picked, or 'cancelled'.",
			jsvalue.NewObject("app", app, "question", str("What to decide, one short sentence."),
				"options", jsvalue.NewObject("type", "array", "items", jsvalue.NewObject("type", "string"),
					"description", "Two to eight options, each up to 80 characters.")), "question", "options"),
		tool("widget_show", "Show an app's widget (a view of the app for the chat) in the feed of this chat, drawn with the data you give. "+
			"Give the same id again to change that widget where it stands (progress, fresh numbers). Answers the widget's id.",
			jsvalue.NewObject("app", app, "view", str("The app's view for the chat (its card)."),
				"data", jsvalue.NewObject("description", "Any JSON the view reads as hl.context.data, up to 64 KB."),
				"id", str("A widget of this chat to change: lower-case latin letters, digits, '-' and '_', up to 40. Omit for a new one."),
				"title", str("A short name of the widget, optional.")), "view"),
		tool("widget_render", "Draw a widget you write yourself in the feed of this chat: a chart, a table, a form, a progress. "+
			"It runs in a sandbox with no network: no CDN, no fetch. window.echarts (Apache ECharts) and window.widgetData (your data) "+
			"are there, the host's base style (/__hl/widget.css: classes row, muted, stat, chart, button.primary) is on, and harnessly.connect() "+
			"gives hl.chat.send(text) to send the person's choice to you. Give the same id again to change it in place.",
			jsvalue.NewObject("title", str("A short name of the widget."),
				"html", str("The body's markup (no <script> in it: scripts go in `script`). Up to 128 KB."),
				"css", str("Its stylesheet, optional. Up to 64 KB."),
				"script", str("Its script: a classic script that runs after the markup, optional. Up to 256 KB."),
				"data", jsvalue.NewObject("description", "Any JSON, as window.widgetData and hl.context.data; up to 64 KB."),
				"id", str("A widget of this chat to change; omit for a new one.")), "title", "html"),
	}, sinopiaToolList()...)
}

func (HostTools) Has(name string) bool {
	return slices.Contains([]string{"open", "data_get", "data_set", "settings_get", "ask", "widget_show", "widget_render"}, name) || isSinopiaTool(name)
}

func refuse(msg string) *jsvalue.Object { return mcpserver.TextResult("refused: "+msg, true) }

func (t HostTools) Call(ctx context.Context, name string, args *jsvalue.Object) *jsvalue.Object {
	if isSinopiaTool(name) {
		return t.sinopia(name, args)
	}
	if name == "widget_render" {
		return t.widgetRender(args)
	}
	enabled := t.Enabled()
	if len(enabled) == 0 {
		return refuse("no app is on in this chat")
	}
	id, _ := args.Get("app")
	app, _ := id.(string)
	if app == "" {
		if len(enabled) != 1 {
			return refuse("several apps are on in this chat (" + strings.Join(enabled, ", ") + "): name one in `app`")
		}
		app = enabled[0]
	} else if !slices.Contains(enabled, app) {
		return refuse(app + " is not on in this chat")
	}
	installed, err := t.Registry.Get(app)
	if err != nil || installed.Manifest == nil {
		return refuse(app + " is not installed")
	}
	text := func(key string) string { v, _ := args.Get(key); s, _ := v.(string); return s }
	switch name {
	case "open":
		return t.open(installed, text("view"))
	case "settings_get":
		values, err := t.Storage.Settings(installed.Manifest, app)
		if err != nil {
			return refuse("the app's settings could not be read")
		}
		raw, _ := json.Marshal(values)
		return mcpserver.TextResult(string(raw), false)
	case "data_get":
		raw, version, ok, err := t.Storage.GetVersioned(app, ScopeChat, t.Conv, text("key"))
		switch {
		case errors.Is(err, ErrStorageArgs):
			return refuse("the key must be a text of up to 256 characters")
		case err != nil:
			return refuse("the app's data could not be read")
		}
		value := json.RawMessage("null")
		if ok {
			value = raw
		}
		out, _ := json.Marshal(struct {
			Value   json.RawMessage `json:"value"`
			Version int64           `json:"version"`
		}{value, version})
		return mcpserver.TextResult(string(out), false)
	case "data_set":
		v, _ := args.Get("value")
		raw := jsvalue.Stringify(v, "")
		if !json.Valid(raw) {
			return refuse("the value is not JSON")
		}
		var ifVersion *int64
		if n, has := args.Number("ifVersion"); has {
			if n < 0 || n != float64(int64(n)) {
				return refuse("ifVersion is a whole number, 0 or more")
			}
			iv := int64(n)
			ifVersion = &iv
		}
		version, err := t.Storage.SetIf(app, ScopeChat, t.Conv, text("key"), raw, ifVersion)
		var conflict *ConflictError
		switch {
		case errors.As(err, &conflict):
			return refuse(fmt.Sprintf("%s: the key is at version %d, not the version you read; read it again and decide. Nothing was written", CodeConflict, conflict.Current))
		case errors.Is(err, ErrQuota):
			return refuse(CodeQuotaExceeded + ": the app's data of this chat is full")
		case errors.Is(err, ErrStorageArgs):
			return refuse("the key must be a text of up to 256 characters")
		case err != nil:
			return refuse("the app's data could not be written")
		}
		return mcpserver.TextResult(fmt.Sprintf(`{"version":%d}`, version), false)
	case "widget_show":
		return t.widgetShow(installed, args)
	case "ask":
		var options []string
		if v, _ := args.Get("options"); v != nil {
			list, _ := v.([]any)
			for _, o := range list {
				s, ok := o.(string)
				if !ok {
					return refuse("options are texts")
				}
				options = append(options, s)
			}
		}
		answer, cancelled, err := t.Asks.Ask(ctx, t.Conv, app, installed.Manifest.Name, text("question"), options)
		switch {
		case errors.Is(err, ErrAskArgs):
			return refuse("give a question of up to 300 characters and two to eight options of up to 80")
		case errors.Is(err, ErrAskBusy):
			return refuse("too many open choices in this chat: wait for the person")
		case err != nil:
			return refuse("the choice could not be shown")
		case cancelled:
			return mcpserver.TextResult("cancelled", false)
		}
		return mcpserver.TextResult(answer, false)
	}
	return refuse("no such tool")
}

func (t HostTools) open(a Installed, view string) *jsvalue.Object {
	var pick *View
	for i, v := range a.Manifest.Views {
		if slices.Contains(v.Targets, TargetPane) && (view == "" || v.ID == view) {
			pick = &a.Manifest.Views[i]
			break
		}
	}
	if pick == nil {
		return refuse(a.ID + " has no such view for the pane")
	}
	if t.Open == nil {
		return refuse("this chat has no pane to open it in")
	}
	if err := t.Open(a.ID + "/" + pick.ID); err != nil {
		return refuse("the pane could not be asked: " + err.Error())
	}
	return mcpserver.TextResult("opened "+a.ID+"/"+pick.ID+" in the person's pane", false)
}

// WidgetAnswer is the text a widget tool answers with, one short line: the feed finds the widget by its id in it (the
// tool's answer reaches the feed through the transcript, which keeps only the start of the first line). A new widget is
// «shown» and drawn under that step; one shown again is «changed», and changes where it first stood.
func WidgetAnswer(w Widget) string {
	if w.Rev > 1 {
		return "widget " + w.ID + " changed"
	}
	return "widget " + w.ID + " shown"
}

func (t HostTools) widgetDone(w Widget, err error) *jsvalue.Object {
	switch {
	case errors.Is(err, ErrWidgetArgs):
		return refuse("the widget is past its limits: id of lower-case latin letters, digits, '-' and '_' up to 40, title up to 120, data JSON up to 64 KB, html 128 KB, css 64 KB, script 256 KB")
	case errors.Is(err, ErrWidgetsFull):
		return refuse("this chat has as many widgets as it may keep: change one by its id")
	case errors.Is(err, ErrWidgetKind):
		return refuse("that id is another widget's: give a new id")
	case err != nil:
		return refuse("the widget could not be kept")
	}
	if t.WidgetChanged != nil {
		t.WidgetChanged(t.Conv)
	}
	return mcpserver.TextResult(WidgetAnswer(w), false)
}

func rawArg(args *jsvalue.Object, key string) json.RawMessage {
	v, ok := args.Get(key)
	if !ok || v == nil {
		return nil
	}
	return json.RawMessage(jsvalue.Stringify(v, ""))
}

func (t HostTools) widgetShow(a Installed, args *jsvalue.Object) *jsvalue.Object {
	if t.Widgets == nil {
		return refuse("this machine keeps no widgets")
	}
	text := func(key string) string { v, _ := args.Get(key); s, _ := v.(string); return s }
	view := text("view")
	found := false
	for _, v := range a.Manifest.Views {
		found = found || (v.ID == view && slices.Contains(v.Targets, TargetChat))
	}
	if !found {
		return refuse(a.ID + " has no view " + view + " for the chat")
	}
	w, err := t.Widgets.Show(t.Conv, WidgetInput{ID: text("id"), Kind: WidgetView, App: a.ID, View: view, Title: text("title"), Data: rawArg(args, "data")})
	return t.widgetDone(w, err)
}

func (t HostTools) widgetRender(args *jsvalue.Object) *jsvalue.Object {
	if t.Widgets == nil {
		return refuse("this machine keeps no widgets")
	}
	text := func(key string) string { v, _ := args.Get(key); s, _ := v.(string); return s }
	html := text("html")
	if strings.Contains(strings.ToLower(html), "<script") {
		return refuse("put the script in `script`, not in the markup: the widget's page takes no inline script")
	}
	w, err := t.Widgets.Show(t.Conv, WidgetInput{ID: text("id"), Kind: WidgetHTML, App: WidgetsApp, Title: text("title"),
		Data: rawArg(args, "data"), HTML: html, Style: text("css"), Script: text("script")})
	return t.widgetDone(w, err)
}
