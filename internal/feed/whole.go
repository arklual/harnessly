package feed

import "github.com/ly-solutions/harnessly/internal/wire"

// WholeTurn is one turn of a whole transcript with every entry it holds, in order: what opened it first.
type WholeTurn struct {
	Turn    wire.Turn
	Entries []wire.Entry
}

// Whole is every turn of a transcript, oldest first, read from its start: what a handoff takes its excerpt from and
// what a fork finds its cut in. The live feed of the conversation is not touched.
func Whole(conv string, provider wire.Provider, src Source) []WholeTurn {
	f := New(conv, provider, src)
	f.loadFrom(src.Tail(1 << 30))
	out := make([]WholeTurn, 0, len(f.b.turns))
	for _, t := range f.b.turns {
		es := make([]wire.Entry, 0, len(t.entries))
		for _, id := range t.entries {
			if i, ok := f.b.byID[id]; ok {
				es = append(es, f.b.entries[i])
			}
		}
		out = append(out, WholeTurn{Turn: t.rec, Entries: es})
	}
	return out
}

// Records are the records of the turns the feed holds, oldest first.
func (f *Feed) Records() []wire.Turn {
	out := make([]wire.Turn, len(f.b.turns))
	for i, t := range f.b.turns {
		out[i] = t.rec
	}
	return out
}
