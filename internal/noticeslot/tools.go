package noticeslot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
)

// The agent's MCP tools of the slot: the same description as `harnessly notice` and the SDK's hl.notice.
const (
	ToolAsk    = "notice_ask"
	ToolShow   = "notice_show"
	ToolUpdate = "notice_update"
	ToolClose  = "notice_close"
)

// Tools is the slot's MCP tools for one calling agent: Chat is its chat ("" when the runtime could not tell, Why says
// why), Name the chat's title for the label over the plate.
type Tools struct {
	S          *Slot
	Chat, Name string
	Why        string
}

var _ mcpserver.Toolset = Tools{}

func (Tools) Instructions() string {
	return "Asking the person in the smart notice: notice_ask shows a choice, a yes/no confirmation or a short field in " +
		"the black plate at the top of their screen and waits for the answer; notice_show shows a status or a progress (notice_update " +
		"moves it, notice_close takes it away). Use it for a decision the work cannot go on without, not for chat. Never ask for a " +
		"password, a key, a token or a sign-in through it: the slot refuses them; for a secret run `harnessly secret ask`, for a site " +
		"sign-in use the browser's sign-in."
}

func specSchema(kinds []any) *jsvalue.Object {
	str := func(d string) *jsvalue.Object { return jsvalue.NewObject("type", "string", "description", d) }
	return jsvalue.NewObject(
		"type", "object",
		"properties", jsvalue.NewObject(
			"kind", jsvalue.NewObject("type", "string", "enum", kinds, "description", "What the plate shows."),
			"title", str("The question or what happens, one line (up to 120 characters)."),
			"text", str("A few words more (up to 600 characters)."),
			"options", jsvalue.NewObject("type", "array", "items", jsvalue.NewObject("type", "string"),
				"description", "choice: 2 to 6 answers, each up to 80 characters."),
			"own", jsvalue.NewObject("type", "boolean", "description", "choice: also a line for the person's own words."),
			"yes", str("confirm: the button that agrees (default «Да»)."),
			"no", str("confirm: the button that declines (default «Нет»)."),
			"danger", jsvalue.NewObject("type", "boolean", "description", "confirm: the action cannot be undone (the yes button is red and never the default)."),
			"placeholder", str("field: the hint inside the empty field."),
			"progress", jsvalue.NewObject("type", "number", "description", "progress: share done, 0 to 1; omit while unknown."),
			"timeoutSec", jsvalue.NewObject("type", "integer", "description", "How long to wait, seconds (default 600, at most a day)."),
		),
		"required", []any{"kind", "title"},
	)
}

func (Tools) Tools() []any {
	id := jsvalue.NewObject("type", "string", "description", "The id notice_show gave.")
	return []any{
		jsvalue.NewObject(
			"name", ToolAsk,
			"description", "Ask the person in the smart notice and wait for the answer (up to timeoutSec, 10 minutes by default). Returns JSON "+
				"{status: answered|dismissed|timeout|muted, value}: a choice gives {choice: index, option} or {text} for own words, a confirm "+
				"true/false, a field its text. «Hide» is not an answer: the question waits in the chat's column. No passwords, keys "+
				"or sign-ins: they are refused (use `harnessly secret ask`).",
			"inputSchema", specSchema([]any{string(KindChoice), string(KindConfirm), string(KindField)}),
		),
		jsvalue.NewObject(
			"name", ToolShow,
			"description", "Show a status (goes by itself after a few seconds) or a progress (stays until notice_close or its timeout) in the "+
				"smart notice, without waiting. Returns the notice's id for notice_update and notice_close.",
			"inputSchema", specSchema([]any{string(KindStatus), string(KindProgress)}),
		),
		jsvalue.NewObject(
			"name", ToolUpdate,
			"description", "Move a progress or change a status shown by notice_show.",
			"inputSchema", jsvalue.NewObject("type", "object", "properties", jsvalue.NewObject(
				"id", id,
				"progress", jsvalue.NewObject("type", "number", "description", "Share done, 0 to 1."),
				"title", jsvalue.NewObject("type", "string"),
				"text", jsvalue.NewObject("type", "string"),
			), "required", []any{"id"}),
		),
		jsvalue.NewObject(
			"name", ToolClose,
			"description", "Take away a notice this chat showed (a progress that ended, a question no longer needed).",
			"inputSchema", jsvalue.NewObject("type", "object", "properties", jsvalue.NewObject("id", id), "required", []any{"id"}),
		),
	}
}

