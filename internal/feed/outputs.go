package feed

import (
	"strings"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/outputs"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// What a turn made that the person opens (wire.TurnOutput) is told by package outputs, from the calls of the turn.

// call is what a tool call says of itself that its result completes: the command it ran, what an Artifact call
// publishes, and the turn it belongs to.
type call struct {
	tool    string
	command string
	publish *jsvalue.Object
	// deliver: the call is the MCP tool that hands a file over.
	deliver bool
	turn    *turn
}

// remember keeps what a call's result will need to tell what it made; only the calls that may make something.
func (b *builder) remember(o *jsvalue.Object, use, tool, summary string) {
	cur := b.cur()
	if cur == nil || use == "" {
		return
	}
	c := call{tool: tool, turn: cur}
	if v, ok := o.Get("publish"); ok {
		c.publish, _ = v.(*jsvalue.Object)
	}
	if tool == "Bash" {
		c.command = strings.TrimPrefix(summary, "$ ")
	}
	c.deliver = outputs.IsDeliverTool(tool)
	if c.publish == nil && c.command == "" && !c.deliver {
		return
	}
	b.calls[use] = c
}

// settle reads what a finished call made off the links of its result.
func (b *builder) settle(use string, o *jsvalue.Object, ok bool) {
	c, known := b.calls[use]
	if !known {
		return
	}
	delete(b.calls, use)
	if !ok {
		return
	}
	links := []string{}
	for _, v := range o.Array("links") {
		if s, isStr := v.(string); isStr {
			links = append(links, s)
		}
	}
	for _, out := range outputs.Of(outputs.FromEnv(), outputs.Call{Tool: c.tool, Command: c.command, Publish: c.publish, Deliver: c.deliver}, links) {
		addOutput(c.turn, out)
	}
}

// ran is a command whose output came with it (Codex): what it made, at once.
func (b *builder) ran(command, stdout string) {
	cur := b.cur()
	if cur == nil || command == "" {
		return
	}
	links := outputs.LinksIn(stdout)
	for _, out := range outputs.Of(outputs.FromEnv(), outputs.Call{Tool: "Bash", Command: command}, links) {
		addOutput(cur, out)
	}
}

// addOutput puts a thing into the turn's outputs once: what is named again keeps its place, and a thing the turn
// made stays made however often it changed it after.
func addOutput(t *turn, out wire.TurnOutput) {
	out.Title = outputs.Clip(out.Title)
	if out.Ref == "" || out.Title == "" {
		return
	}
	for i, have := range t.rec.Outputs {
		if have.Kind == out.Kind && have.Ref == out.Ref {
			if out.Op == wire.OutputCreated {
				t.rec.Outputs[i].Op = out.Op
			}
			if out.Kind == wire.OutputDelivery {
				// A file handed over again after a change is the new bytes: the card vouches for the last hand-over.
				t.rec.Outputs[i].SHA256, t.rec.Outputs[i].Size = out.SHA256, out.Size
			}
			return
		}
	}
	t.rec.Outputs = append(t.rec.Outputs, out)
}
