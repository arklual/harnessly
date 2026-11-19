// Package routinetools is the agents' MCP tools of the person's routines — recurring agent tasks the hub runs on a
// schedule (docs/hub/routines.md): the same acts as `harnessly routine`, over the same call.
package routinetools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
)

const (
	ToolList   = "routine_list"
	ToolCreate = "routine_create"
	ToolUpdate = "routine_update"
	ToolRun    = "routine_run"
	ToolRuns   = "routine_runs"
	ToolDelete = "routine_delete"
)

// Instruction is what an agent is told of routines.
const Instruction = "Routines (регулярные задачи): when the person asks for something to happen again and again — «каждый " +
	"понедельник присылай аналитику», «каждое утро дайджест в файл», «следи за выкатом и пиши, если упал», «раз в неделю " +
	"проверяй лимиты» — or once at a later time («25-го в 12:00 убери флаг»), make a routine with routine_create (or " +
	"`harnessly routine create`), never a cron job, /loop or CronCreate of your own: those die with this session. The hub runs " +
	"a routine on one of the person's machines in a new chat with no one watching; its agent reports ok, quiet (nothing new) " +
	"or failed with a summary and artifacts, the hub checks the report and tells the person. Write the prompt as a full " +
	"brief for that agent (it does not see this chat): what to gather, from where, what the result looks like, what is " +
	"«nothing new». Put the schedule as cron in the person's zone and as their own words in `words`. Add deterministic " +
	"checks the result must pass (contains a section, a file written, today's date). Pick the channels the person named " +
	"(telegram, file with a path, doc, notice); none named: notice. Say back in one line what was made: name, schedule in " +
	"words, next start, where the result goes. Change one with routine_update (enabled false pauses it; state \"\" clears " +
	"its memory), run it now with routine_run, read its history with routine_runs."

// Tools are the routines' MCP tools; Hub carries a call to the hub (the runtime's agent relay).
type Tools struct {
	Hub func(ctx context.Context, call hubproto.RoutinesCall) (int, []byte, error)
	// Chat is the calling agent's chat: a routine made here remembers it.
	Chat string
}

var _ mcpserver.Toolset = Tools{}

func (Tools) Instructions() string { return Instruction }

func str(desc string) *jsvalue.Object {
	return jsvalue.NewObject("type", "string", "description", desc)
}

func routineProps() *jsvalue.Object {
	return jsvalue.NewObject(
		"name", str("A short name in the person's language, unique among their routines («Дайджест дня»)."),
		"prompt", str("The full brief for the run's agent, which does not see this chat: what to do, from where, what the result looks like, what counts as nothing new."),
		"cron", str("The schedule: five cron fields in the person's zone, minute hour day month weekday («0 9 * * 1-5»). For one time, the exact minute, with once true."),
		"tz", str("The zone of the schedule (default Europe/Moscow)."),
		"words", str("The schedule as the person said it («по будням в 9:00»)."),
		"model", str("sonnet (default), haiku, opus, or default (the hub's setting)."),
		"when", jsvalue.NewObject("type", "string", "enum", []any{"changed", "always", "failure", "never"},
			"description", "When the person hears of a run: changed (default: ok or failed, not quiet), always, failure only, never (just a check)."),
		"channels", jsvalue.NewObject("type", "array", "items", jsvalue.NewObject("type", "string", "enum", []any{"notice", "telegram", "messenger", "mail", "file", "doc"}),
			"description", "Where the result goes besides the run's chat."),
		"file", str("For channel file: the path written as a new version every run ({date} is YYYY-MM-DD), e.g. digest/{date}.md."),
		"checks", jsvalue.NewObject("type", "array", "items", jsvalue.NewObject("type", "object", "properties", jsvalue.NewObject(
			"kind", jsvalue.NewObject("type", "string", "enum", []any{"contains", "regex", "artifact", "file", "fresh"}),
			"value", str("contains: the text; regex: the pattern; artifact: its kind (doc, file, chart, mr…) or empty."),
			"label", str("How the check reads to the person («Есть раздел «Риски»»).")), "required", []any{"kind"}),
			"description", "Deterministic checks of the result, done by the hub; a run that fails one is a failed run."),
		"autonomy", jsvalue.NewObject("type", "string", "enum", []any{"read", "draft", "act"},
			"description", "read (default: only gathers), draft (prepares MRs, documents, drafts; sends nothing), act (changes things, never merges, deploys or writes to people)."),
		"once", jsvalue.NewObject("type", "boolean", "description", "Run one time, then turn off."),
		"until", str("Stop after this day, YYYY-MM-DD."),
		"budget_usd", jsvalue.NewObject("type", "number", "description", "The run's spending cap the agent is told to keep."),
	)
}

