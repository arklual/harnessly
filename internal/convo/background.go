package convo

import (
	"reflect"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// bgEvery is how often an agent's background tasks are read: the read is a stat of the transcript, and the lines
// it gained when it grew.
const bgEvery = 3 * time.Second

// watchBackground reads the agent's background tasks off the actor, now and then, and gives them back on it. The tasks
// of an agent that is not running are gone with it; the subagents that ended stay for a person to read. A chat nobody
// has opened since the runtime started, whose agent is not running, is not read at all, and one that was is read once.
func (c *conv) watchBackground(now time.Time) {
	if c.d.Background == nil || c.deletedAt != 0 {
		return
	}
	dead := false
	switch c.rec.State.K {
	case wire.StateStopped, wire.StateGone, wire.StateLaunchFailed:
		dead = true
	case wire.StateStarting, wire.StateUnknown:
		return
	}
	switch {
	case dead && !c.bgOpened:
		c.bgWake = 0
		c.bgApply(nil, now)
		return
	case dead && c.bgDeadRead:
		return
	case !dead:
		c.bgDeadRead = false
	}
	if c.bgReading || now.Sub(c.bgAt) < bgEvery {
		return
	}
	c.bgReading, c.bgAt = true, now
	pane := ""
	if c.look != nil {
		pane = c.look.Plain
	}
	rec, bg := c.rec, c.d.Background
	c.spawn(func() any {
		got := bgRead{tasks: bg.Tasks(rec, pane)}
		if w, ok := bg.(Waker); ok {
			got.wakeup = w.Wakeup(rec)
		}
		return got
	}, func(r any) {
		c.bgReading = false
		c.bgDeadRead = dead
		got, _ := r.(bgRead)
		if dead { // what ran with the agent is over, its wakeup too; the subagents that ended stay
			got.tasks = slices.DeleteFunc(got.tasks, func(t wire.BackgroundTask) bool { return !t.Teammate && t.Status == wire.BgStatusRunning })
			got.tasks = teammatesOver(got.tasks)
			got.wakeup = 0
		}
		c.bgWake = got.wakeup
		c.bgApply(got.tasks, c.d.Clock.Now())
	})
}

// teammatesOver: the named teammates live in the agent's process, so with it gone the ones at work or idle are over
// too — done, and listed for a person to read like a subagent that ended.
func teammatesOver(tasks []wire.BackgroundTask) []wire.BackgroundTask {
	for i, t := range tasks {
		if t.Teammate && (t.Status == wire.BgStatusRunning || t.Status == wire.BgStatusIdle) {
			tasks[i].Status = wire.BgStatusDone
		}
	}
	return tasks
}

// bgRead is what one read of the agent's background work brings.
type bgRead struct {
	tasks  []wire.BackgroundTask
	wakeup wire.Ms
}

// bgApply makes the page's list the agent's: a task new or changed goes out whole, one gone is removed. A task we
// ended stays as stopped for as long as the agent still lists it.
func (c *conv) bgApply(list []wire.BackgroundTask, now time.Time) {
	listed := map[string]bool{}
	for _, t := range list {
		listed[t.ID] = true
		i := slices.IndexFunc(c.bg, func(x wire.BackgroundTask) bool { return x.ID == t.ID })
		if t.StartedAt == 0 {
			// A task only the footer knows has no start; it started no later than it was first seen.
			t.StartedAt = ms(now)
			if i >= 0 {
				t.StartedAt = c.bg[i].StartedAt
			}
		}
		if c.bgStopped[t.ID] && i >= 0 {
			continue
		}
		if i < 0 {
			c.bg = append(c.bg, t)
		} else if reflect.DeepEqual(c.bg[i], t) {
			continue
		} else {
			c.bg[i] = t
		}
		c.dirty = true
		c.emit(wire.BackgroundUpsertEv{Task: t})
	}
	for _, t := range slices.Clone(c.bg) {
		if listed[t.ID] {
			continue
		}
		c.bg = slices.DeleteFunc(c.bg, func(x wire.BackgroundTask) bool { return x.ID == t.ID })
		delete(c.bgStopped, t.ID)
		c.dirty = true
		c.emit(wire.BackgroundRemovedEv{ID: t.ID})
	}
	c.bgAgents(now)
	slices.SortStableFunc(c.bg, func(a, b wire.BackgroundTask) int {
		switch {
		case a.StartedAt != b.StartedAt:
			return int(a.StartedAt - b.StartedAt)
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		}
		return 0
	})
}

// bgAgents keeps the record's account of what the agent still waits for — subagents, teammates at work (an idle one
// waits for a message, nothing waits for it), workflows, commands, monitors and its scheduled wakeup: the sidebar draws a chat that waits for any of them as at work, not as finished. A wakeup whose
// time has come is no longer waited for.
func (c *conv) bgAgents(now time.Time) {
	var w wire.BgWait
	commands := []string{}
	for _, t := range c.bg {
		if t.Status != wire.BgStatusRunning {
			continue
		}
		switch {
		case t.Teammate:
			w.Teammates++
		case t.Kind == wire.BgSubagent:
			w.Agents++
		case t.Kind == wire.BgWorkflow:
			w.Workflows++
		case t.Monitor:
			w.Monitors++
		case t.Kind == wire.BgCommand:
			w.Commands++
			commands = append(commands, t.Title)
		}
	}
	if w.Commands == 1 {
		w.Command = commands[0]
	}
	if c.bgWake > 0 && int64(c.bgWake) > ms(now) {
		at := c.bgWake
		w.WakeupAt = &at
	}
	n := w.Total()
	var next *wire.BgWait
	if n > 0 {
		next = &w
	}
	if n == c.rec.BgAgents && reflect.DeepEqual(next, c.rec.BgWait) {
		return
	}
	c.rec.BgAgents, c.rec.BgWait = n, next
	c.touch()
	c.flush(now, false)
}

func (c *conv) background() []wire.BackgroundTask {
	return slices.Clone(c.bg)
}

// bgStop ends a background task of the agent. The answer is accepted on the conversation's channel from the event
// that shows the task stopped, once the task is over.
func (c *conv) bgStop(m wire.BgStopCmd, finish func(wire.CmdResult), now time.Time) {
	c.mark, c.markConvs = 0, 0
	i := slices.IndexFunc(c.bg, func(x wire.BackgroundTask) bool { return x.ID == m.TaskID })
	switch {
	case c.deletedAt != 0 || i < 0:
		finish(wire.Refused(wire.RefuseNotFound))
		return
	case c.bg[i].Status != wire.BgStatusRunning:
		finish(wire.Accepted(wire.ConvChannel(c.rec.ID), c.seq)) // already over
		return
	case c.d.Background == nil || !c.bg[i].Stoppable:
		finish(wire.Refused(wire.RefuseUnsupported))
		return
	}
	rec, bg, session := c.rec, c.d.Background, c.rec.Session
	c.spawn(func() any {
		// Under the pane's lock: nobody types into the box while the process under it goes.
		unlock := typist.Lock(session)
		defer unlock()
		return bg.Stop(rec, m.TaskID)
	}, func(r any) {
		c.mark = 0
		err, _ := r.(error)
		if err != nil {
			c.d.Log.Warn("background task not stopped", "conv", c.rec.ID, "task", m.TaskID, "err", err)
			finish(wire.Refused(wire.RefuseInternal))
			return
		}
		i := slices.IndexFunc(c.bg, func(x wire.BackgroundTask) bool { return x.ID == m.TaskID })
		if i >= 0 {
			ended := ms(c.d.Clock.Now())
			c.bg[i].Status, c.bg[i].EndedAt, c.bg[i].Stoppable = wire.BgStatusStopped, &ended, false
			if c.bgStopped == nil {
				c.bgStopped = map[string]bool{}
			}
			c.bgStopped[m.TaskID] = true
			c.dirty = true
			c.emit(wire.BackgroundUpsertEv{Task: c.bg[i]})
		}
		finish(wire.Accepted(wire.ConvChannel(c.rec.ID), c.convSeq()))
	})
}
