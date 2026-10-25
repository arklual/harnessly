package feed

import "github.com/ly-solutions/harnessly/internal/wire"

// Point is a turn a rewind goes back to: the turn the person's message opened, and what the rewind leaves behind.
type Point struct {
	// Turns is how many turns the rewind leaves behind: this one and every one after it.
	Turns int
	// After is the last turn kept (nil: none the feed read), and its end (nil: it has not ended).
	After    *string
	AfterEnd *wire.Ms
	// At is when the message was written.
	At wire.Ms
}

// RewindPoint is the turn turnID as a rewind's point; ok false when the transcript has no such turn or a person's
// message did not open it. Older turns than the feed holds are read until it is found.
func (f *Feed) RewindPoint(turnID string) (Point, bool) {
	i, ok := f.find(turnID)
	if !ok {
		return Point{}, false
	}
	t := f.b.turns[i]
	j, has := f.b.byID[t.rec.ID]
	if !has || f.b.entries[j].T != wire.EntryUser {
		return Point{}, false
	}
	p := Point{Turns: len(f.b.turns) - i, At: f.b.entries[j].At}
	if i > 0 {
		before := f.b.turns[i-1].rec
		p.After, p.AfterEnd = sptr(before.ID), before.EndedAt
	}
	return p, true
}

// From is the turns from turnID to the end, by their faces (the message that opened each and its answer), at most
// limit of the newest; ok false when the transcript has no such turn.
func (f *Feed) From(turnID string, limit int) (wire.TurnsPage, bool) {
	i, ok := f.find(turnID)
	if !ok {
		return wire.TurnsPage{}, false
	}
	end := len(f.b.turns)
	ts, es, _ := f.page(end, min(limit, end-i), 0)
	return wire.TurnsPage{Turns: ts, Entries: es}, true
}

// find is the index of turn id, reading older pages of the transcript until it is there or there are none.
func (f *Feed) find(id string) (int, bool) {
	for {
		if i, ok := f.b.byTurn[id]; ok {
			return i, true
		}
		if !f.more || f.older() == 0 && !f.more {
			return 0, false
		}
	}
}