func (Tools) Tools() []any {
	id := str("The routine's id or its name.")
	upd := routineProps()
	upd.Set("id", id)
	upd.Set("enabled", jsvalue.NewObject("type", "boolean", "description", "false pauses the routine, true resumes it."))
	upd.Set("state", str("Replaces the routine's memory; \"\" clears it."))
	return []any{
		jsvalue.NewObject("name", ToolList,
			"description", "The person's routines: id, on or paused, schedule, next start, last runs.",
			"inputSchema", jsvalue.NewObject("type", "object", "properties", jsvalue.NewObject())),
		jsvalue.NewObject("name", ToolCreate,
			"description", "Make a routine: a task the hub runs on the person's machine on a schedule, in a new chat, and reports to them.",
			"inputSchema", jsvalue.NewObject("type", "object", "properties", routineProps(), "required", []any{"name", "prompt", "cron"})),
		jsvalue.NewObject("name", ToolUpdate,
			"description", "Change a routine: only the fields given change. enabled false pauses it, state \"\" clears its memory.",
			"inputSchema", jsvalue.NewObject("type", "object", "properties", upd, "required", []any{"id"})),
		jsvalue.NewObject("name", ToolRun,
			"description", "Run a routine now, outside its schedule.",
			"inputSchema", jsvalue.NewObject("type", "object", "properties", jsvalue.NewObject("id", id), "required", []any{"id"})),
		jsvalue.NewObject("name", ToolRuns,
			"description", "A routine's last runs: when, outcome (ok, quiet, failed), summary or error.",
			"inputSchema", jsvalue.NewObject("type", "object", "properties", jsvalue.NewObject("id", id), "required", []any{"id"})),
		jsvalue.NewObject("name", ToolDelete,
			"description", "Delete a routine and its history. Only when the person asked to delete it; to stop it, pause it.",
			"inputSchema", jsvalue.NewObject("type", "object", "properties", jsvalue.NewObject("id", id), "required", []any{"id"})),
	}
}

func (Tools) Has(name string) bool {
	switch name {
	case ToolList, ToolCreate, ToolUpdate, ToolRun, ToolRuns, ToolDelete:
		return true
	}
	return false
}

type args struct {
	ID        string           `json:"id"`
	Name      *string          `json:"name"`
	Prompt    *string          `json:"prompt"`
	Cron      string           `json:"cron"`
	TZ        string           `json:"tz"`
	Words     *string          `json:"words"`
	Model     *string          `json:"model"`
	When      string           `json:"when"`
	Channels  []string         `json:"channels"`
	File      string           `json:"file"`
	Checks    []map[string]any `json:"checks"`
	Autonomy  string           `json:"autonomy"`
	Once      *bool            `json:"once"`
	Until     string           `json:"until"`
	BudgetUSD *float64         `json:"budget_usd"`
	Enabled   *bool            `json:"enabled"`
	State     *string          `json:"state"`
}

// Body is the rule the hub takes, from the tool's arguments. A routine's fields replace the routine whole on update,
// so prev (the routine as it is) fills what the call does not name.
func Body(a args, prev map[string]any, chat string) map[string]any {
	body := map[string]any{}
	if a.Name != nil {
		body["name"] = *a.Name
	}
	if a.Prompt != nil {
		body["promptTemplate"] = *a.Prompt
	}
	if a.Model != nil {
		body["model"] = *a.Model
	}
	if a.Enabled != nil {
		body["enabled"] = *a.Enabled
	}
	if a.State != nil {
		body["state"] = *a.State
	}
	if a.Cron != "" {
		sched := map[string]any{"cron": a.Cron}
		if a.TZ != "" {
			sched["tz"] = a.TZ
		}
		body["trigger"] = map[string]any{"schedule": sched}
	}
	rt := map[string]any{}
	for k, v := range prev {
		rt[k] = v
	}
	changed := prev == nil
	set := func(k string, v any) { rt[k] = v; changed = true }
	if a.Words != nil {
		set("words", *a.Words)
	}
	if a.Autonomy != "" {
		set("autonomy", a.Autonomy)
	}
	if a.Once != nil {
		set("once", *a.Once)
	}
	if a.Until != "" {
		set("until", a.Until)
	}
	if a.BudgetUSD != nil {
		set("budgetUsd", *a.BudgetUSD)
	}
	if a.Checks != nil {
		set("checks", a.Checks)
	}
	if a.When != "" || a.Channels != nil || a.File != "" {
		d, _ := rt["delivery"].(map[string]any)
		nd := map[string]any{}
		for k, v := range d {
			nd[k] = v
		}
		if a.When != "" {
			nd["when"] = a.When
		}
		if a.Channels != nil {
			nd["channels"] = a.Channels
		}
		if a.File != "" {
			nd["file"] = a.File
		}
		set("delivery", nd)
	}
	if prev == nil && chat != "" {
		rt["chatId"] = chat
	}
	if prev == nil {
		if _, ok := rt["delivery"]; !ok {
			rt["delivery"] = map[string]any{"channels": []string{"notice"}}
		}
	}
	if changed {
		body["routine"] = rt
	}
	return body
}

