package apps

import (
	"context"
	"encoding/json"
	"errors"
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
}

var _ mcpserver.Toolset = HostTools{}

func (HostTools) Instructions() string {
	return "Tools of the apps turned on in this chat: app_open shows an app's view to the person in the right pane; " +
		"app_data_get and app_data_set keep the app's data of this chat; app_ask puts a choice to the person in the app and waits for it. " +
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
		tool("data_get", "Read what the app keeps for this chat under a key (the same data its view reads with hl.storage.chat).",
			jsvalue.NewObject("app", app, "key", str("The key.")), "key"),
		tool("data_set", "Write what the app keeps for this chat under a key; the value is any JSON. The area is small (256 KB).",
			jsvalue.NewObject("app", app, "key", str("The key."), "value", jsvalue.NewObject("description", "Any JSON value.")), "key", "value"),
		tool("ask", "Put a choice to the person in the app's view and wait for the answer (up to ten minutes). Returns the option picked, or 'cancelled'.",
			jsvalue.NewObject("app", app, "question", str("What to decide, one short sentence."),
				"options", jsvalue.NewObject("type", "array", "items", jsvalue.NewObject("type", "string"),
					"description", "Two to eight options, each up to 80 characters.")), "question", "options"),
	}, sinopiaToolList()...)
}

func (HostTools) Has(name string) bool {
	return slices.Contains([]string{"open", "data_get", "data_set", "ask"}, name) || isSinopiaTool(name)
}

func refuse(msg string) *jsvalue.Object { return mcpserver.TextResult("refused: "+msg, true) }

func (t HostTools) Call(ctx context.Context, name string, args *jsvalue.Object) *jsvalue.Object {
	if isSinopiaTool(name) {
		return t.sinopia(name, args)
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
	case "data_get":
		raw, ok, err := t.Storage.Get(app, ScopeChat, t.Conv, text("key"))
		switch {
		case errors.Is(err, ErrStorageArgs):
			return refuse("the key must be a text of up to 256 characters")
		case err != nil:
			return refuse("the app's data could not be read")
		case !ok:
			return mcpserver.TextResult("null", false)
		}
		return mcpserver.TextResult(string(raw), false)
	case "data_set":
		v, _ := args.Get("value")
		raw := jsvalue.Stringify(v, "")
		if !json.Valid(raw) {
			return refuse("the value is not JSON")
		}
		switch err := t.Storage.Set(app, ScopeChat, t.Conv, text("key"), raw); {
		case errors.Is(err, ErrQuota):
			return refuse("the app's data of this chat is full")
		case errors.Is(err, ErrStorageArgs):
			return refuse("the key must be a text of up to 256 characters")
		case err != nil:
			return refuse("the app's data could not be written")
		}
		return mcpserver.TextResult("saved", false)
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
