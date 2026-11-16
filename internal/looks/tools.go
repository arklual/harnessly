package looks

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
)

// The agents' MCP tools of the looks: the same core as `harnessly theme` (Inbox.Submit), another way in.
const (
	ToolGet = "theme_get"
	ToolSet = "theme_set"
)

// Tools are the look tools for one calling chat.
type Tools struct {
	Inbox *Inbox
	// Caller is the calling chat ("" when it could not be told): folder and chat scopes need it.
	Caller string
}

var _ mcpserver.Toolset = Tools{}

func (Tools) Instructions() string {
	return "Looks: when the person asks to change how harnessly looks — a theme, colours, a background for the chats, an animated " +
		"background (matrix, stars, aurora, rain, synthwave…), grain, a font, the look of the message bubbles or the field " +
		"(«сделай тему как ночной Токио», «поставь на эту папку матрицу», «фон со звёздами в этом чате») — call theme_set " +
		"(a preset id, or a look you compose; scope all, folder or chat). The page applies it at once with Undo. Never write CSS " +
		"or code for it."
}

func (Tools) Tools() []any {
	presets := []any{}
	names := []string{}
	for _, p := range Presets() {
		presets = append(presets, p.ID)
		names = append(names, p.ID+" ("+p.NameEn+")")
	}
	return []any{
		jsvalue.NewObject(
			"name", ToolGet,
			"description", "The person's looks of harnessly now (the one for all chats, the folders', the chats') and the ready presets.",
			"inputSchema", jsvalue.NewObject("type", "object", "properties", jsvalue.NewObject()),
		),
		jsvalue.NewObject(
			"name", ToolSet,
			"description", "Change the person's look of harnessly: colours, the chats' background (gradient, picture, animated scene), "+
				"grain, dim, roundness, font, and safe variants of the chat's parts. Give a preset ("+strings.Join(names, ", ")+
				"), or a look, or reset. scope: all (every chat, the default), folder (the folder of this chat), chat (this chat). "+
				"A chat wears its own look, else its folder's, else the one for all. The page applies it with «Applied · Undo»; "+
				"the answer says applied, or waiting when no page of the person is open. Look fields: v=1, name, mode auto|light|dark, "+
				"hues [{h 0-360, s 0-100}] 1-3 (the first is the accent), intensity 0-100, accent {h,s}?, bg {kind none|color|gradient|image|scene, "+
				"scene, image (sha256 of an uploaded picture), fit cover|tile, speed 0-100}, grain 0-100, dim 0-100, blur 0-24, "+
				"radius sharp|default|round, density compact|cozy, font system|inter|serif|mono|rounded, parts {bubble soft|solid|outline|glass, "+
				"turns plain|card|glass, composer solid|glass|outline, sidebar plain|tinted|glass}.",
			"inputSchema", jsvalue.NewObject(
				"type", "object",
				"properties", jsvalue.NewObject(
					"scope", jsvalue.NewObject("type", "string", "enum", []any{ScopeAll, ScopeFolder, ScopeChat}, "description", "Where: all chats, this chat's folder, or this chat."),
					"preset", jsvalue.NewObject("type", "string", "enum", presets, "description", "A ready look."),
					"look", jsvalue.NewObject("type", "object", "description", "A look of your own (see the fields above); bg.scene is one of "+strings.Join(Scenes, ", ")+"."),
					"reset", jsvalue.NewObject("type", "boolean", "description", "Take the scope's own look away: it inherits again."),
					"why", jsvalue.NewObject("type", "string", "description", "What the person asked for, in a few words."),
				),
				"required", []any{"why"},
			),
		),
	}
}

func (Tools) Has(name string) bool { return name == ToolGet || name == ToolSet }

func (t Tools) Call(ctx context.Context, name string, args *jsvalue.Object) *jsvalue.Object {
	if t.Inbox == nil {
		return mcpserver.TextResult("looks are not available on this machine", true)
	}
	if name == ToolGet {
		out := map[string]any{"looks": t.Inbox.State(), "presets": Presets()}
		raw, _ := json.Marshal(out)
		return mcpserver.TextResult(string(raw), false)
	}
	a := Ask{Scope: args.String("scope"), Conv: t.Caller, Preset: args.String("preset"), Why: args.String("why"), Wait: 20 * time.Second}
	if v, _ := args.Get("reset"); v == true {
		a.Reset = true
	}
	if o := args.Object("look"); o != nil {
		a.Look = jsvalue.Stringify(o, "")
	}
	if strings.TrimSpace(a.Why) == "" {
		return mcpserver.TextResult("refused: give why (what the person asked for)", true)
	}
	req, status, err := t.Inbox.Submit(ctx, a)
	if err != nil {
		return mcpserver.TextResult("refused: "+err.Error(), true)
	}
	switch status {
	case StateApplied:
		return mcpserver.TextResult("applied on "+req.Scope+": the person sees it now, with Undo", false)
	case StateFailed:
		return mcpserver.TextResult("failed: the page did not apply it — "+req.Note, true)
	}
	return mcpserver.TextResult("waiting: no page of the person has it on yet; it applies when they open harnessly", false)
}
