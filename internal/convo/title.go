package convo

import (
	"github.com/ly-solutions/harnessly/internal/attach"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// titleWait is how long the agent has, after the first answer, to write a title of its own before one is made for the
// chat: Claude writes its ai-title a moment after the answer, and the read of the transcript that finds it is every
// metaEvery.
const titleWait = 30 * time.Second

// armTitle starts the wait for the agent's title when a turn ends in a chat still named by its first message. Only a
// turn that ended while we watched counts: a chat opened on a transcript that is over already is not named anew at
// every start of the runtime.
func (c *conv) armTitle(now time.Time) {
	if c.d.Titler == nil || c.titleTried || !c.titleDueAt.IsZero() || c.rec.TitleSource != wire.TitleFromFirstMessage {
		return
	}
	c.titleDueAt = now.Add(titleWait)
}

// watchTitle makes a title for a chat the agent wrote none for, off the actor, once.
func (c *conv) watchTitle(now time.Time) {
	if c.titleDueAt.IsZero() || c.titling || now.Before(c.titleDueAt) || c.deletedAt != 0 {
		return
	}
	if c.rec.TitleSource != wire.TitleFromFirstMessage {
		c.titleDueAt, c.titleTried = time.Time{}, true // the agent or the person named it meanwhile
		return
	}
	if c.metaReading {
		c.titleDueAt = now.Add(metaEvery) // the transcript is being asked for the agent's title just now
		return
	}
	first, answer := c.firstExchange()
	if first == "" || answer == "" {
		c.titleDueAt = now.Add(titleWait) // the answer is not in the feed yet
		return
	}
	c.titling = true
	titler, provider := c.d.Titler, c.rec.Provider
	c.spawn(func() any {
		t, err := titler(provider, first, answer)
		if err != nil {
			c.d.Log.Warn("chat title not made", "conv", c.rec.ID, "err", err)
			return ""
		}
		return t
	}, func(r any) {
		c.titling, c.titleTried, c.titleDueAt = false, true, time.Time{}
		t, _ := r.(string)
		now := c.d.Clock.Now()
		// The agent's own title, or a rename, may have come while the title was being made: then it is not needed.
		if c.rec.TitleSource == wire.TitleFromFirstMessage && c.setAgentTitle(t) {
			c.touch()
			c.flush(now, true)
		}
	})
}

// retitleWait is how long a «name it anew» holds its answer for the title: a cheap model answers in a few seconds, and
// a slower one is answered as accepted, the title coming with the record when it is made.
const retitleWait = 8 * time.Second

// retitleDone is what the model made, or why it did not.
type retitleDone struct {
	title string
	err   error
}

// retitle names the conversation anew from its first message and the agent's latest answer, whatever named it before.
// The answer is the title's when it comes in time.
func (c *conv) retitle(finish func(wire.CmdResult), now time.Time) {
	c.mark, c.markConvs = 0, 0
	if c.deletedAt != 0 {
		finish(wire.Refused(wire.RefuseNotFound))
		return
	}
	if c.d.Titler == nil {
		finish(wire.Refused(wire.RefuseUnsupported))
		return
	}
	if c.titling {
		finish(wire.Refused(wire.RefuseConflict))
		return
	}
	first, answer := c.firstExchange()
	if latest := c.latestAnswer(); latest != "" {
		answer = latest
	}
	if first == "" || answer == "" {
		finish(wire.Refused(wire.RefuseBadRequest)) // nothing to name it from yet
		return
	}
	c.titling = true
	titler, provider := c.d.Titler, c.rec.Provider
	made := make(chan retitleDone, 1)
	go func() {
		t, err := titler(provider, first, answer)
		made <- retitleDone{t, err}
	}()
	apply := func(r retitleDone) bool {
		c.titling = false
		if r.err != nil || strings.TrimSpace(r.title) == "" {
			if r.err != nil {
				c.d.Log.Warn("chat title not made", "conv", c.rec.ID, "err", r.err)
			}
			return false
		}
		c.rec.Title, c.rec.TitleSource = titleOf(r.title), wire.TitleFromAgent
		c.touch()
		c.flush(c.d.Clock.Now(), true)
		return true
	}
	c.spawn(func() any {
		select {
		case r := <-made:
			return r
		case <-time.After(retitleWait):
			return nil
		}
	}, func(v any) {
		if r, ok := v.(retitleDone); ok {
			if !apply(r) {
				finish(wire.Refused(wire.RefuseInternal))
				return
			}
			finish(c.recResult(c.d.Clock.Now()))
			return
		}
		// Slow: the person has the answer, the title follows on the record.
		finish(c.recResult(c.d.Clock.Now()))
		c.spawn(func() any { return <-made }, func(v any) {
			if r, ok := v.(retitleDone); ok {
				apply(r)
			}
		})
	})
}

// latestAnswer is the last thing the agent said in the chat.
func (c *conv) latestAnswer() string {
	_, entries, _ := c.fd.Snapshot(1 << 20)
	answer := ""
	for _, e := range entries {
		if e.T == wire.EntrySay {
			if md := strings.TrimSpace(e.MD); md != "" {
				answer = md
			}
		}
	}
	return answer
}

// firstExchange is the first message of the chat and the first thing the agent said back.
func (c *conv) firstExchange() (message, answer string) {
	_, entries, _ := c.fd.Snapshot(1 << 20)
	for _, e := range entries {
		switch {
		case e.T == wire.EntryUser && message == "":
			// The places attached to it are not what the chat is about.
			message = strings.TrimSpace(attach.Strip(e.Text))
		case e.T == wire.EntrySay && message != "" && answer == "":
			answer = strings.TrimSpace(e.MD)
		}
	}
	return message, answer
}
