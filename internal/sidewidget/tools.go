package sidewidget

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
)

// The MCP tools of the widgets under the chat's right column: the same acts as `harnessly widget create|update|list|
// remove`, on the same Service.
const (
	ToolCreate = "widget_create"
	ToolUpdate = "widget_update"
	ToolList   = "widget_list"
	ToolRemove = "widget_remove"
)

// Instruction is what an agent is told of the widgets, here and in the workspace's prompt.
const Instruction = "Widgets of the right column: on a computer the person keeps small cards under the chat's right column " +
	"(«Лимиты», «Машина», «Подчаты», «Выкат», «Заметка», «Часы» they add themselves from «+ Виджет»). When the person asks for " +
	"a widget that shows something («сделай виджет, который показывает…», «добавь виджет …», \"make a widget that shows …\"), " +
	"make it with widget_create (or `harnessly widget create`): a name, a title, and what it shows — `sources` (limits, machine, " +
	"subagents: the host's data it is given), `smart` (a request a model answers over those data every `every` minutes, at least 5, " +
	"and at once on the card's button) and/or your own `html`/`css`/`js` (a sandboxed page: hl.context.data has {sources, smart}, " +
	"hl.on('context', fn) brings new data; no network, no secrets, no chats). It appears in the person's stack by itself. Change it " +
	"with widget_update, list with widget_list, remove with widget_remove. A built-in widget is not made: tell the person to add it " +
	"from «+ Виджет» under the right column."

// Tools are the widgets' MCP tools.
type Tools struct {
	S *Service
}

var _ mcpserver.Toolset = Tools{}

func (Tools) Instructions() string { return Instruction }

func specProps() *jsvalue.Object {
	str := func(desc string) *jsvalue.Object { return jsvalue.NewObject("type", "string", "description", desc) }
	num := func(desc string) *jsvalue.Object { return jsvalue.NewObject("type", "number", "description", desc) }
	return jsvalue.NewObject(
		"name", str("The widget's name: lower case latin letters, digits and '-', up to 33 (e.g. limits-forecast)."),
		"title", str("What the card is called, in the person's language, short (e.g. «Прогноз лимитов»)."),
		"icon", str("A Lucide icon name (gauge, server, clock, git-merge, sparkles…); optional."),
		"sources", jsvalue.NewObject("type", "array", "items", jsvalue.NewObject("type", "string", "enum", []any{"limits", "machine", "subagents"}),
			"description", "The host's data the widget is given: limits (5 h and week left by subscription), machine (CPU, memory, disk), subagents (the delegated chats at work)."),
		"smart", str("A request a model answers over the sources in at most three short lines (the card shows the answer); omit for a widget that only draws its data."),
		"every", num("How often, in minutes, the smart request is asked again (5 to 1440, default 30)."),
		"size", str("The card's height: s (96 px, default), m (160) or l (240)."),
		"refresh", num("How often, in seconds, the data are given again while the card is on screen (30 at least; 0: once)."),
		"html", str("Your own markup of the body (optional): without it the card shows the smart answer, or the data."),
		"css", str("Your own style (optional); the host's tokens --t-* and classes .hl-* are there."),
		"js", str("Your own classic script (optional): window.harnessly.connect().then(hl => …), hl.context.data = {sources, smart}, hl.on('context', fn)."),
	)
}

func (Tools) Tools() []any {
	return []any{
		jsvalue.NewObject("name", ToolCreate,
			"description", "Make a widget under the chat's right column that shows something the person asked for («сделай виджет, который показывает…»). It is added to the person's widgets by itself, on every computer.",
			"inputSchema", jsvalue.NewObject("type", "object", "properties", specProps(), "required", []any{"name", "title"})),
		jsvalue.NewObject("name", ToolUpdate,
			"description", "Change a widget you made: only the fields given change.",
			"inputSchema", jsvalue.NewObject("type", "object", "properties", specProps(), "required", []any{"name"})),
		jsvalue.NewObject("name", ToolList,
			"description", "The widgets agents made on this machine: name, title, sources, smart request.",
			"inputSchema", jsvalue.NewObject("type", "object", "properties", jsvalue.NewObject())),
		jsvalue.NewObject("name", ToolRemove,
			"description", "Remove a widget you made, by its name.",
			"inputSchema", jsvalue.NewObject("type", "object", "properties", jsvalue.NewObject(
				"name", jsvalue.NewObject("type", "string", "description", "The widget's name.")), "required", []any{"name"})),
	}
}

func (Tools) Has(name string) bool {
	return name == ToolCreate || name == ToolUpdate || name == ToolList || name == ToolRemove
}

func (t Tools) Call(_ context.Context, name string, args *jsvalue.Object) *jsvalue.Object {
	if t.S == nil {
		return mcpserver.TextResult("refused: widgets are not served on this machine", true)
	}
	raw := jsvalue.Stringify(args, "")
	switch name {
	case ToolList:
		list, err := t.S.List()
		if err != nil {
			return mcpserver.TextResult("failed: "+err.Error(), true)
		}
		if len(list) == 0 {
			return mcpserver.TextResult("no widgets made by agents", false)
		}
		lines := make([]string, 0, len(list))
		for _, w := range list {
			lines = append(lines, ListLine(w))
		}
		return mcpserver.TextResult(strings.Join(lines, "\n"), false)
	case ToolRemove:
		var b struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(raw, &b)
		if err := t.S.Remove(b.Name); err != nil {
			return mcpserver.TextResult("refused: "+err.Error(), true)
		}
		return mcpserver.TextResult("widget "+b.Name+" removed", false)
	case ToolCreate:
		var sp Spec
		if err := json.Unmarshal(raw, &sp); err != nil {
			return mcpserver.TextResult("refused: "+err.Error(), true)
		}
		if _, err := t.S.Create(sp); err != nil {
			return mcpserver.TextResult("refused: "+err.Error(), true)
		}
		return mcpserver.TextResult(Created(sp.Name, sp.Title), false)
	case ToolUpdate:
		var b struct {
			Name string `json:"name"`
			Patch
		}
		if err := json.Unmarshal(raw, &b); err != nil {
			return mcpserver.TextResult("refused: "+err.Error(), true)
		}
		if _, err := t.S.Update(b.Name, b.Patch); err != nil {
			return mcpserver.TextResult("refused: "+err.Error(), true)
		}
		return mcpserver.TextResult("widget "+b.Name+" changed", false)
	}
	return mcpserver.TextResult("unknown tool", true)
}

// ListLine is one widget of a list, as the CLI prints it too.
func ListLine(w Info) string {
	line := fmt.Sprintf("%s\t%s", w.Name, w.Title)
	if len(w.Sources) > 0 {
		line += "\tsources " + strings.Join(w.Sources, ",")
	}
	if w.Smart != "" {
		line += fmt.Sprintf("\tsmart every %d min: %s", max(w.Every, 30*boolInt(w.Every == 0)), w.Smart)
	}
	return line
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Created is the answer to a widget made.
func Created(name, title string) string {
	return fmt.Sprintf("widget %s («%s») made: it is under the chat's right column on the person's computers now", name, title)
}
