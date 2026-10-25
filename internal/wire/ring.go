package wire

import "time"

// The ring of a channel keeps its last events for a page that comes back: RingSize of them, none older than RingAge.
// A sub whose afterSeq is still in the ring gets the events it missed; past that it gets a snapshot.
const (
	RingSize = 2000
	RingAge  = 5 * time.Minute
)

// ringItem is one event as it went out: its seq, when, and the whole encoded ev frame. Older holds the frame a page
// without every Cap gets, for the caps it differs for (eventFor); nil when it is the same for all.
type ringItem struct {
	seq   int64
	at    time.Time
	frame []byte
	older map[Caps][]byte
}

// forCaps is the frame a page with caps gets.
func (it ringItem) forCaps(caps Caps) []byte {
	if b, ok := it.older[caps]; ok {
		return b
	}
	return it.frame
}

// ring is a fixed buffer of a channel's last events, oldest first. Not safe for concurrent use: the hub holds it under
// its lock.
type ring struct {
	buf   []ringItem
	start int // index of the oldest item
	n     int
}

func (r *ring) push(it ringItem) {
	if r.buf == nil {
		r.buf = make([]ringItem, RingSize)
	}
	if r.n == len(r.buf) {
		r.buf[r.start] = ringItem{}
		r.start = (r.start + 1) % len(r.buf)
		r.n--
	}
	r.buf[(r.start+r.n)%len(r.buf)] = it
	r.n++
}

func (r *ring) at(i int) *ringItem { return &r.buf[(r.start+i)%len(r.buf)] }

// trim drops the items older than RingAge at now.
func (r *ring) trim(now time.Time) {
	for r.n > 0 && now.Sub(r.at(0).at) > RingAge {
		r.buf[r.start] = ringItem{}
		r.start = (r.start + 1) % len(r.buf)
		r.n--
	}
}

// since is the frames after seq after, as a page with caps gets them, given that last is the channel's last seq; ok
// is false when the ring no longer holds all of them (or after is not a seq of this channel), and the page needs a
// snapshot.
func (r *ring) since(after, last int64, now time.Time, caps Caps) (frames [][]byte, ok bool) {
	if after < 0 || after > last {
		return nil, false
	}
	if after == last {
		return nil, true
	}
	r.trim(now)
	if r.n == 0 || r.at(0).seq > after+1 {
		return nil, false
	}
	for i := 0; i < r.n; i++ {
		if it := r.at(i); it.seq > after {
			frames = append(frames, it.forCaps(caps))
		}
	}
	return frames, true
}
