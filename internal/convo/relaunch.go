package convo

import (
	"context"
	"time"
)

// Relaunch starts the conversation's agent again on its session, with its history, once it is at rest: a turn that
// runs finishes first. What the next launch is (Harnessly Connect: on the person's computer or here) is the
// launcher's to know; a chat whose agent is not running now needs nothing, its next launch takes it. ErrNotFound for
// an unknown conversation.
func (r *Runtime) Relaunch(ctx context.Context, id string) error {
	if r.view(id) == nil {
		return ErrNotFound
	}
	return r.call(ctx, id, func(c *conv, now time.Time) {
		c.relaunchWanted = true
		c.pump(now)
	})
}

// relaunchAtRest restarts the agent when Relaunch asked for it and the agent is at its prompt; it reports whether it
// took the pane. A chat with no agent running forgets the ask: its next launch is already the new one.
func (c *conv) relaunchAtRest(now time.Time) bool {
	if !c.relaunchWanted {
		return false
	}
	if c.rec.Session == "" || c.d.Machine.Pane(c.rec.Session) == nil {
		c.relaunchWanted = false
		return false
	}
	if !c.canSwitchNow() {
		return false
	}
	c.relaunchWanted = false
	if !c.canRestart() {
		c.d.Log.Warn("relaunch: the agent cannot be restarted", "conv", c.rec.ID)
		return false
	}
	// The model stays: only where the agent runs changes.
	c.restartAgent(nil, func(err error, now time.Time) {
		if err != nil {
			c.d.Log.Warn("relaunch failed", "conv", c.rec.ID, "err", err)
		}
		c.read(now)
		c.pump(now)
	})
	return true
}
