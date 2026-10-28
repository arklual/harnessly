package convo

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentstate"
	"github.com/ly-solutions/harnessly/internal/attach"
	"github.com/ly-solutions/harnessly/internal/chatguard"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/outbox"
	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// interruptHold: a second Stop within this of the first does not press Escape again.
const interruptHold = 5 * time.Second

// do runs a command of this conversation on its actor. The result names the first seq its effect shows from.
func (c *conv) do(device string, cmd wire.Command, now time.Time) wire.CmdResult {
	c.mark, c.markConvs = 0, 0
	if c.deletedAt != 0 {
		if r, ok := cmd.(wire.ConversationRestoreCmd); ok {
			return c.restore(r, now)
		}
		return wire.Refused(wire.RefuseNotFound)
	}
	switch m := cmd.(type) {
	case wire.SendCmd:
		if c.d.Moves != nil && c.d.Moves.Blocked(c.rec.ID) {
			return wire.Refused(wire.RefuseConflict)
		}
		return c.send(m, now)
	case wire.CancelCmd:
		ch, code := c.box.Cancel(m.ClientID, now)
		return c.result(ch, code, wire.ResultAccepted, now)
	case wire.RetryCmd:
		ch, code := c.box.Retry(m.ClientID, c.fd.FindUser, now)
		r := c.result(ch, code, wire.ResultQueued, now)
		if code == "" {
			c.freshLaunch(now)
		}
		c.pump(now)
		return r
	case wire.PromoteCmd:
		ch, code := c.box.Promote(m.ClientID, m.To, c.rec.State, now)
		r := c.result(ch, code, wire.ResultAccepted, now)
		c.pump(now)
		return r
	case wire.QueueMoveCmd:
		before := ""
		if m.BeforeClientID != nil {
			before = *m.BeforeClientID
		}
		ch, code := c.box.Move(m.ClientID, before, now)
		r := c.result(ch, code, wire.ResultAccepted, now)
		c.pump(now)
		return r
	case wire.QueueResumeCmd:
		if p := c.box.Paused(); p != nil && p.Reason == wire.PausedLimited {
			// The queue held on the usage limit goes on as the limit's «now» does.
			return c.limitCmd(wire.LimitCmd{OpID: m.OpID, ID: c.rec.ID, Action: wire.LimitNow}, now)
		}
		ch, ok := c.box.Resume(now)
		if !ok {
			return wire.Refused(wire.RefuseNotFound)
		}
		r := c.result(ch, "", wire.ResultAccepted, now)
		c.freshLaunch(now)
		c.pump(now)
		return r
	case wire.AnswerCmd:
		return c.answer(device, m, now)
	case wire.DecideCmd:
		return c.decide(device, m, now)
	case wire.InterruptCmd:
		return c.interrupt(m, now)
	case wire.SeenCmd:
		return c.seen(m.UpTo, now)
	case wire.ConversationRenameCmd:
		c.rec.Title, c.rec.TitleSource = strings.TrimSpace(m.Title), wire.TitleFromUser
		return c.recResult(now)
	case wire.ConversationPinCmd:
		c.rec.Pinned = m.Pinned
		return c.recResult(now)
	case wire.ConversationMoveCmd:
		c.rec.ProjectID = m.ProjectID
		return c.recResult(now)
	case wire.ConversationDeleteCmd:
		if c.rec.State.K == wire.StateWorking && (m.Force == nil || !*m.Force) {
			return wire.Refused(wire.RefuseAgentWorking)
		}
		c.deletedAt, c.dirty = ms(now), true
		seq := c.emitConvs(wire.ConversationRemovedEv{ID: c.rec.ID})
		c.closePane()
		return wire.Accepted(wire.ChannelConvs, seq)
	case wire.ConversationRestoreCmd:
		return wire.Refused(wire.RefuseNotFound)
	case wire.ConversationResumeCmd:
		if c.rec.State.K != wire.StateStopped || !c.rec.State.Resumable || c.d.Launcher == nil {
			return wire.Refused(wire.RefuseAgentGone)
		}
		c.resume(now)
		c.flush(now, true)
		return wire.Accepted(wire.ChannelConvs, c.convsSeq())
	case wire.GoalCmd:
		return c.goal(m, now)
	case wire.LimitCmd:
		return c.limitCmd(m, now)
	case wire.ModelCmd, wire.BgStopCmd, wire.TerminalStopCmd, wire.ConversationRetitleCmd:
		// The runtime routes these to model, bgStop and stopTerminal, which answer when the work is done.
		return wire.Refused(wire.RefuseUnsupported)
	}
	return wire.Refused(wire.RefuseUnsupported)
}