func (Tools) Has(name string) bool {
	return name == ToolAsk || name == ToolShow || name == ToolUpdate || name == ToolClose
}

// AgentCodeRefusal: an agent never sees a one-time code; the safe hands it to the command that needs it.
const AgentCodeRefusal = "refused: an agent does not get one-time codes through the notice. Run the command that needs the code " +
	"with `harnessly secret run --code VAR --why \"…\" -- COMMAND`: the person types it in the chat's panel and it goes " +
	"straight into the command's environment."

// Refusal is the words an error of the slot gives a caller.
func Refusal(err error) string {
	switch {
	case errors.Is(err, ErrSecret):
		return "refused: the notice slot does not ask for passwords, keys, tokens or sign-ins. For a secret run `harnessly secret ask NAME --why …` (the safe); for a site sign-in use the browser's sign-in."
	case errors.Is(err, ErrMuted):
		return "refused: the person turned notices from this source off"
	case errors.Is(err, ErrRate):
		return "refused: too many notices from this source in the last minute; wait and ask once"
	case errors.Is(err, ErrBusy):
		return "refused: this source has too many open notices; wait for the answers"
	case errors.Is(err, ErrGone):
		return "refused: no such open notice"
	case errors.Is(err, ErrArgs):
		return "refused: check kind, title (up to 120), text (up to 600), choice options (2 to 6, up to 80 each), code digits (4 to 8), progress (0 to 1)"
	}
	return "refused: " + err.Error()
}

func (t Tools) source() Source {
	name := t.Name
	if name == "" {
		name = "Агент"
	}
	return Source{Kind: SourceAgent, ID: t.Chat, Name: name}
}

func (t Tools) Call(ctx context.Context, name string, args *jsvalue.Object) *jsvalue.Object {
	if t.S == nil {
		return mcpserver.TextResult("refused: the notice slot is not running on this machine", true)
	}
	if t.Chat == "" {
		why := t.Why
		if why == "" {
			why = "the machine could not tell this agent's chat"
		}
		return mcpserver.TextResult("refused: "+why, true)
	}
	src := t.source()
	switch name {
	case ToolAsk, ToolShow:
		var sp Spec
		if json.Unmarshal(jsvalue.Stringify(args, ""), &sp) != nil {
			return mcpserver.TextResult(Refusal(ErrArgs), true)
		}
		if sp.Kind == KindCode {
			return mcpserver.TextResult(AgentCodeRefusal, true)
		}
		if sp.Kind.Waits() != (name == ToolAsk) || sp.Kind == KindCustom {
			return mcpserver.TextResult(fmt.Sprintf("refused: %s does not show the kind %q", name, sp.Kind), true)
		}
		if name == ToolShow {
			n, err := t.S.Open(src, t.Chat, sp)
			if err != nil {
				return mcpserver.TextResult(Refusal(err), true)
			}
			return mcpserver.TextResult(string(jsvalue.Stringify(jsvalue.NewObject("id", n.ID), "")), false)
		}
		r, err := t.S.Ask(ctx, src, t.Chat, sp)
		if err != nil {
			return mcpserver.TextResult(Refusal(err), true)
		}
		raw, _ := json.Marshal(r)
		return mcpserver.TextResult(string(raw), false)
	case ToolUpdate:
		id := args.String("id")
		var progress *float64
		if p, ok := args.Number("progress"); ok {
			progress = &p
		}
		var title, text *string
		if args.Has("title") {
			s := args.String("title")
			title = &s
		}
		if args.Has("text") {
			s := args.String("text")
			text = &s
		}
		if err := t.S.Update(src, id, progress, text, title); err != nil {
			return mcpserver.TextResult(Refusal(err), true)
		}
		return mcpserver.TextResult("updated", false)
	case ToolClose:
		if err := t.S.Close(src, args.String("id")); err != nil {
			return mcpserver.TextResult(Refusal(err), true)
		}
		return mcpserver.TextResult("closed", false)
	}
	return mcpserver.TextResult("refused: unknown tool", true)
}
