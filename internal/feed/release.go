package feed

import (
	"bytes"
	"math"
	"os"
	"slices"

	"github.com/ly-solutions/harnessly/internal/transcript"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A quiet conversation's feed lets go of what it read (HRN-901). Every conversation of a machine holds its feed, and
// a feed holds its last 2000 entries twice over (the source's and the built ones): ~4 MB for a busy chat, 1.6 GB for
// the 395 chats of one account, most of them asleep and opened by nobody. Released, the feed keeps where its read stood
// and the few answers the actor asks of it on every look; anything else reads the same bytes of the transcript again
// and builds the feed as it was (the ids come from the transcript, and what the runtime set on the feed is kept).

// releasedFeed is what a released feed keeps: where its read stood, and the summary the actor's looks ask for.
type releasedFeed struct {
	start, size int64
	more        bool
	lastTurn    *wire.Turn
	snippet     string
	context     int64
	model       string
	tasks       []wire.Task
	// revs are the entries' revisions a rebuild does not give (a client id set, a turn ended): a page that held the
	// entry takes an update only above the revision it has.
	revs map[string]int64
}

// Release lets go of the entries read, keeping where the read stood; false while a turn is open (the actor reads the
// open turn on every look), before the first read, or when already released.
func (f *Feed) Release() bool {
	if !f.loaded || f.rel != nil || f.b.openID() != "" {
		return false
	}
	r := &releasedFeed{start: f.start, size: f.size, more: f.more, context: f.b.context, model: f.b.model,
		tasks: slices.Clone(f.b.tasks.all())}
	for _, e := range f.b.entries {
		if e.Rev > 1 {
			if r.revs == nil {
				r.revs = map[string]int64{}
			}
			r.revs[e.ID] = e.Rev
		}
	}
	uses := f.uses
	if t := f.LastTurn(); t != nil {
		r.lastTurn, r.snippet = t, f.Snippet(*t)
	}
	f.uses = uses
	f.raws = nil
	f.b = newBuilder(f.conv, f.provider)
	f.b.vendor = f.vendor
	f.rel = r
	return true
}

// Released says whether the feed holds nothing read now.
func (f *Feed) Released() bool { return f.rel != nil }

// Uses counts the reads of the feed beyond the summary a look asks for: a caller that sees it unchanged for a while
// knows nobody reads the feed.
func (f *Feed) Uses() int { return f.uses }

// ensure makes the feed hold what it read again, when released, and counts a use.
func (f *Feed) ensure() {
	f.uses++
	if f.rel == nil {
		return
	}
	r := f.rel
	f.rel = nil
	c := f.reread(r)
	f.raws = f.raws[:0]
	for _, o := range c.Entries {
		f.raws = append(f.raws, raw{o, -1})
	}
	f.start, f.more, f.size = r.start, r.more, r.size
	f.rebuild()
	for id, rev := range r.revs {
		if i, ok := f.b.byID[id]; ok && f.b.entries[i].Rev < rev {
			f.b.entries[i].Rev = rev
		}
	}
}

// reread is the entries whose lines start in [start, size) of the transcript: what the feed held when released. The
// source's place for the next incremental read is not touched.
func (f *Feed) reread(r *releasedFeed) transcript.Chunk {
	if r.size <= r.start {
		return transcript.Chunk{}
	}
	if w, ok := f.src.(windowed); ok {
		from, to := r.start, r.size
		if cs, ok := f.src.(*ClaudeSource); ok {
			from, to = lineBounds(cs.Path, from, to)
		}
		return w.BeforeWindow(to, to-from, math.MaxInt32)
	}
	return f.src.Before(r.size, math.MaxInt32)
}

// pollReleased is Poll on a released feed: a read that gains no entries leaves it released (an idle chat is polled
// every few seconds); one that gains some takes back what the feed held first, so the new entries build on it.
func (f *Feed) pollReleased() (Update, bool) {
	from := f.size
	inc := f.src.Since()
	if inc.NewOffset < from {
		f.rel = nil
		return f.Load(), true
	}
	if len(inc.NewEntries) == 0 {
		f.size = inc.NewOffset - int64(len(inc.PendingBuffer))
		f.rel.size = f.size
		return Update{}, true
	}
	f.ensure()
	f.size = inc.NewOffset - int64(len(inc.PendingBuffer))
	var u Update
	for _, o := range inc.NewEntries {
		f.raws = append(f.raws, raw{o, from})
		f.apply(&u, f.b.add(o, from))
	}
	return u, true
}

// lineBounds widens [from, to) to the lines it cuts: the feed counts its places in decoded text (a broken character
// counts three bytes), so they may stand a byte or two inside the line the read began or ended with.
func lineBounds(path string, from, to int64) (int64, int64) {
	fh, err := os.Open(path)
	if err != nil {
		return from, to
	}
	defer fh.Close()
	var buf [4096]byte
	// from: just after the last newline before it.
	for end := from; end > 0; {
		lo := max(0, end-int64(len(buf)))
		n, _ := fh.ReadAt(buf[:end-lo], lo)
		if i := bytes.LastIndexByte(buf[:n], '\n'); i >= 0 {
			from = lo + int64(i) + 1
			break
		}
		end = lo
		if lo == 0 {
			from = 0
		}
	}
	// to: just after the first newline at or after its last byte.
	for at := max(0, to-1); ; {
		n, err := fh.ReadAt(buf[:], at)
		if i := bytes.IndexByte(buf[:n], '\n'); i >= 0 {
			return from, at + int64(i) + 1
		}
		if err != nil || n == 0 {
			return from, to
		}
		at += int64(n)
	}
}