// goal sets or clears the /goal of a Claude: «/goal text» (or «/goal clear») goes into the queue as a command message,
// so it is typed when the agent can take it. The goal on the record follows what the transcript says.
func (c *conv) goal(m wire.GoalCmd, now time.Time) wire.CmdResult {
	if c.rec.Provider != wire.ProviderClaude {
		return wire.Refused(wire.RefuseUnsupported)
	}
	line := "/goal clear"
	if text := strings.TrimSpace(m.Text); text != "" {
		line = "/goal " + text
	}
	return c.send(wire.SendCmd{OpID: m.OpID, ClientID: m.OpID, ConversationID: c.rec.ID, Text: line,
		AttachmentIDs: []string{}, Mode: wire.ModeQueue, Kind: wire.KindCommand}, now)
}

// result is a box command's answer: its refusal, or kind on the conv channel from the first event it made.
func (c *conv) result(ch outbox.Change, code wire.RefuseCode, kind wire.ResultKind, now time.Time) wire.CmdResult {
	if code != "" {
		return wire.Refused(code)
	}
	c.change(ch, now)
	r := wire.CmdResult{R: kind, Ch: wire.ConvChannel(c.rec.ID), Seq: c.convSeq()}
	return r
}

func (c *conv) convSeq() int64 {
	if c.mark != 0 {
		return c.mark
	}
	return c.seq
}

func (c *conv) convsSeq() int64 {
	if c.markConvs != 0 {
		return c.markConvs
	}
	return c.seqConvs
}

// recResult answers a change of the record: its upsert goes now, the page waits for it on convs.
func (c *conv) recResult(now time.Time) wire.CmdResult {
	c.touch()
	c.flush(now, true)
	rev := c.rec.Rev
	r := wire.Accepted(wire.ChannelConvs, c.convsSeq())
	r.Rev = &rev
	return r
}

func (c *conv) restore(_ wire.ConversationRestoreCmd, now time.Time) wire.CmdResult {
	if c.deletedAt == 0 || now.Sub(time.UnixMilli(c.deletedAt)) > DeletedKept {
		return wire.Refused(wire.RefuseNotFound)
	}
	c.deletedAt = 0
	return c.recResult(now)
}

func (c *conv) seen(upTo wire.Ms, now time.Time) wire.CmdResult {
	if upTo > c.rec.SeenAt {
		c.rec.SeenAt = upTo
		c.rec.Rev++
		c.dirty = true
		c.emitConvs(wire.ConversationSeenEv{ID: c.rec.ID, SeenAt: c.rec.SeenAt, Rev: c.rec.Rev})
	}
	return wire.Accepted(wire.ChannelConvs, c.convsSeq())
}

// terminalOff: the machine's policy keeps the terminal off for the hub.
func (c *conv) terminalOff() bool { return c.d.TerminalOff != nil && c.d.TerminalOff() }

func (c *conv) send(m wire.SendCmd, now time.Time) wire.CmdResult {
	if c.terminalOff() && chatguard.BashMode(m.Text) {
		// A bash-mode line is a shell command to the agent, run without asking: the terminal's work, not the chat's.
		return wire.Refused(wire.RefuseForbidden)
	}
	d := outbox.Draft{ClientID: m.ClientID, Text: m.Text, Mode: m.Mode, Kind: m.Kind, Attachments: []wire.AttachmentRef{}}
	if len(m.AttachmentIDs) > 0 {
		if c.d.Uploads == nil {
			return wire.Refused(wire.RefuseAttachmentMissing)
		}
		refs, images, code := c.d.Uploads.Resolve(m.AttachmentIDs)
		if code != "" {
			return wire.Refused(code)
		}
		d.Attachments, d.Images = refs, images
	}
	if len(m.Refs) > 0 {
		block, code := c.refsBlock(m.Refs)
		if code != "" {
			return wire.Refused(code)
		}
		d.Text, d.Refs = attach.With(m.Text, block), slices.Clone(m.Refs)
	}
	ch, code := c.box.Send(d, c.rec.State, now)
	r := c.result(ch, code, wire.ResultQueued, now)
	if code == "" {
		// The person wrote: the chat rises in the sidebar now, not when the transcript shows the line.
		c.promptedAt(ms(now))
		c.freshLaunch(now)
		c.pump(now)
	}
	if c.lostTmux() {
		// The message waits in the paused queue; a look that finds the pane brings the agent and the queue back.
		c.observe(now)
	}
	return r
}

