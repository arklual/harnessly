package transcriptindex

import (
	"sync"
	"time"
)

const (
	// DefaultFollowBytes is how much of a transcript a Follower holds for its reader.
	DefaultFollowBytes = 64 << 10
	// sweepPlaces is how many places a Follower holds before a new one makes it look for ended ones.
	sweepPlaces = 64
	// DefaultFollowIdle is for how long a Follower holds lines for a reader that does not come for them.
	DefaultFollowIdle = 2 * time.Minute
)

// Follower serves a reader that reads transcripts when asked, not on a clock: a list of sessions, a count of
// messages, a page of costs. Such a reader keeps a cursor of its own in many files, most of them at rest. For the
// files the index follows anyway (a conversation is live on them) the Follower holds the lines the index reads, so
// the reader does not open those files again; it never makes the index follow a file, and ends with the file's last
// other subscriber. For every other file, and whenever Take says no, the reader reads the file itself as it always
// did and tells the Follower where it stands (Seat).
//
// What a Follower holds is bounded twice: by bytes per file (lines that do not fit are not kept and the place is
// given up) and by time (a reader that has not come for DefaultFollowIdle holds nothing until it comes again).
// Giving a place up is the ordinary course for such a reader, not a loss: it is neither counted nor logged.
// A nil Follower serves nothing: every Take says no.
type Follower struct {
	ix   *Index
	name string
	max  int
	idle time.Duration

	mu     sync.Mutex
	places map[string]*place
}

// place is a reader's place in one transcript.
type place struct {
	fo  *Follower
	sub *Sub

	mu      sync.Mutex
	buf     []byte
	from    int64 // where buf starts in the file
	next    int64 // the start of the first line the reader has not taken
	takenAt time.Time
}

// Follower is a follower for the reader called name that holds max bytes per file (DefaultFollowBytes when 0). On a
// nil index it is nil.
func (ix *Index) Follower(name string, max int) *Follower {
	if ix == nil {
		return nil
	}
	if max <= 0 {
		max = DefaultFollowBytes
	}
	return &Follower{ix: ix, name: name, max: max, idle: DefaultFollowIdle, places: map[string]*place{}}
}

// Lines keeps a copy of the batch for the reader. Called by the index with the file held.
func (p *place) Lines(b *Batch) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	data := b.Bytes()
	if p.fo.ix.o.Now().Sub(p.takenAt) > p.fo.idle || len(p.buf)+len(data) > p.fo.max {
		p.buf = nil
		return false
	}
	if len(p.buf) == 0 {
		p.from = b.From
	}
	p.buf = append(p.buf, data...)
	return true
}

// Take gives the whole lines the transcript gained after offset, the reader's own place: data is the reader's to
// keep. partial says a line is still being written after them. ok is false when the Follower cannot say what the
// file gained after offset: the reader reads the file itself and seats again. With ok and no data and no partial the
// file is as the reader left it.
func (fo *Follower) Take(path string, offset int64) (data []byte, partial, ok bool) {
	if fo == nil {
		return nil, false, false
	}
	fo.mu.Lock()
	p := fo.places[path]
	fo.mu.Unlock()
	if p == nil {
		return nil, false, false
	}
	st, partial := p.sub.Sync(false)
	p.mu.Lock()
	p.takenAt = fo.ix.o.Now()
	data, from := p.buf, p.from
	p.buf = nil
	if len(data) == 0 {
		from = p.next
	}
	ok = st == Fresh && p.next == offset && from == offset
	if ok {
		p.next = offset + int64(len(data))
	}
	p.mu.Unlock()
	if !ok {
		fo.drop(path, p)
		return nil, false, false
	}
	return data, partial, true
}

// Seat says the reader has read the transcript up to offset, the start of a line, itself. A transcript the index
// does not follow is not followed for the reader's sake.
func (fo *Follower) Seat(path string, offset int64) {
	if fo == nil {
		return
	}
	fo.mu.Lock()
	p := fo.places[path]
	if p == nil {
		if len(fo.places) >= sweepPlaces {
			// Places whose file nobody follows any more are let go of.
			for k, old := range fo.places {
				if old.sub.Closed() {
					delete(fo.places, k)
				}
			}
		}
		p = &place{fo: fo}
		if p.sub = fo.ix.subscribePassive(path, fo.name, p); p.sub == nil {
			fo.mu.Unlock()
			return
		}
		fo.places[path] = p
	}
	fo.mu.Unlock()
	p.mu.Lock()
	p.buf, p.next, p.takenAt = nil, offset, fo.ix.o.Now()
	p.mu.Unlock()
	p.sub.Seat(offset, 0)
	p.mu.Lock()
	if len(p.buf) > 0 && p.from != offset {
		p.buf = nil // lines of the place before
	}
	p.mu.Unlock()
}

// Forget lets go of the reader's place in the transcript.
func (fo *Follower) Forget(path string) {
	if fo == nil {
		return
	}
	fo.mu.Lock()
	p := fo.places[path]
	fo.mu.Unlock()
	if p != nil {
		fo.drop(path, p)
	}
}

// Places is how many transcripts the follower has a place in.
func (fo *Follower) Places() int {
	if fo == nil {
		return 0
	}
	fo.mu.Lock()
	defer fo.mu.Unlock()
	return len(fo.places)
}

// drop ends a place. Called without the place's lock: closing takes the file, which is held while the place is
// handed lines.
func (fo *Follower) drop(path string, p *place) {
	fo.mu.Lock()
	if fo.places[path] == p {
		delete(fo.places, path)
	}
	fo.mu.Unlock()
	p.sub.Close()
}
