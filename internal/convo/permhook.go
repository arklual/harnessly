package convo

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentstate"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Claude's PermissionRequest hook fires the moment a tool asks, with the call itself. The hook waits here for the
// person's decision and carries it back, so the card needs no menu on any screen and no key is pressed. Claude's dialog
// stands in the terminal meanwhile: an answer there closes the card, and a hook that goes leaves the dialog to answer.

const (
	planTool     = "ExitPlanMode"
	questionTool = "AskUserQuestion"
)

// PermissionAsk is what a hook asks: the tool, its input as the agent gave it, Claude's suggested rules for «don't ask
// again» (nil when there are none), the folder.
type PermissionAsk struct {
	Tool        string
	Input       json.RawMessage
	Suggestions json.RawMessage
	Cwd         string
}

// asksThePerson: a card can stand for it. A question has its own card, by the transcript.
func (a PermissionAsk) asksThePerson() bool { return a.Tool != "" && a.Tool != questionTool }

// PermissionReply is the `decision` of the hook's output.
type PermissionReply struct {
	Behavior           string          `json:"behavior"`
	UpdatedInput       json.RawMessage `json:"updatedInput,omitempty"`
	UpdatedPermissions json.RawMessage `json:"updatedPermissions,omitempty"`
	Message            string          `json:"message,omitempty"`
	Interrupt          bool            `json:"interrupt,omitempty"`
}

// setMode is the rule that puts the session in a permission mode once a plan is approved.
func setMode(mode string) json.RawMessage {
	return json.RawMessage(`[{"type":"setMode","mode":"` + mode + `","destination":"session"}]`)
}

// replyOf is the decision an option of the card stands for.
func replyOf(ask PermissionAsk, scope wire.PermissionScope) PermissionReply {
	switch scope {
	case wire.ScopeOnce:
		return PermissionReply{Behavior: "allow"}
	case wire.ScopeAlways:
		return PermissionReply{Behavior: "allow", UpdatedPermissions: ask.Suggestions}
	case wire.ScopePlanAsk:
		// Claude takes a plan's approval only with the plan in it.
		return PermissionReply{Behavior: "allow", UpdatedInput: ask.Input, UpdatedPermissions: setMode("default")}
	case wire.ScopePlanAuto:
		return PermissionReply{Behavior: "allow", UpdatedInput: ask.Input, UpdatedPermissions: setMode("acceptEdits")}
	case wire.ScopeRevise:
		return PermissionReply{Behavior: "deny", Interrupt: true, Message: "The person wants the plan changed and will say how. Wait for them."}
	}
	return PermissionReply{Behavior: "deny", Interrupt: true, Message: "The person declined this."}
}

func hookOptions(ask PermissionAsk) []wire.PermissionOption {
	if ask.Tool == planTool {
		return []wire.PermissionOption{
			{ID: "1", Label: locale.Pick("Yes, edit without asking", "Да, править без вопросов"), Scope: wire.ScopePlanAuto},
			{ID: "2", Label: locale.Pick("Yes, ask before edits", "Да, спрашивать перед правками"), Scope: wire.ScopePlanAsk},
			{ID: "3", Label: locale.Pick("Change the plan", "Изменить план"), Scope: wire.ScopeRevise},
		}
	}
	out := []wire.PermissionOption{{ID: "1", Label: locale.Pick("Yes", "Да"), Scope: wire.ScopeOnce}}
	if len(ask.Suggestions) > 0 {
		out = append(out, wire.PermissionOption{ID: "2", Label: locale.Pick("Yes, and don't ask again", "Да, больше не спрашивать"), Scope: wire.ScopeAlways})
	}
	return append(out, wire.PermissionOption{ID: itoa(int64(len(out) + 1)), Label: locale.Pick("No", "Нет"), Scope: wire.ScopeDeny})
}