// refsBlock is the block of the places a message points at, as the agent reads it under the person's words. A design is
// named by the machine's library (Deps.Refs), which also notes this conversation among the design's chats.
func (c *conv) refsBlock(refs []wire.MessageRef) (string, wire.RefuseCode) {
	items := make([]attach.Item, 0, len(refs))
	for _, r := range refs {
		line, body := attach.LineOf(r), ""
		if r.Kind == wire.RefChat {
			var code wire.RefuseCode
			if line, body, code = c.chatRef(r); code != "" {
				return "", code
			}
		}
		if r.Kind == wire.RefDesign {
			if c.d.Refs == nil {
				return "", wire.RefuseUnsupported
			}
			var code wire.RefuseCode
			if line, code = c.d.Refs.Line(c.rec.ID, r); code != "" {
				return "", code
			}
		}
		items = append(items, attach.Item{Ref: r, Line: line, Body: body})
	}
	return attach.Block(items), ""
}

// ChatRefBudget is how many characters of another chat a message carries under its words (about 6 000 tokens).
const ChatRefBudget = 18000

// chatRef is the line and the excerpt of another chat the message points at: its title, the first request, the latest
// messages, and the path of the whole conversation as Markdown.
func (c *conv) chatRef(r wire.MessageRef) (line, body string, code wire.RefuseCode) {
	if c.d.Digest == nil {
		return "", "", wire.RefuseUnsupported
	}
	if r.ID == c.rec.ID {
		return "", "", wire.RefuseBadRequest
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	d, err := c.d.Digest(ctx, r.ID, ChatRefBudget)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", "", wire.RefuseNotFound
		}
		c.d.Log.Warn("chat reference not read", "conv", c.rec.ID, "ref", r.ID, "err", err)
		return "", "", wire.RefuseInternal
	}
	line = attach.LineOf(wire.MessageRef{Kind: wire.RefChat, Title: d.Title}) + locale.Pick(
		". An excerpt follows; the whole conversation is the Markdown file ", ". Ниже выдержка; весь разговор — Markdown-файл ") + attach.OneLine(d.Path)
	return line, d.Text, ""
}

// --- gates ---

func (c *conv) gateFor(id string) (*gate, wire.RefuseCode) {
	g := c.gates[id]
	if g == nil {
		return nil, wire.RefuseGateClosed
	}
	switch g.state().K {
	case wire.GateStateClosed:
		return nil, wire.RefuseGateClosed
	case wire.GateStateAnswering, wire.GateStateAnswered:
		return nil, wire.RefuseGateAnswered
	}
	return g, ""
}

func (c *conv) answering(g *gate, opID, device string, now time.Time) {
	at := ms(now)
	c.setGateState(g, wire.GateState{K: wire.GateStateAnswering, OpID: opID, ByDevice: device, At: at,
		Wait: &wire.Wait{SlowAt: at + wire.WaitSlowMs, DeadlineAt: at + wire.AnswerDeadline}}, now)
}

// keys runs a dialog's keys off the actor under the pane lock; a failure opens the gate again.
func (c *conv) keys(g *gate, work func(p typist.Pane) error, ok func(now time.Time)) {
	pane := c.d.Machine.Pane(c.rec.Session)
	if pane == nil {
		c.setGateState(g, wire.GateState{K: wire.GateStateOpen}, c.d.Clock.Now())
		return
	}
	c.busyGate = true
	session := c.rec.Session
	c.spawn(func() any {
		unlock := typist.Lock(session)
		defer unlock()
		return work(pane)
	}, func(r any) {
		c.busyGate = false
		now := c.d.Clock.Now()
		if err, _ := r.(error); err != nil {
			c.d.Log.Warn("gate keys failed", "conv", c.rec.ID, "gate", g.id(), "err", err)
			switch {
			case errors.Is(err, typist.ErrMenuChanged):
				// The menu on the screen is not the one decided on: no key went in. When the agent asks about another
				// call now, this gate is stale and the look that follows opens the screen's own; when it is the same
				// call, the gate opens again for the page to decide anew.
				c.read(now)
				if e, ok := c.fd.PendingTool(); ok && wire.PermissionID(e.ToolUseID) != g.p.ID {
					c.closeGate(g.id(), wire.ClosedAgentMovedOn, now)
					c.input(agentstate.Input{K: agentstate.GateResolved}, now)
					c.observe(now)
				} else if g.state().K == wire.GateStateAnswering {
					// The same call still asks: the person decides in the terminal, and the page says so.
					refused := wire.GateRefusedMenuUnmatched
					c.setGateState(g, wire.GateState{K: wire.GateStateOpen, Refused: &refused}, now)
				}
			case g.state().K == wire.GateStateAnswering:
				c.setGateState(g, wire.GateState{K: wire.GateStateOpen}, now)
			}
		} else if ok != nil {
			ok(now)
		}
		c.read(now)
		c.pump(now)
	})
}

