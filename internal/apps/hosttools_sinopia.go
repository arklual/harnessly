package apps

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
)

// The Document API for the agent: the tools sinopia_* of the harnessly-app server (the policy reads them as
// app.sinopia.*). They are the same calls a view makes (sinopia_api.go), for the document the editor shows beside the
// agent's chat, or the one named in `document`.

// SinopiaHost is what the agent's tools need of the machine's Sinopia.
type SinopiaHost struct {
	Docs  SinopiaDocs
	State *SinopiaState
	// Tab is the design tab of a chat (the holder of its claims); "" when it has none.
	Tab func(conv string) string
	// Package reads an installed app's package; nil when it cannot be read.
	Package func(id string) *Package
}

// sinopiaTools are the tool names with the Document API method each calls.
var sinopiaTools = map[string]string{
	"sinopia_document":   "document",
	"sinopia_selection":  "selection",
	"sinopia_node":       "node",
	"sinopia_query":      "query",
	"sinopia_fields":     "fields",
	"sinopia_set_fields": "setFields",
	"sinopia_patch":      "patch",
	"sinopia_mark":       "mark",
	"sinopia_comment":    "comment",
	"sinopia_render":     "render",
	"sinopia_boards_add": "boards.add",
}

func sinopiaToolList() []any {
	app := str("The app's id; omit when only one app works in this document.")
	doc := str("The Sinopia document (project) id; omit for the one the editor shows beside this chat.")
	tool := func(name, desc string, props *jsvalue.Object, required ...any) any {
		props.Set("app", app)
		props.Set("document", doc)
		return jsvalue.NewObject("name", name, "description", desc,
			"inputSchema", jsvalue.NewObject("type", "object", "properties", props, "required", required))
	}
	obj := func(desc string) *jsvalue.Object { return jsvalue.NewObject("type", "object", "description", desc) }
	list := func(desc string) *jsvalue.Object {
		return jsvalue.NewObject("type", "array", "description", desc, "items", jsvalue.NewObject("type", "object"))
	}
	return []any{
		tool("sinopia_document", "The Sinopia document: its kind, title, boards and revision.", jsvalue.NewObject()),
		tool("sinopia_selection", "The elements the person picked in the editor now.", jsvalue.NewObject()),
		tool("sinopia_node", "One element of a board by its id (<page id>:<number>).", jsvalue.NewObject("id", str("The node id.")), "id"),
		tool("sinopia_query", "Elements that match a selector (type, #id, .class, [attr], space, >), in one board or all.",
			jsvalue.NewObject("selector", str("The selector."), "page", str("A page id, file or title; omit for all.")), "selector"),
		tool("sinopia_fields", "The data fields the app binds to the board's elements, with their values.", jsvalue.NewObject()),
		tool("sinopia_set_fields", "Set the values of bound fields; the board is rewritten, as one history entry. A value outside the app's schema is refused.",
			jsvalue.NewObject("patch", obj("Field name to value.")), "patch"),
		tool("sinopia_patch", "Change the board: ops setText, setStyle, setAttr, insert, remove, setField, all or none, as one history entry. A board another chat holds is refused.",
			jsvalue.NewObject("ops", list("Each op is {op, node, …}: setText {value}, setStyle {style}, setAttr {name, value}, insert {position, html}, remove, setField {field, value}.")), "ops"),
		tool("sinopia_mark", "Put a note on elements of the board, which the person sees on it.",
			jsvalue.NewObject("nodes", jsvalue.NewObject("type", "array", "items", jsvalue.NewObject("type", "string")), "note", str("The note."),
				"level", str("error, warning or info."), "replace", jsvalue.NewObject("type", "boolean", "description", "Take this app's earlier marks away first; with no nodes it clears them.")), "nodes"),
		tool("sinopia_comment", "Comment an element: the comment shows on the board as a mark.",
			jsvalue.NewObject("node", str("The node id."), "text", str("The comment.")), "node", "text"),
		tool("sinopia_render", "Render a board as PNG, PDF or HTML. Not available yet.", jsvalue.NewObject("board", str("The page."), "format", str("png, pdf or html.")), "board"),
		tool("sinopia_boards_add", "Add a board from one of the app's templates, with data for its bound fields.",
			jsvalue.NewObject("template", str("The template's name."), "data", obj("Field name to value."), "title", str("The board's title.")), "template"),
	}
}

func isSinopiaTool(name string) bool { _, ok := sinopiaTools[name]; return ok }

// sinopia runs one Document API tool of the agent of the chat.
func (t HostTools) sinopia(name string, args *jsvalue.Object) *jsvalue.Object {
	h := t.Sinopia
	if h == nil || h.Docs == nil {
		return refuse("this machine has no Sinopia")
	}
	text := func(key string) string { v, _ := args.Get(key); s, _ := v.(string); return s }
	project, kind := "", ""
	if project = text("document"); project != "" {
		doc, err := h.Docs.Document(project)
		if err != nil {
			return refuse(err.Error())
		}
		kind = doc.Kind
	} else {
		var ok bool
		if project, kind, ok = h.State.DocumentOf(t.Conv); !ok {
			return refuse("no Sinopia document is open beside this chat: name one in `document`")
		}
	}
	// The apps that work here: those turned on in this chat and those that are on in every document of the kind.
	candidates := slices.Clone(t.Enabled())
	if list, err := t.Registry.List(); err == nil {
		for _, a := range ActiveInDocument(list, kind) {
			if !slices.Contains(candidates, a.ID) {
				candidates = append(candidates, a.ID)
			}
		}
	}
	var working []string
	for _, id := range candidates {
		if a, err := t.Registry.Get(id); err == nil && a.Manifest != nil && a.Manifest.HasTarget("sinopia."+kind) {
			working = append(working, id)
		}
	}
	app := text("app")
	switch {
	case app != "" && !slices.Contains(working, app):
		return refuse(app + " does not work in this " + kind + " document")
	case app == "" && len(working) == 0:
		return refuse("no app works in this " + kind + " document")
	case app == "" && len(working) > 1:
		return refuse("several apps work in this document (" + strings.Join(working, ", ") + "): name one in `app`")
	case app == "":
		app = working[0]
	}
	installed, err := t.Registry.Get(app)
	if err != nil || installed.Manifest == nil {
		return refuse(app + " is not installed")
	}
	params := map[string]any{}
	if args != nil {
		for _, k := range args.Keys() {
			if k != "app" && k != "document" {
				v, _ := args.Get(k)
				params[k] = plain(v)
			}
		}
	}
	call := SinopiaCall{Docs: h.Docs, State: h.State, App: installed, Project: project, Who: installed.Manifest.Name, Agent: true}
	if h.Tab != nil {
		call.Tab = h.Tab(t.Conv)
	}
	if h.Package != nil {
		call.Pkg = h.Package(app)
	}
	res, err := call.Call(sinopiaTools[name], params)
	if err != nil {
		var be *BridgeError
		if errors.As(err, &be) {
			return refuse(be.Message)
		}
		return refuse(err.Error())
	}
	out, err := json.Marshal(res)
	if err != nil {
		return refuse("the answer could not be written")
	}
	return mcpserver.TextResult(string(out), false)
}

// plain turns what the MCP server parsed (jsvalue objects) into plain maps and lists.
func plain(v any) any {
	switch x := v.(type) {
	case *jsvalue.Object:
		m := map[string]any{}
		for _, k := range x.Keys() {
			c, _ := x.Get(k)
			m[k] = plain(c)
		}
		return m
	case []any:
		out := make([]any, len(x))
		for i, c := range x {
			out[i] = plain(c)
		}
		return out
	}
	return v
}