// summaryOfInput is the line the card shows for a call: a shell command as «$ …», else the path or address it is about.
func summaryOfInput(input json.RawMessage) string {
	var f map[string]any
	if json.Unmarshal(input, &f) != nil {
		return ""
	}
	if cmd, ok := f["command"].(string); ok && strings.TrimSpace(cmd) != "" {
		return "$ " + cmd
	}
	for _, key := range []string{"file_path", "notebook_path", "path", "url", "pattern", "query"} {
		if s, ok := f[key].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// planOfInput is the plan text of an ExitPlanMode call.
func planOfInput(input json.RawMessage) string {
	var f struct {
		Plan string `json:"plan"`
	}
	_ = json.Unmarshal(input, &f)
	return f.Plan
}

// askPermission opens the card, or takes over the one the screen opened for this same call.
func (c *conv) askPermission(ask PermissionAsk, reply chan PermissionReply, now time.Time) {
	c.read(now)
	c.leaveDialog(now)
	plan := ask.Tool == planTool
	summary := summaryOfInput(ask.Input)
	if g := c.openGate(c.rec.State.PermissionID); g != nil && g.p != nil && g.reply == nil && (g.p.Kind == wire.PermPlan) == plan &&
		g.state().K == wire.GateStateOpen && (plan || g.p.Command == nil || *g.p.Command == summary) {
		g.reply, g.ask = reply, ask
		g.p.Options = hookOptions(ask)
		if plan {
			md := planOfInput(ask.Input)
			g.p.PlanMD = &md
		}
		c.setGateState(g, wire.GateState{K: wire.GateStateOpen}, now)
		return
	}
	p := wire.Permission{ID: c.screenGateID(), ConversationID: c.rec.ID, Kind: wire.PermTool, AskedAt: ms(now),
		Options: hookOptions(ask), State: wire.GateState{K: wire.GateStateOpen}}
	in := agentstate.PermissionPrompt
	if plan {
		md := planOfInput(ask.Input)
		p.Kind, p.PlanMD, in = wire.PermPlan, &md, agentstate.PlanProposed
	} else {
		tool := ask.Tool
		p.Tool = &tool
		if summary != "" {
			p.Command = &summary
		}
		if ask.Cwd != "" {
			cwd := ask.Cwd
			p.Cwd = &cwd
		}
		if e, ok := c.fd.PendingTool(); ok && e.Tool == ask.Tool && c.gates[wire.PermissionID(e.ToolUseID)] == nil {
			p.ID = wire.PermissionID(e.ToolUseID)
		}
	}
	c.placeScreenGate(&p)
	g := &gate{p: &p, reply: reply, ask: ask}
	if !c.addGate(g) {
		close(reply)
		return
	}
	if c.rec.State.PermissionID == "" {
		c.input(agentstate.Input{K: in, GateID: p.ID}, now)
	}
	if c.rec.State.PermissionID == p.ID {
		return
	}
	// Another card stands (this one takes over when that ends, resumeHookGates), or the chat cannot wait from where it
	// is: the person still gets the card.
	if plan {
		c.emit(wire.PlanProposedEv{Permission: p})
	} else {
		c.emit(wire.PermissionRequestedEv{Permission: p})
	}
}

// answerHook carries the person's decision to the hook. The gate is answered at once: the agent has the answer.
func (c *conv) answerHook(g *gate, m wire.DecideCmd, device string, now time.Time) {
	scope := wire.ScopeDeny
	for _, o := range g.p.Options {
		if o.ID == m.OptionID {
			scope = o.Scope
		}
	}
	g.reply <- replyOf(g.ask, scope)
	close(g.reply)
	g.reply = nil
	c.setGateState(g, wire.GateState{K: wire.GateStateAnswered, OpID: m.OpID, At: ms(now), ByDevice: device}, now)
	if c.rec.State.PermissionID == g.id() {
		c.input(agentstate.Input{K: agentstate.GateResolved}, now)
	}
	c.resumeHookGates(now)
}

// resumeHookGates: another hook still waits on the person, and the chat waits with it.
func (c *conv) resumeHookGates(now time.Time) {
	if c.rec.State.PermissionID != "" {
		return
	}
	for _, id := range c.order {
		g := c.gates[id]
		if g == nil || g.reply == nil || g.p == nil || g.state().K != wire.GateStateOpen {
			continue
		}
		in := agentstate.PermissionPrompt
		if g.p.Kind == wire.PermPlan {
			in = agentstate.PlanProposed
		}
		c.input(agentstate.Input{K: in, GateID: id}, now)
		return
	}
}

// hookGone: the hook stopped waiting (the agent cancelled it or ended). If its dialog is still up, the card answers
// there now, with the options the screen shows.
func (c *conv) hookGone(reply chan PermissionReply, now time.Time) {
	for _, id := range c.order {
		g := c.gates[id]
		if g == nil || g.reply != reply {
			continue
		}
		g.reply = nil
		g.p.Options = []wire.PermissionOption{}
		c.dirty = true
		c.observe(now)
		return
	}
}