func (c *conv) answer(device string, m wire.AnswerCmd, now time.Time) wire.CmdResult {
	g, code := c.gateFor(m.QuestionID)
	if code != "" {
		return wire.Refused(code)
	}
	if g.q == nil {
		return wire.Refused(wire.RefuseGateClosed)
	}
	if m.ChatAbout {
		c.answering(g, m.OpID, device, now)
		clock := c.d.Clock
		c.keys(g, func(p typist.Pane) error {
			if !typist.ChatAbout(p, clock) {
				return typist.ErrDialogStayed
			}
			return nil
		}, func(now time.Time) {
			c.setGateState(g, wire.GateState{K: wire.GateStateClosed, At: ms(now), Reason: wire.ClosedChatAbout}, now)
		})
		return wire.Accepted(wire.ConvChannel(c.rec.ID), c.convSeq())
	}
	shapes, choices, code := answerKeys(g.q.Items, m.Answers)
	if code != "" {
		return wire.Refused(code)
	}
	c.answering(g, m.OpID, device, now)
	clock := c.d.Clock
	c.keys(g, func(p typist.Pane) error { return typist.Answer(p, clock, shapes, choices) }, nil)
	return wire.Accepted(wire.ConvChannel(c.rec.ID), c.convSeq())
}

// answerKeys turns the page's answers into the dialog's shapes and choices; option-unknown for a choice or an item
// the question does not have.
func answerKeys(items []wire.QuestionItem, answers []wire.QuestionAnswer) ([]screen.QuestionShape, []screen.QuestionChoice, wire.RefuseCode) {
	if len(answers) != len(items) {
		return nil, nil, wire.RefuseBadRequest
	}
	shapes := make([]screen.QuestionShape, len(items))
	choices := make([]screen.QuestionChoice, len(items))
	seen := map[int]bool{}
	for _, a := range answers {
		if a.I < 0 || a.I >= len(items) || seen[a.I] {
			return nil, nil, wire.RefuseOptionUnknown
		}
		seen[a.I] = true
		item := items[a.I]
		for _, ch := range a.Choices {
			if ch < 0 || ch >= len(item.Options) {
				return nil, nil, wire.RefuseOptionUnknown
			}
		}
		choice := screen.QuestionChoice{Picked: append([]int{}, a.Choices...)}
		if a.Text != nil {
			choice.Other = *a.Text
		}
		choices[a.I] = choice
	}
	for i, item := range items {
		s := screen.QuestionShape{OptionCount: len(item.Options), MultiSelect: item.Multi}
		for _, o := range item.Options {
			if o.Preview != nil {
				s.Preview = true
			}
		}
		shapes[i] = s
	}
	if screen.ValidateAnswerRequest(shapes, choices) != nil {
		return nil, nil, wire.RefuseOptionUnknown
	}
	return shapes, choices, ""
}

func (c *conv) decide(device string, m wire.DecideCmd, now time.Time) wire.CmdResult {
	g, code := c.gateFor(m.PermissionID)
	if code != "" {
		return wire.Refused(code)
	}
	if g.p == nil {
		return wire.Refused(wire.RefuseGateClosed)
	}
	want := typist.PickWant{Plan: g.p.Kind == wire.PermPlan, Cwd: c.rec.Cwd}
	if g.p.Command != nil {
		want.Command = *g.p.Command
	}
	for i, o := range g.p.Options {
		if o.ID == m.OptionID {
			want.N, want.Label = i+1, o.Label
			if k, err := strconv.Atoi(o.ID); err == nil {
				want.N = k
			}
		}
	}
	if want.N == 0 {
		return wire.Refused(wire.RefuseOptionUnknown)
	}
	c.answering(g, m.OpID, device, now)
	clock, provider := c.d.Clock, c.rec.Provider
	c.keys(g, func(p typist.Pane) error { return typist.Pick(p, clock, provider, want) }, nil)
	return wire.Accepted(wire.ConvChannel(c.rec.ID), c.convSeq())
}

