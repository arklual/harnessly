package convo

import (
	"github.com/ly-solutions/harnessly/internal/agentstate"
	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// closePane ends the agent of a deleted conversation: its tmux session and its tab. It runs off the actor, under the
// pane's lock (nobody types into the box while it goes). The conversation stays for its 7 days; restored, it is a
// conversation whose agent is gone.
func (c *conv) closePane() {
	if c.d.Closer == nil || c.rec.Session == "" {
		return
	}
	rec, closer := c.rec, c.d.Closer
	c.spawn(func() any {
		unlock := typist.Lock(rec.Session)
		defer unlock()
		return closer.Close(rec)
	}, func(r any) {
		if err, _ := r.(error); err != nil {
			c.d.Log.Warn("pane of a deleted conversation not closed", "conv", rec.ID, "err", err)
			return
		}
		c.input(agentstate.Input{K: agentstate.Gone, GoneReason: wire.GoneTabClosed}, c.d.Clock.Now())
	})
}
