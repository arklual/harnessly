// Package transcriptindex reads what an agent's transcript gains once for everything that follows it. A transcript
// is one append-only file of JSON lines, and several parts of the runtime follow each: the conversation's feed, its
// background work, what the session says about itself. Each used to keep a cursor of its own, open the file and
// decode the same lines again. The index keeps one cursor per file: a look stats the file, reads the bytes it gained
// in bounded blocks, cuts them into whole lines, and hands each block to the file's subscribers while it is held. A
// line is decoded as JSON at most once, into the few fields the subscribers read (Fields), whoever asks first.
//
// The index holds nothing of a transcript: no line outlives the look that read it. A file is known only while it has
// subscribers, so a conversation that let go of its transcript (parked, deleted) costs the index nothing, and its
// file is never opened. The index reads no history: a subscriber reads its own start (a tail, a page of the past)
// and seats itself where that read ended; from there on the lines come from the index. A subscriber the index cannot
// serve in order — it could not take a block (a full Queue), the file was replaced or cut, its place is not one the
// index reads from — is lost: it reads from its own place itself and seats again. A loss is counted and logged.
//
// The index is driven by its subscribers' looks (Sync), not by a clock of its own: a look is a stat, and a look
// shortly after another subscriber's takes that one's word.
package transcriptindex

import (
	"bytes"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/transcriptindex/tally"
)

const (
	// DefaultBlock is how many bytes of a transcript one read takes; a longer line is read whole.
	DefaultBlock = 1 << 20
	// DefaultFresh is for how long a look that found a file as it was answers the other subscribers' looks at it.
	DefaultFresh = 500 * time.Millisecond
)

// State is what a look found for the subscriber that looked.
type State int

const (
	// Fresh: the subscriber has been handed every whole line up to the file's end.
	Fresh State = iota
	// Lost: the index does not serve the subscriber; it reads from its own place and seats again.
	Lost
	// Gone: the file is not there.
	Gone
)

// Sink takes the whole lines a transcript gained, in order, on the goroutine of whoever looked. The batch and
// everything reached through it are valid only during the call. It must be quick: the file's other subscribers wait
// for it. false says the batch could not be taken: the subscriber is lost from here on.
type Sink interface {
	Lines(b *Batch) bool
}

// SinkFunc is a function as a Sink.
type SinkFunc func(b *Batch) bool

func (f SinkFunc) Lines(b *Batch) bool { return f(b) }