// permissionOptions reads a menu's options ("1. Yes", "2. Yes, and don't ask again …", "3. No …") into the
// permission's, the scope by their words.
func permissionOptions(lines []string, plan bool) []wire.PermissionOption {
	out := []wire.PermissionOption{}
	for i, line := range lines {
		id, label := strconv.Itoa(i+1), typist.OptionLabel(line)
		if num, _, ok := strings.Cut(strings.TrimSpace(line), ". "); ok {
			if _, err := strconv.Atoi(num); err == nil {
				id = num
			}
		}
		out = append(out, wire.PermissionOption{ID: id, Label: label, Scope: scopeOf(label, plan)})
	}
	return out
}

func scopeOf(label string, plan bool) wire.PermissionScope {
	l := strings.ToLower(label)
	switch {
	case plan && strings.HasPrefix(l, "yes") && (strings.Contains(l, "auto") || strings.Contains(l, "bypass")):
		return wire.ScopePlanAuto
	case plan && strings.HasPrefix(l, "yes"):
		return wire.ScopePlanAsk
	case plan:
		return wire.ScopeRevise
	case strings.HasPrefix(l, "no"):
		return wire.ScopeDeny
	case strings.Contains(l, "don't ask again") || strings.Contains(l, "always") || strings.Contains(l, "all "):
		return wire.ScopeAlways
	}
	return wire.ScopeOnce
}

// --- stop ---

// interrupt presses Escape in the agent's pane. Nothing on the screen stands in its way, the box's text least of all:
// a Stop is the person's, and the state on record may be behind the screen (a turn taken for ended while the agent
// waits on its model with nothing drawn, a short pane that hid the spinner). Only an agent that is not running, or a
// second Stop on the heels of the first, is refused — and the refusal is logged.
func (c *conv) interrupt(m wire.InterruptCmd, now time.Time) wire.CmdResult {
	working := false
	switch c.rec.State.K {
	case wire.StateWorking, wire.StateWaitingQuestion, wire.StateWaitingPermission:
		working = true
	case wire.StateIdle, wire.StateReady, wire.StateUnknown:
	default:
		return c.refuseInterrupt(wire.RefuseNotWorking, "agent not running")
	}
	if !c.interruptAt.IsZero() && now.Sub(c.interruptAt) < interruptHold {
		return c.refuseInterrupt(wire.RefuseNotWorking, "stopped a moment ago")
	}
	pane := c.d.Machine.Pane(c.rec.Session)
	if pane == nil {
		return c.refuseInterrupt(wire.RefuseAgentGone, "no pane")
	}
	c.interruptAt = now
	if !working {
		c.d.Log.Info("interrupt of an agent not on record as working", "conv", c.rec.ID, "state", string(c.rec.State.K))
	}
	// The queue waits for the interrupt to end the turn; a turn not on record ends nothing, so its queue is not held.
	if m.Pauses() && working {
		c.interrupting = true
		c.change(c.box.Pause(wire.PausedInterrupted, now), now)
	}
	session, clock := c.rec.Session, c.d.Clock
	c.spawn(func() any {
		unlock := typist.Lock(session)
		defer unlock()
		return typist.Interrupt(pane, clock)
	}, func(r any) {
		if err, _ := r.(error); err != nil {
			c.d.Log.Warn("interrupt failed", "conv", c.rec.ID, "err", err)
			c.interruptAt = time.Time{}
		}
		c.read(c.d.Clock.Now())
	})
	if c.mark == 0 {
		// Nothing to show yet: the effect is the turn's end, seen from the next event on.
		return wire.Accepted(wire.ConvChannel(c.rec.ID), c.seq+1)
	}
	return wire.Accepted(wire.ConvChannel(c.rec.ID), c.mark)
}

func (c *conv) refuseInterrupt(code wire.RefuseCode, why string) wire.CmdResult {
	c.d.Log.Warn("interrupt refused", "conv", c.rec.ID, "state", string(c.rec.State.K), "code", string(code), "why", why)
	return wire.Refused(code)
}
