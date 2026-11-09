package convo

import (
	"errors"
	"regexp"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agentstate"
	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// modelIDRe is a model id as it may be typed into the agent's box: an alias, a full id, the [1m] suffix.
var modelIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}(\[1m\])?$`)

// model switches the model of a Claude: «/model id» typed into its box, then «/effort level» when the effort changes
// too, then «/effort ultracode on|off» when its ultracode mode does. Ultracode counts as a change of model here: it is
// applied at the same first safe moment. It never refuses for the agent's state and never makes the person wait for it:
//
//   - an agent at its prompt takes the change at once (the typing runs off the actor under the pane lock, as every
//     writer of a pane does) and the answer comes when it confirmed, or did not;
//   - an agent that is not started (stopped, gone, never launched, still starting) or at work (a turn, a question, a
//     permission, a message on its way) leaves the change queued on the conversation as pendingModel and answers at
//     once: the server applies it at the first moment the agent is at its prompt, before the next queued message is
//     typed (see applyPending). A change asked again replaces the one waiting.
//
// Codex has no command for it in its box that a page could rely on, so its conversations are refused unsupported.
func (c *conv) model(m wire.ModelCmd, finish func(wire.CmdResult), now time.Time) {
	c.mark, c.markConvs = 0, 0
	if c.deletedAt != 0 {
		finish(wire.Refused(wire.RefuseNotFound))
		return
	}
	if c.rec.Provider != wire.ProviderClaude {
		finish(wire.Refused(wire.RefuseUnsupported))
		return
	}
	// A conversation on a vendor profile names the profile's models, which may be «vendor/model»; modelFits then
	// holds them to the profile's own list.
	onProfile := c.rec.Model != nil && c.rec.Model.Profile != ""
	if !modelIDRe.MatchString(m.Model) && !(onProfile && agents.IsProfileModelID(m.Model)) ||
		m.Permission != nil && !m.Permission.Known() {
		finish(wire.Refused(wire.RefuseBadRequest))
		return
	}
	if code := c.modelFits(m); code != "" {
		finish(wire.Refused(code))
		return
	}
	diff := c.modelDiff(m)
	if !diff.any() {
		// Back to what the agent runs: whatever waited is no longer wanted.
		c.dropPending()
		finish(c.recResult(now))
		return
	}
	if !c.canSwitchNow() {
		c.queueModel(m, now)
		finish(c.recResult(now))
		c.pump(now)
		return
	}
	c.dropPending()
	c.typeModel(m, diff, func(err error, now time.Time) {
		if err != nil {
			finish(wire.Refused(modelRefusal(err)))
			return
		}
		finish(c.recResult(now))
	})
}

// modelDiff is which of the commands a change needs, against the model on record.
type modelDiff struct {
	model, effort, ultracode, fast, permission bool
	// ultracodeOn, fastOn and permissionTo are the modes the change asks for (whether they are to be typed or not).
	ultracodeOn, fastOn bool
	permissionTo        wire.PermissionMode
}

func (d modelDiff) any() bool { return d.model || d.effort || d.ultracode || d.fast || d.permission }

// modelDiff says which of the commands the change needs, against the model on record. A model switch leaves the
// ultracode mode as it was in Claude Code: it is typed only when the ask turns it the other way.
func (c *conv) modelDiff(m wire.ModelCmd) modelDiff {
	curModel, curEffort, curUltra := "", (*wire.Effort)(nil), false
	if cur := c.rec.Model; cur != nil {
		curModel, curEffort, curUltra = cur.ID, cur.Effort, cur.Ultracode
	}
	d := modelDiff{model: m.Model != curModel, ultracodeOn: m.UltracodeAfter(c.rec.Model),
		fastOn: m.FastAfter(c.rec.Model), permissionTo: m.PermissionAfter(c.rec.Model)}
	d.effort = m.Effort != nil && (d.model || curEffort == nil || *curEffort != *m.Effort)
	d.ultracode = d.ultracodeOn != curUltra
	// Fast mode and the permission mode are typed when the ask turns them; a permission mode of "" is the
	// machine's default and is never typed.
	curFast, curPermission := false, wire.PermissionMode("")
	if cur := c.rec.Model; cur != nil {
		curFast, curPermission = cur.Fast, cur.Permission
	}
	d.fast = d.fastOn != curFast
	d.permission = d.permissionTo != "" && d.permissionTo != curPermission
	return d
}

// canSwitchNow: the agent is at its prompt and nothing else is being typed into it or on its way.
func (c *conv) canSwitchNow() bool {
	switch c.rec.State.K {
	case wire.StateReady, wire.StateIdle:
	default:
		return false
	}
	return !c.typing && !c.switching && !c.busyGate && c.box.InFlight() == "" && c.d.Machine.Pane(c.rec.Session) != nil
}

// queueModel keeps the change until the agent can take it.
func (c *conv) queueModel(m wire.ModelCmd, now time.Time) {
	c.rec.PendingModel = &wire.PendingModel{ID: m.Model, Effort: m.Effort, Ultracode: m.UltracodeAfter(c.rec.Model),
		Fast: m.FastAfter(c.rec.Model), Permission: m.PermissionAfter(c.rec.Model), Since: ms(now)}
	c.pendingTries, c.pendingAt = 0, time.Time{}
	c.touch()
}

// dropPending forgets the waiting change.
func (c *conv) dropPending() {
	if c.rec.PendingModel == nil {
		return
	}
	c.rec.PendingModel = nil
	c.pendingTries, c.pendingAt = 0, time.Time{}
	c.touch()
}

// pendingRetries: a change the agent's box refused this many times (a screen that is not at its prompt, text in the
// box) is dropped; the record then shows the model it still runs on.
const pendingRetries = 12

// applyPending takes the waiting change in hand once the agent is at its prompt. It reports whether it has the pane:
// the caller then types nothing, and the queue goes on when the change is done. It is called before every try at the
// queue, so a change asked during a turn is applied before the next queued message — and, for an agent that was not
// started, right after its launch and before its first message.
func (c *conv) applyPending(now time.Time) bool {
	p := c.rec.PendingModel
	if p == nil || c.deletedAt != 0 {
		return false
	}
	if c.switching {
		return true
	}
	if !c.pendingAt.IsZero() && now.Before(c.pendingAt) {
		return false
	}
	if !c.canSwitchNow() {
		return false
	}
	ultra, fast := p.Ultracode, p.Fast
	m := wire.ModelCmd{ID: c.rec.ID, Model: p.ID, Effort: p.Effort, Ultracode: &ultra, Fast: &fast}
	if p.Permission != "" {
		permission := p.Permission
		m.Permission = &permission
	}
	diff := c.modelDiff(m)
	if !diff.any() {
		c.dropPending()
		return false
	}
	if !p.Applying {
		applying := *p
		applying.Applying = true
		c.rec.PendingModel = &applying
		c.touch()
	}
	c.typeModel(m, diff, func(err error, now time.Time) {
		switch {
		case err == nil:
		case errors.Is(err, typist.ErrNotAtPrompt), errors.Is(err, typist.ErrInputOccupied):
			// The screen was not what the state said: it is looked at again soon, a few times.
			c.pendingTries++
			if c.pendingTries < pendingRetries {
				again := *p
				again.Applying = false
				c.rec.PendingModel = &again
				c.pendingAt = now.Add(min(time.Second<<min(c.pendingTries, 3), 10*time.Second))
				c.touch()
				return
			}
			c.refusedPending(*p, err, now)
		default:
			c.refusedPending(*p, err, now)
		}
	})
	return true
}

// refusedPending forgets a waiting change the agent did not take, and says so in the log the feed draws: nobody waits
// on a command's answer for it, and the pill falls back to the model on record without a word otherwise.
func (c *conv) refusedPending(p wire.PendingModel, err error, now time.Time) {
	c.dropPending()
	code := modelRefusal(err)
	to := wire.Model{ID: p.ID, Effort: p.Effort, Ultracode: p.Ultracode, Fast: p.Fast, Permission: p.Permission}
	if c.rec.Model != nil {
		to.Profile = c.rec.Model.Profile
	}
	c.logSwitch(wire.ModelSwitch{At: ms(now), From: c.rec.Model, To: to, Refused: &code})
}

// typeModel types the change into the agent's box off the actor and gives the outcome to done, on the actor, once the
// record says what is true. Whatever the outcome, the queue goes on after it.
func (c *conv) typeModel(m wire.ModelCmd, diff modelDiff, done func(err error, now time.Time)) {
	pane := c.d.Machine.Pane(c.rec.Session)
	if pane == nil {
		done(typist.ErrNotAtPrompt, c.d.Clock.Now())
		return
	}
	session, clock := c.rec.Session, c.d.Clock
	// A permission mode shift+tab cannot reach truthfully is the restart's, after what is typed (restartInMode).
	restart := diff.permission && c.permissionNeedsRestart(diff.permissionTo)
	if restart {
		if !c.canRestart() {
			done(errNoRestart, c.d.Clock.Now())
			return
		}
		diff.permission = false
	}
	c.switching = true
	from := c.rec.Model
	c.spawn(func() any {
		unlock := typist.Lock(session)
		defer unlock()
		// Each step that took is kept: a model switched and an effort or a mode refused leaves the model switched, and
		// the record says what is true.
		var took modelTook
		if diff.model {
			if err := typist.SetModel(pane, clock, m.Model); err != nil {
				return took.failed(err)
			}
			took.model = true
		}
		if diff.effort {
			if err := typist.SetEffort(pane, clock, *m.Effort); err != nil {
				return took.failed(err)
			}
			took.effort = true
		}
		if diff.ultracode {
			if err := typist.SetUltracode(pane, clock, diff.ultracodeOn); err != nil {
				return took.failed(err)
			}
			took.ultracode = true
		}
		if diff.fast {
			if err := typist.SetFast(pane, clock, diff.fastOn); err != nil {
				return took.failed(err)
			}
			took.fast = true
		}
		if diff.permission {
			if err := typist.SetPermissionMode(pane, clock, claudeMode[diff.permissionTo]); err != nil {
				return took.failed(err)
			}
			took.permission = true
		}
		return took
	}, func(r any) {
		c.switching = false
		now := c.d.Clock.Now()
		c.mark, c.markConvs = 0, 0 // the answer names the first seq of what this change publishes
		took, _ := r.(modelTook)
		if to := took.on(from, m, diff); to != nil {
			c.applied(to, now)
			if k, ok := c.d.Launcher.(ModeKeeper); ok && (took.fast || took.permission) {
				k.KeepModes(c.rec.Session, to.Fast && to.Profile == "", to.Permission)
			}
		}
		err := took.err
		if err != nil {
			c.d.Log.Warn("model not switched", "conv", c.rec.ID, "err", err)
		}
		if err == nil && restart {
			c.restartInMode(diff.permissionTo, func(err error, now time.Time) {
				if err != nil {
					c.d.Log.Warn("permission mode not switched", "conv", c.rec.ID, "err", err)
				}
				done(err, now)
				c.read(now)
				c.pump(now)
			})
			return
		}
		done(err, now)
		c.read(now)
		c.pump(now)
	})
}

// errNoRestart: the chat's agent cannot be started again here (no terminals or launcher of the runtime's own, no session
// to resume on).
var errNoRestart = errors.New("convo: the agent cannot be restarted in another permission mode")

// permissionNeedsRestart: shift+tab cannot turn the running Claude to the mode truthfully. Its cycle has bypass only in
// a Claude started with it, and in one started with it the other modes do not ask («Session was started with bypass
// permissions, so plan mode won't ask…», Claude Code 2.1.319). Plan and ask go round from one to the other; anything
// with bypass on either side, or a chat that never said its mode (started on the machine's default), is restarted.
func (c *conv) permissionNeedsRestart(to wire.PermissionMode) bool {
	cur := wire.PermissionMode("")
	if c.rec.Model != nil {
		cur = c.rec.Model.Permission
	}
	return cur == "" || cur == wire.PermissionBypass || to == wire.PermissionBypass
}

// canRestart: the runtime can stop the chat's terminal and resume its agent on its session.
func (c *conv) canRestart() bool {
	return c.d.Terminals != nil && c.d.Launcher != nil && c.rec.Session != "" &&
		(c.rec.AgentSessionID != "" || c.rec.TranscriptPath != "")
}

// restartInMode starts the chat's agent again in the permission mode: its terminal is stopped the way an idle
// one is, under the pane's lock, the record takes the mode, and the agent is resumed on its session at once, with the
// mode on its launch line (the tab's launch keeps it, Launcher.Resume). The queue waits meanwhile, as for a switch.
func (c *conv) restartInMode(to wire.PermissionMode, done func(err error, now time.Time)) {
	if !c.canRestart() {
		done(errNoRestart, c.d.Clock.Now())
		return
	}
	session, terms := c.rec.Session, c.d.Terminals
	c.switching = true
	c.spawn(func() any {
		if !terms.Owned(session) {
			return errNotOwned
		}
		unlock := typist.Lock(session)
		defer unlock()
		_, err := terms.Stop(session)
		return err
	}, func(r any) {
		c.switching = false
		now := c.d.Clock.Now()
		if err, _ := r.(error); err != nil {
			done(err, now)
			return
		}
		c.mark, c.markConvs = 0, 0
		c.lookSeq++
		c.looking, c.lookAgain = false, false
		c.input(agentstate.Input{K: agentstate.Shell, Resumable: true}, now)
		next := wire.Model{}
		if c.rec.Model != nil {
			next = *c.rec.Model
		}
		next.Permission = to
		c.applied(&next, now)
		c.resume(now)
		done(nil, now)
	})
}

// applied puts the model the agent took on the record and in the log the feed draws; a waiting change is done.
func (c *conv) applied(to *wire.Model, now time.Time) {
	from := c.rec.Model
	c.rec.Model = to
	c.rec.PendingModel = nil
	c.pendingTries, c.pendingAt = 0, time.Time{}
	c.logSwitch(wire.ModelSwitch{At: ms(now), From: from, To: *to})
}

// logSwitch adds a change, applied or refused, to the log the feed draws, keeping the last ModelLogMax.
func (c *conv) logSwitch(sw wire.ModelSwitch) {
	c.rec.ModelLog = append(c.rec.ModelLog, sw)
	if n := len(c.rec.ModelLog); n > wire.ModelLogMax {
		c.rec.ModelLog = slices.Clone(c.rec.ModelLog[n-wire.ModelLogMax:])
	}
	c.touch()
}

// modelTook is which steps of a change the agent confirmed, and the error of the first that it did not.
type modelTook struct {
	model, effort, ultracode, fast, permission bool
	err                                        error
}

func (t modelTook) failed(err error) modelTook {
	t.err = err
	return t
}

// on is the model the agent runs after the steps that took, from what it ran; nil when none took.
func (t modelTook) on(from *wire.Model, m wire.ModelCmd, diff modelDiff) *wire.Model {
	if !t.model && !t.effort && !t.ultracode && !t.fast && !t.permission {
		return nil
	}
	to := wire.Model{}
	if from != nil {
		to = *from
	}
	if t.model {
		// A new model runs at its own effort until one is set: the old one's level is not carried over as known.
		to.ID, to.Effort = m.Model, nil
	}
	if t.effort {
		to.Effort = m.Effort
	}
	if t.ultracode {
		to.Ultracode = diff.ultracodeOn
	}
	if t.fast {
		to.Fast = diff.fastOn
	}
	if t.permission {
		to.Permission = diff.permissionTo
	}
	return &to
}

// modelFits checks the ask against the models the machine lists for the provider.
func (c *conv) modelFits(m wire.ModelCmd) wire.RefuseCode {
	if cur := c.rec.Model; cur != nil && cur.Profile != "" {
		// A conversation on a backend profile changes models only among the profile's own; each model names its efforts.
		if c.d.Profiles == nil {
			return wire.RefuseUnsupported
		}
		_, _, models, ok := c.d.Profiles.Lookup(cur.Profile)
		capabilities, known := c.d.Profiles.Model(cur.Profile, m.Model)
		if !ok || !known || !slices.Contains(models, m.Model) || m.UltracodeAfter(cur) || m.FastAfter(cur) ||
			(m.Effort != nil && !slices.Contains(capabilities.Efforts, *m.Effort)) {
			return wire.RefuseOptionUnknown
		}
		return ""
	}
	if c.d.vendorOnly(c.rec.Provider, m.Model) {
		return wire.RefuseOptionUnknown
	}
	if c.d.Models == nil {
		return ""
	}
	list := c.d.Models(c.rec.Provider)
	i := slices.IndexFunc(list, func(x wire.AgentModel) bool { return x.ID == m.Model })
	if i < 0 {
		return ""
	}
	if m.Effort != nil && !slices.Contains(list[i].Efforts, *m.Effort) {
		return wire.RefuseOptionUnknown
	}
	if m.UltracodeAfter(c.rec.Model) && !list[i].Ultracode {
		return wire.RefuseOptionUnknown
	}
	if m.Fast != nil && *m.Fast && !list[i].Fast {
		return wire.RefuseOptionUnknown
	}
	return ""
}

// claudeMode is Claude Code's footer name of a permission mode of the picker.
var claudeMode = map[wire.PermissionMode]string{
	wire.PermissionPlan:   screen.ClaudeModePlan,
	wire.PermissionAsk:    screen.ClaudeModeDefault,
	wire.PermissionBypass: screen.ClaudeModeBypass,
}

func modelRefusal(err error) wire.RefuseCode {
	switch {
	case errors.Is(err, typist.ErrNotAtPrompt), errors.Is(err, typist.ErrInputOccupied):
		return wire.RefuseAgentWorking
	case errors.Is(err, typist.ErrCommandDeclined):
		return wire.RefuseConflict
	case errors.Is(err, typist.ErrCommandRefused):
		return wire.RefuseOptionUnknown
	case errors.Is(err, errNoRestart), errors.Is(err, errNotOwned):
		return wire.RefuseUnsupported
	}
	return wire.RefuseInternal
}

// vendorOnly: the model is a vendor's that only a profile reaches: a profile owns the id (configured or not, of either
// CLI) and the provider's own account does not list it. Codex signed in with ChatGPT lists the GPT ids the built-in GPT
// profile has too: those run on the account. A conversation of p without a profile is not started on a vendor-only
// model nor switched to it, so that it does not run on the wrong account.
func (d *Deps) vendorOnly(p wire.Provider, model string) bool {
	if d.Profiles == nil || !d.Profiles.OwnsModel(model) {
		return false
	}
	if d.Models != nil && slices.ContainsFunc(d.Models(p), func(x wire.AgentModel) bool { return x.ID == model }) {
		return false
	}
	return true
}