// Options are the index's settings; the zero value is the defaults.
type Options struct {
	// Block is how many bytes one read takes (DefaultBlock).
	Block int
	// Fresh is for how long a look answers the other subscribers' unforced looks (DefaultFresh); negative: never.
	Fresh time.Duration
	// Warn logs a subscriber lost; nil logs nothing.
	Warn func(msg string, kv ...any)
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Counters are the index's own numbers.
type Counters struct {
	Files int // transcripts with a subscriber
	Subs  int // subscriptions
	// Looks counts the stats of transcripts, Opens the times a transcript was opened to read what it gained, Bytes
	// what was read; all since the runtime started.
	Looks, Opens, Bytes int64
	// Overflows counts the subscribers lost because they could not take a batch; Gaps, those whose place the index
	// could not read from; Resets, the files found replaced, cut or gone.
	Overflows, Gaps, Resets int64
}

// Index is the shared reader. Safe for concurrent use.
type Index struct {
	o     Options
	mu    sync.Mutex
	files map[string]*file

	overflows, gaps, resets atomic.Int64
	looks, opens, bytes     atomic.Int64
}

// New is an index with no file.
func New(o Options) *Index {
	if o.Block <= 0 {
		o.Block = DefaultBlock
	}
	if o.Fresh == 0 {
		o.Fresh = DefaultFresh
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Index{o: o, files: map[string]*file{}}
}

// Counters is the index's numbers now.
func (ix *Index) Counters() Counters {
	c := Counters{Overflows: ix.overflows.Load(), Gaps: ix.gaps.Load(), Resets: ix.resets.Load(),
		Looks: ix.looks.Load(), Opens: ix.opens.Load(), Bytes: ix.bytes.Load()}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	c.Files = len(ix.files)
	for _, f := range ix.files {
		f.mu.Lock()
		c.Subs += len(f.subs)
		f.mu.Unlock()
	}
	return c
}

// file is one transcript the index follows. mu makes its looks take turns and guards everything below it.
type file struct {
	path string
	mu   sync.Mutex
	subs []*Sub
	// seated: offset is a place a subscriber gave; before that the file has not been looked at.
	seated bool
	// offset is how far the file is read: always the start of a line.
	offset int64
	ino    uint64
	// tail is the size at which the file was last found to end in a line still being written: that line is not read
	// again until the file grows.
	tail int64
	// size is the file's size at the last stat; statAt and statBy, when that was and for whom.
	size   int64
	statAt time.Time
	statBy *Sub
}

// Sub is one subscriber's place in one transcript.
type Sub struct {
	ix   *Index
	f    *file
	name string
	sink Sink
	// next is the start of the first line the subscriber has not been handed; guarded by f.mu, as the flags are.
	next                 int64
	seated, lost, closed bool
	// passive: the subscriber does not keep the file followed; it is closed with the file's last other subscriber.
	passive bool
}

// Subscribe adds a subscriber of the transcript. It is handed nothing until it seats itself (Seat).
func (ix *Index) Subscribe(path, name string, sink Sink) *Sub {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	f := ix.files[path]
	if f == nil {
		f = &file{path: path}
		ix.files[path] = f
	}
	s := &Sub{ix: ix, f: f, name: name, sink: sink}
	f.mu.Lock()
	f.subs = append(f.subs, s)
	f.mu.Unlock()
	return s
}

// subscribePassive adds a subscriber to a transcript that is already followed, and none (nil) to one that is not: a
// passive subscriber rides on the reads made for the others and ends with the last of them.
func (ix *Index) subscribePassive(path, name string, sink Sink) *Sub {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	f := ix.files[path]
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.followed() {
		return nil
	}
	s := &Sub{ix: ix, f: f, name: name, sink: sink, passive: true}
	f.subs = append(f.subs, s)
	return s
}

// followed says the file has a subscriber that keeps it. Called with f.mu held.
func (f *file) followed() bool {
	for _, s := range f.subs {
		if !s.passive {
			return true
		}
	}
	return false
}

// Close ends the subscription. A file without subscribers is forgotten whole.
func (s *Sub) Close() {
	f := s.f
	f.mu.Lock()
	if s.closed {
		f.mu.Unlock()
		return
	}
	s.closed = true
	for i, x := range f.subs {
		if x == s {
			f.subs = append(f.subs[:i], f.subs[i+1:]...)
			break
		}
	}
	if f.statBy == s {
		f.statBy = nil
	}
	empty := !f.followed()
	f.mu.Unlock()
	if !empty {
		return
	}
	s.ix.mu.Lock()
	f.mu.Lock()
	if !f.followed() && s.ix.files[f.path] == f {
		// Nobody keeps the file: those who only rode along end here too.
		for _, p := range f.subs {
			p.closed = true
		}
		f.subs, f.statBy = nil, nil
		delete(s.ix.files, f.path)
	}
	f.mu.Unlock()
	s.ix.mu.Unlock()
}

// Closed says the subscription has ended: by Close, or with the file's last subscriber that kept it.
func (s *Sub) Closed() bool {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	return s.closed
}

// Seat says the subscriber has read the file up to offset, the start of a line, itself: the lines from there on are
// the index's to hand over. ino is the inode of the file the subscriber read, 0 when it does not know.
func (s *Sub) Seat(offset int64, ino uint64) {
	f := s.f
	f.mu.Lock()
	defer f.mu.Unlock()
	if s.closed {
		return
	}
	if ino != 0 && f.ino != 0 && ino != f.ino {
		// The subscriber read another file than the index follows: the index starts over on the subscriber's.
		s.ix.reset(f, "replaced")
	}
	if ino != 0 {
		f.ino = ino
	}
	if s.passive && f.seated && offset < f.offset {
		// The index does not read a stretch again for a reader that only rides along: it reads that stretch itself.
		s.seated = false
		return
	}
	s.next, s.seated, s.lost = offset, true, false
	if !f.seated || offset < f.offset {
		// A place before the index's is read again from there; those who have those lines skip them.
		f.offset, f.seated, f.tail = offset, true, 0
		f.statBy = nil
	}
}

// Sync looks at the file for the subscriber: what it gained is read and handed to every subscriber in order. force
// makes the look a stat of its own; without it a look another subscriber made within Options.Fresh answers. partial
// says the file ends in a line still being written past the subscriber's place.
func (s *Sub) Sync(force bool) (st State, partial bool) {
	f, ix := s.f, s.ix
	f.mu.Lock()
	defer f.mu.Unlock()
	if s.closed || !s.seated || s.lost {
		return Lost, false
	}
	now := ix.o.Now()
	if force || f.statBy == nil || f.statBy == s || ix.o.Fresh < 0 || now.Sub(f.statAt) >= ix.o.Fresh {
		info, err := os.Stat(f.path)
		tally.Stat()
		ix.looks.Add(1)
		if err != nil {
			if f.ino != 0 {
				ix.reset(f, "gone")
			} else {
				// A file the index never saw is not one that went away: its readers ask again and again while it is
				// not there, and that is neither counted nor logged.
				f.forget()
			}
			return Gone, false
		}
		ino := inode(info)
		if f.ino == 0 {
			f.ino = ino
		}
		if ino != f.ino || info.Size() < f.offset {
			ix.reset(f, "replaced or cut")
			f.ino = ino
			return Lost, false
		}
		f.size, f.statAt, f.statBy = info.Size(), now, s
		ix.read(f)
	}
	if s.lost {
		return Lost, false
	}
	return Fresh, f.size > max(f.offset, s.next)
}

// reset loses every subscriber of a file that is not the one they read: each reads its own start again.
func (ix *Index) reset(f *file, why string) {
	ix.resets.Add(1)
	f.forget()
	if ix.o.Warn != nil {
		ix.o.Warn("transcript index: file starts over, its readers read it themselves", "path", f.path, "why", why, "resets", ix.resets.Load())
	}
}

// forget loses the file's subscribers and what the index knew of the file. The caller holds f.mu.
func (f *file) forget() {
	for _, s := range f.subs {
		s.lost = true
	}
	f.seated, f.offset, f.ino, f.size, f.tail, f.statBy = false, 0, 0, 0, 0, nil
}

func (ix *Index) lose(s *Sub, why string, n *atomic.Int64) {
	s.lost = true
	n.Add(1)
	if ix.o.Warn != nil {
		ix.o.Warn("transcript index: reader lost, it reads from its own place", "path", s.f.path, "reader", s.name, "why", why,
			"overflows", ix.overflows.Load(), "gaps", ix.gaps.Load())
	}
}

// read takes what the file gained up to f.size and hands it over, block by block. Called with f.mu held.
func (ix *Index) read(f *file) {
	// Bytes nobody is waiting for are not read.
	lo, wanted := int64(0), false
	for _, s := range f.subs {
		if s.seated && !s.lost && (!wanted || s.next < lo) {
			lo, wanted = s.next, true
		}
	}
	if !wanted {
		return
	}
	if lo > f.offset {
		f.offset = lo
	}
	if f.size <= f.offset || f.size == f.tail {
		return
	}
	fh, err := os.Open(f.path)
	tally.Open()
	ix.opens.Add(1)
	if err != nil {
		return
	}
	defer fh.Close()
	block := int64(ix.o.Block)
	var buf []byte
	for f.offset < f.size {
		n := min(block, f.size-f.offset)
		if int64(cap(buf)) < n {
			buf = make([]byte, n)
		}
		got, _ := fh.ReadAt(buf[:n], f.offset)
		tally.Read(got)
		ix.bytes.Add(int64(got))
		complete := bytes.LastIndexByte(buf[:got], '\n') + 1
		if complete == 0 {
			if int64(got) < n || f.offset+n >= f.size {
				f.tail = f.size // the last line is still being written
				return
			}
			block *= 2 // a line longer than the block is read whole
			continue
		}
		blk := &span{path: f.path, base: f.offset, data: buf[:complete]}
		for _, s := range f.subs {
			ix.hand(s, blk)
		}
		f.offset += int64(complete)
		block = int64(ix.o.Block)
	}
}

// hand gives a block to a subscriber from the subscriber's place on.
func (ix *Index) hand(s *Sub, blk *span) {
	to := blk.base + int64(len(blk.data))
	switch {
	case !s.seated || s.lost || to <= s.next:
		return
	case s.passive:
		// A reader that only rides along is not lost, counted or logged: where it cannot be served in order it
		// stands aside until it seats again.
		if blk.base > s.next || blk.base < s.next && blk.data[s.next-blk.base-1] != '\n' ||
			!s.sink.Lines(&Batch{Path: blk.path, From: s.next, To: to, blk: blk}) {
			s.seated = false
			return
		}
		s.next = to
		return
	case blk.base > s.next:
		ix.lose(s, "its place is before what the index reads", &ix.gaps)
		return
	case blk.base < s.next && blk.data[s.next-blk.base-1] != '\n':
		ix.lose(s, "its place is inside a line", &ix.gaps)
		return
	}
	if !s.sink.Lines(&Batch{Path: blk.path, From: s.next, To: to, blk: blk}) {
		ix.lose(s, "it could not take the lines", &ix.overflows)
		return
	}
	s.next = to
}

func inode(st os.FileInfo) uint64 {
	if s, ok := st.Sys().(*syscall.Stat_t); ok {
		return uint64(s.Ino)
	}
	return 0
}