func (t Tools) call(ctx context.Context, c hubproto.RoutinesCall) (map[string]any, error) {
	if t.Hub == nil {
		return nil, fmt.Errorf("routines are not served on this machine")
	}
	status, raw, err := t.Hub(ctx, c)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if status != 200 {
		msg, _ := out["error"].(string)
		if m, ok := out["message"].(string); ok && m != "" {
			msg = m
		}
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return out, nil
}

func (t Tools) Call(ctx context.Context, name string, in *jsvalue.Object) *jsvalue.Object {
	var a args
	if err := json.Unmarshal(jsvalue.Stringify(in, ""), &a); err != nil {
		return mcpserver.TextResult("refused: "+err.Error(), true)
	}
	fail := func(err error) *jsvalue.Object { return mcpserver.TextResult("refused: "+err.Error(), true) }
	pretty := func(v any) *jsvalue.Object {
		b, _ := json.MarshalIndent(v, "", "  ")
		return mcpserver.TextResult(string(b), false)
	}
	switch name {
	case ToolList:
		out, err := t.call(ctx, hubproto.RoutinesCall{Op: "list"})
		if err != nil {
			return fail(err)
		}
		return pretty(out["routines"])
	case ToolCreate:
		raw, _ := json.Marshal(Body(a, nil, t.Chat))
		out, err := t.call(ctx, hubproto.RoutinesCall{Op: "create", Body: raw})
		if err != nil {
			return fail(err)
		}
		return mcpserver.TextResult(saved("made", out), false)
	case ToolUpdate:
		cur, err := t.call(ctx, hubproto.RoutinesCall{Op: "get", ID: a.ID})
		if err != nil {
			return fail(err)
		}
		r, _ := cur["routine"].(map[string]any)
		prev, _ := r["routine"].(map[string]any)
		if prev == nil {
			prev = map[string]any{}
		}
		raw, _ := json.Marshal(Body(a, prev, t.Chat))
		out, err := t.call(ctx, hubproto.RoutinesCall{Op: "update", ID: a.ID, Body: raw})
		if err != nil {
			return fail(err)
		}
		return mcpserver.TextResult(saved("saved", out), false)
	case ToolRun:
		if _, err := t.call(ctx, hubproto.RoutinesCall{Op: "run", ID: a.ID}); err != nil {
			return fail(err)
		}
		return mcpserver.TextResult("queued: the hub starts it on one of the person's machines within a minute", false)
	case ToolRuns:
		out, err := t.call(ctx, hubproto.RoutinesCall{Op: "runs", ID: a.ID, Limit: 20})
		if err != nil {
			return fail(err)
		}
		return pretty(out["runs"])
	case ToolDelete:
		if _, err := t.call(ctx, hubproto.RoutinesCall{Op: "delete", ID: a.ID}); err != nil {
			return fail(err)
		}
		return mcpserver.TextResult("deleted", false)
	}
	return mcpserver.TextResult("unknown tool "+name, true)
}

func saved(what string, out map[string]any) string {
	r, _ := out["routine"].(map[string]any)
	line := fmt.Sprintf("%s: %v (id %v)", what, r["name"], r["id"])
	if next, ok := out["next"].([]any); ok && len(next) > 0 {
		line += fmt.Sprintf(", next start %v", next[0])
	}
	if !truthy(r["enabled"]) {
		line += ", paused"
	}
	if u, ok := out["url"].(string); ok && u != "" {
		line += "\npage: " + u
	}
	return line
}

func truthy(v any) bool { b, _ := v.(bool); return b }
