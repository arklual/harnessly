package convo

import (
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/attach"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// summarize asks the cheap model, once a turn, for a few lines on what the agent did in the conversation's last turn
// The answer is accepted at once; the lines come later on the record as its recap, as Claude's own «while you
// were away» does, and stand like it until the person writes again. A recap Claude wrote after the turn ended is kept:
// the model is not asked for what is there.
func (c *conv) summarize(turnID string, now time.Time) wire.CmdResult {
	c.mark, c.markConvs = 0, 0
	if c.deletedAt != 0 {
		return wire.Refused(wire.RefuseNotFound)
	}
	if c.d.Summarizer == nil {
		return wire.Refused(wire.RefuseUnsupported)
	}
	t := c.rec.LastTurn
	if t == nil || t.ID != turnID || t.Status != wire.TurnDone || t.EndedAt == nil {
		return wire.Refused(wire.RefuseBadRequest) // only the last turn, and only one that is done
	}
	if c.summarized == turnID {
		return c.recResult(now)
	}
	if r := c.rec.Recap; r != nil && r.At >= *t.EndedAt && c.recapStands(r) {
		c.summarized = turnID
		return c.recResult(now)
	}
	message, replies := c.turnTexts(turnID)
	if len(replies) == 0 {
		return wire.Refused(wire.RefuseBadRequest) // the turn said nothing to sum up
	}
	c.summarized = turnID
	summarizer, provider := c.d.Summarizer, c.rec.Provider
	c.spawn(func() any {
		text, err := summarizer(provider, message, replies)
		if err != nil {
			return err
		}
		return text
	}, func(v any) {
		text, ok := v.(string)
		if !ok {
			c.d.Log.Warn("turn summary not made", "conv", c.rec.ID, "err", v)
			return
		}
		c.applySummary(turnID, strings.TrimSpace(text))
	})
	return c.recResult(now)
}

// applySummary puts the turn's summary on the record, unless the chat moved on or Claude wrote a recap of its own after
// the turn meanwhile.
func (c *conv) applySummary(turnID, text string) {
	t := c.rec.LastTurn
	if text == "" || t == nil || t.ID != turnID || t.EndedAt == nil {
		return
	}
	now := c.d.Clock.Now()
	r := &wire.Recap{Text: text, At: ms(now)}
	c.summary = r
	if cur := c.rec.Recap; cur != nil && cur.At >= *t.EndedAt && c.recapStands(cur) {
		return
	}
	c.rec.Recap = r
	c.touch()
	c.flush(now, true)
}

// turnTexts are the person's message that opened a turn and what the agent said in it, in order: its replies only,
// never what the tools were given or gave back.
func (c *conv) turnTexts(turnID string) (message string, replies []string) {
	_, entries, _ := c.fd.Snapshot(1 << 20)
	for _, e := range entries {
		if e.ID != turnID && (e.TurnID == nil || *e.TurnID != turnID) {
			continue
		}
		switch e.T {
		case wire.EntryUser:
			if message == "" {
				message = strings.TrimSpace(attach.Strip(e.Text))
			}
		case wire.EntrySay:
			if md := strings.TrimSpace(e.MD); md != "" {
				replies = append(replies, md)
			}
		}
	}
	return message, replies
}

// summaryOver is the recap the record shows when the transcript has recap: the turn's own summary while it is the newer
// and still stands, else the transcript's.
func (c *conv) summaryOver(recap *wire.Recap) *wire.Recap {
	if s := c.summary; s != nil && c.recapStands(s) && (recap == nil || s.At > recap.At) {
		return s
	}
	return recap
}
