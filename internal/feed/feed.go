// Package feed is a conversation's feed on top of the agent's transcript: entries with ids that come from the
// transcript itself (a re-read and an incremental read agree), cut into turns by the runtime, read in pages by turn,
// and the signals in it that move the agent's state (a question asked, a gate resolved, a turn ended).
//
// The transcript is read by internal/transcript (Claude and Codex); the feed only reads its timeline entries, through
// Source, so a test gives it files from testdata.
package feed

import (
	"math"
	"runtime"
	"slices"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/transcript"
	"github.com/ly-solutions/harnessly/internal/transcriptindex"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Source reads one transcript.
type Source interface {
	// Tail is the last max entries; it sets where Since goes on from.
	Tail(max int) transcript.Chunk
	// Before is up to max entries that end before the byte.
	Before(beforeByte int64, max int) transcript.Chunk
	// Since is what the transcript gained after the last read.
	Since() transcript.Incremental
}

// windowed is a Source that also reads by bytes from the end (Claude): the first read takes the last bytes of the
// transcript, however few entries they hold, so a transcript of large lines (tool outputs, pictures) shows its newest
// turns without being parsed from far back; older bytes are read when the person scrolls up to them.
type windowed interface {
	// TailWindow is the last max entries of the last window bytes; it sets where Since goes on from.
	TailWindow(window int64, max int) transcript.Chunk
	// BeforeWindow is the last max entries whose lines start in the window bytes before the byte.
	BeforeWindow(beforeByte, window int64, max int) transcript.Chunk
}

// seated is a Source whose place for Since can be written down and set again without a read: what lets a feed at rest
// come back after a restart of the runtime with its transcript unread (Restore).
type seated interface {
	// AtRest says Since from the source's place needs nothing the source holds beyond that place.
	AtRest() bool
	// Seat sets where Since goes on from.
	Seat(offset int64)
}

// ClaudeSource reads a Claude Code transcript.
type ClaudeSource struct {
	Path string
	// Index, when set, reads what the transcript gains for the source and for the transcript's other readers at once;
	// nil has the source read the file itself.
	Index   *transcriptindex.Index
	offset  int64
	pending string
	carry   transcript.Carry
	// q is the source's place in the index; nil until the first Since.
	q *transcriptindex.Queue
}

func (s *ClaudeSource) Tail(max int) transcript.Chunk {
	c := transcript.ReadTail(s.Path, max)
	s.offset, s.pending, s.carry = c.FileSize, "", c.Carry
	s.seat()
	return c
}

func (s *ClaudeSource) Before(beforeByte int64, max int) transcript.Chunk {
	return transcript.ReadBefore(s.Path, beforeByte, max)
}

func (s *ClaudeSource) TailWindow(window int64, max int) transcript.Chunk {
	c := transcript.ReadTailBytes(s.Path, window, max)
	s.offset, s.pending, s.carry = c.FileSize, "", c.Carry
	s.seat()
	return c
}

func (s *ClaudeSource) BeforeWindow(beforeByte, window int64, max int) transcript.Chunk {
	return transcript.ReadBeforeBytes(s.Path, beforeByte, window, max)
}

// AtRest says a read that starts at the source's place with nothing held reads what this one would.
func (s *ClaudeSource) AtRest() bool { return s.pending == "" && !s.carry.Open() }

// Seat makes Since go on from offset with nothing held.
func (s *ClaudeSource) Seat(offset int64) {
	s.offset, s.pending, s.carry = offset, "", transcript.Carry{}
	s.seat()
}

func (s *ClaudeSource) Since() transcript.Incremental {
	if inc, ok := s.sinceIndexed(); ok {
		return inc
	}
	inc := transcript.ParseIncrementalCarry(s.Path, s.offset, s.pending, s.carry)
	s.offset, s.pending, s.carry = inc.NewOffset, inc.PendingBuffer, inc.Carry
	s.seat()
	return inc
}

// sinceIndexed is Since off the index: the whole lines the transcript gained after the source's place, read once for
// all of the transcript's readers. false leaves the read to the source: it has no place in the index yet, it holds
// the start of a line, the index lost it, or the file ends in a line still being written (which a read of the file
// may take whole).
func (s *ClaudeSource) sinceIndexed() (transcript.Incremental, bool) {
	if s.Index == nil || s.pending != "" || s.q == nil {
		return transcript.Incremental{}, false
	}
	data, from, partial, st := s.q.Take()
	if st != transcriptindex.Fresh || len(data) > 0 && from != s.offset {
		return transcript.Incremental{}, false
	}
	if len(data) == 0 {
		if partial {
			return transcript.Incremental{}, false
		}
		return transcript.Incremental{NewEntries: []*jsvalue.Object{}, NewOffset: s.offset, Carry: s.carry}, true
	}
	inc := transcript.ParseAppended(data, s.offset, s.carry)
	if inc.NewOffset == s.offset {
		// The lines cannot be read, by a read of the file either: the source stays where it is, as it does without
		// the index, and reads the file itself when the transcript grows again.
		return inc, true
	}
	s.offset, s.carry = inc.NewOffset, inc.Carry
	return inc, true
}

// seat tells the index where the source stands after a read of its own. A source that holds the start of a line has
// no place to give: it reads by itself until the line is whole.
func (s *ClaudeSource) seat() {
	if s.Index == nil || s.pending != "" {
		return
	}
	if s.q == nil {
		s.q = s.Index.Queue(s.Path, "feed", 0)
		// A source dropped without Close lets go of the transcript all the same.
		runtime.AddCleanup(s, func(q *transcriptindex.Queue) { q.Close() }, s.q)
	}
	s.q.Seat(s.offset)
}

// Close lets go of the source's place in the index: the transcript is not read for it any more. A later read of the
// source takes a place again.
func (s *ClaudeSource) Close() {
	if s.q != nil {
		s.q.Close()
		s.q = nil
	}
}

// CodexSource reads a Codex transcript.
type CodexSource struct {
	Path string
	// Index, when set, reads what the transcript gains for the source and for the transcript's other readers at once;
	// nil has the source read the file itself.
	Index *transcriptindex.Index
	p     *transcript.CodexParser
	// q is the source's place in the index; nil until the first read.
	q *transcriptindex.Queue
}

func (s *CodexSource) parser() *transcript.CodexParser {
	if s.p == nil {
		s.p = transcript.NewCodexParser(s.Path, transcript.CodexOptions{})
	}
	return s.p
}

func (s *CodexSource) Tail(max int) transcript.Chunk {
	c := s.parser().ParseTail(max)
	s.seat()
	return c
}

func (s *CodexSource) Before(beforeByte int64, max int) transcript.Chunk {
	return transcript.ReadCodexBefore(s.Path, beforeByte, max, -1, transcript.CodexOptions{})
}

func (s *CodexSource) Since() transcript.Incremental {
	if inc, ok := s.sinceIndexed(); ok {
		return inc
	}
	inc := s.parser().ParseIncremental()
	s.seat()
	return inc
}

// sinceIndexed is Since off the index, as ClaudeSource's: false leaves the read to the parser.
func (s *CodexSource) sinceIndexed() (transcript.Incremental, bool) {
	if s.Index == nil || s.q == nil || s.p == nil || s.p.Holding() {
		return transcript.Incremental{}, false
	}
	data, from, partial, st := s.q.Take()
	if st != transcriptindex.Fresh || len(data) > 0 && from != s.p.Offset() || len(data) == 0 && partial {
		return transcript.Incremental{}, false
	}
	return s.p.ParseAppended(data, from)
}

// seat tells the index where the source stands after a read of its own; a parser that holds the start of a line has no
// place to give.
func (s *CodexSource) seat() {
	if s.Index == nil || s.p == nil || s.p.Holding() {
		return
	}
	if s.q == nil {
		s.q = s.Index.Queue(s.Path, "feed", 0)
		runtime.AddCleanup(s, func(q *transcriptindex.Queue) { q.Close() }, s.q)
	}
	s.q.Seat(s.p.Offset())
}

// Close lets go of the source's place in the index.
func (s *CodexSource) Close() {
	if s.q != nil {
		s.q.Close()
		s.q = nil
	}
}

func (s *CodexSource) AtRest() bool { return s.p != nil && s.p.AtRest() }

func (s *CodexSource) Seat(offset int64) {
	s.parser().Seat(offset)
	s.seat()
}

// TailEntries is how many timeline entries the first read takes from the end at most; older ones come page by page.
const TailEntries = 2000

// TailBytes is how much of a transcript read by bytes the first read takes from its end, and each read of older
// bytes before what the feed holds; a window that holds fewer than TailTurns turns is doubled until it does.
const TailBytes = 4 << 20

// TailTurns is how many turns the first read wants whole: the last three a snapshot holds with their steps, and one
// more, so the newest turns are never cut at their start.
const TailTurns = 4

// Update is what a read added to the feed.
type Update struct {
	// Steps are the new entries in order, each with its turn's counters after it.
	Steps []wire.StepEv
	// Updated are entries changed in place (rev+1).
	Updated []wire.Entry
	// Started and Ended are the turns that opened and ended, as their records are now.
	Started []wire.Turn
	Ended   []wire.Turn
	Signals []Signal
	// Events are all of the above as the conversation's channel takes them, in the transcript's order: turn_started,
	// step, entry_update, and turn_finished of a turn the transcript ended.
	Events []wire.Event
	// Read are the source entries the read took, in the transcript's order, for what is gathered from the same read
	// beside the feed. Whole says they are all the feed holds now (a first read, a transcript read anew), not what
	// it gained.
	Read  []*jsvalue.Object
	Whole bool
}

// Feed is one conversation's feed. Not safe for concurrent use: the conversation's actor owns it.
type Feed struct {
	conv     string
	provider wire.Provider
	src      Source
	b        *builder
	// tail is how many entries one read of the past takes; window, how many bytes (a windowed source).
	tail   int
	window int64
	// raws are the source entries read, with the offset each was read at: a page of older ones builds the feed
	// again over all of them, ids do not move.
	raws []raw
	// clients and ends are what the runtime set on the feed beyond the transcript; a rebuild keeps them.
	clients map[string]string
	ends    map[string]end
	// vendor: the chat runs on a backend profile (another vendor's API behind Claude Code), so the notices of its
	// usage limit are the vendor's words, not Claude's.
	vendor bool
	// start is where the read entries start in the file; more: there are older ones.
	start  int64
	more   bool
	size   int64
	loaded bool
	// rel is what the feed keeps while released (release.go); uses counts its reads beyond a look's summary.
	rel  *releasedFeed
	uses int
	// builds counts the builds over all raws (rebuild): each costs the whole feed read.
	builds int
}

type raw struct {
	o      *jsvalue.Object
	offset int64
}

type end struct {
	status  wire.TurnStatus
	failure *wire.TurnFailure
	at      wire.Ms
}

// New is the feed of a conversation over its transcript source.
func New(conv string, provider wire.Provider, src Source) *Feed {
	return &Feed{conv: conv, provider: provider, src: src, tail: TailEntries, window: TailBytes, b: newBuilder(conv, provider),
		clients: map[string]string{}, ends: map[string]end{}}
}

// OnVendor says the chat runs on a backend profile: a usage limit is read off the vendor's words only.
func (f *Feed) OnVendor() {
	f.vendor, f.b.vendor = true, true
}

// Everything is a feed that reads a transcript of size bytes whole, in one read: for what wants all the messages of a
// conversation (the search index), not for a page of turns.
func Everything(conv string, provider wire.Provider, src Source, size int64) *Feed {
	f := New(conv, provider, src)
	f.tail, f.window = math.MaxInt32, max(size, TailBytes)+1
	return f
}

// Load reads the transcript's tail. Its signals are the state of the transcript as it is now (an open question, an
// open turn): the caller weighs them as a look at the past, not as news.
//
// A windowed source is read from its end by bytes: the window doubles until it holds TailTurns turns, TailEntries
// entries or the whole file, so the newest turns are there at once however large the transcript is.
func (f *Feed) Load() Update {
	f.rel = nil
	w, ok := f.src.(windowed)
	if !ok {
		return f.loadFrom(f.src.Tail(f.tail))
	}
	for window := f.window; ; window *= 2 {
		c := w.TailWindow(window, f.tail)
		u := f.loadFrom(c)
		if !c.HasMore || len(c.Entries) >= f.tail || len(f.b.turns)-f.lead() >= TailTurns {
			return u
		}
	}
}

func (f *Feed) loadFrom(c transcript.Chunk) Update {
	f.start, f.more, f.size, f.loaded = c.StartByteOffset, c.HasMore, c.FileSize, true
	f.raws = nil
	for _, o := range c.Entries {
		f.raws = append(f.raws, raw{o, -1})
	}
	u := f.rebuild()
	u.Read, u.Whole = c.Entries, true
	return u
}

// Poll reads what the transcript gained since the last read. The entries' offset is where the read began: their
// lines start there or later.
func (f *Feed) Poll() Update {
	if !f.loaded {
		return f.Load()
	}
	if f.rel != nil {
		u, _ := f.pollReleased()
		return u
	}
	from := f.size
	inc := f.src.Since()
	if inc.NewOffset < from {
		// The file was cut or replaced: the feed starts over on what it holds now.
		return f.Load()
	}
	f.size = inc.NewOffset - int64(len(inc.PendingBuffer))
	u := Update{Read: inc.NewEntries}
	for _, o := range inc.NewEntries {
		f.raws = append(f.raws, raw{o, from})
		f.apply(&u, f.b.add(o, from))
	}
	return u
}

// Sources are the source entries the feed holds, oldest first: what its reads took, for what starts to be gathered
// beside the feed after them. Nothing is read from the transcript beyond what a released feed takes back.
func (f *Feed) Sources() []*jsvalue.Object {
	if !f.loaded {
		return nil
	}
	f.ensure()
	out := make([]*jsvalue.Object, len(f.raws))
	for i, r := range f.raws {
		out[i] = r.o
	}
	return out
}

// rebuild builds the feed over the raws read, with what the runtime set on it.
func (f *Feed) rebuild() Update {
	f.builds++
	f.b = newBuilder(f.conv, f.provider)
	f.b.vendor = f.vendor
	var u Update
	for _, r := range f.raws {
		f.apply(&u, f.b.add(r.o, r.offset))
	}
	for entryID, clientID := range f.clients {
		if i, ok := f.b.byID[entryID]; ok {
			f.b.entries[i].ClientID = sptr(clientID)
		}
	}
	for id, e := range f.ends {
		f.end(id, e)
	}
	return u
}

func (f *Feed) apply(u *Update, s step) {
	// One source entry ends a turn before it opens the next, and opens it before its first step.
	for _, id := range s.ended {
		t := f.b.turns[f.b.byTurn[id]].rec
		u.Ended = append(u.Ended, t)
		u.Events = append(u.Events, wire.TurnFinishedEv{Turn: t})
	}
	for _, id := range s.started {
		t := f.b.turns[f.b.byTurn[id]].rec
		u.Started = append(u.Started, t)
		u.Events = append(u.Events, wire.TurnStartedEv{Turn: t})
	}
	for _, i := range s.added {
		e := f.b.entries[i]
		st := wire.StepEv{Entry: e, Counts: f.b.counts(e)}
		u.Steps = append(u.Steps, st)
		u.Events = append(u.Events, st)
	}
	for _, i := range s.updated {
		e := f.b.entries[i]
		u.Updated = append(u.Updated, e)
		u.Events = append(u.Events, wire.EntryUpdateEv{Entry: e})
	}
	// A turn that took lines after it ended has new counters and maybe an answer: its record goes out again.
	for _, id := range s.regrown {
		t := f.b.turns[f.b.byTurn[id]].rec
		if t.Status == wire.TurnFailed {
			u.Events = append(u.Events, wire.TurnFailedEv{Turn: t})
		} else {
			u.Events = append(u.Events, wire.TurnFinishedEv{Turn: t})
		}
	}
	u.Signals = append(u.Signals, s.signals...)
}

// PendingTool is the open turn's latest tool call still without its result: the call a permission prompt is about.
func (f *Feed) PendingTool() (wire.Entry, bool) {
	if f.rel != nil {
		return wire.Entry{}, false // a released feed has no open turn
	}
	id := f.b.openID()
	if id == "" {
		return wire.Entry{}, false
	}
	t := f.b.turns[f.b.byTurn[id]]
	for i := len(t.entries) - 1; i >= 0; i-- {
		if e := f.b.entries[f.b.byID[t.entries[i]]]; e.T == wire.EntryTool && e.OK == nil && e.ToolUseID != "" {
			return e, true
		}
	}
	return wire.Entry{}, false
}

// Close lets go of what the feed's source holds outside the feed (its place in the transcript index). The feed stays
// usable: its next read takes a place again.
func (f *Feed) Close() {
	if c, ok := f.src.(interface{ Close() }); ok {
		c.Close()
	}
}

// Size is how far the transcript is read: an entry written after it is newer than anything the feed has.
func (f *Feed) Size() int64 { return f.size }

// Entry is one entry by its id.
func (f *Feed) Entry(id string) (wire.Entry, bool) {
	f.ensure()
	i, ok := f.b.byID[id]
	if !ok {
		return wire.Entry{}, false
	}
	return f.b.entries[i], true
}

// Messages calls fn with each of the person's messages and the agent's answers (user and say entries), oldest first.
func (f *Feed) Messages(fn func(wire.Entry)) {
	f.ensure()
	f.held(fn)
}

// Held is Messages over what the feed holds now, without reading a released feed again or counting a use: a look the
// actor makes by itself on the clock must not keep an idle chat's transcript in memory. False when released.
func (f *Feed) Held(fn func(wire.Entry)) bool {
	if f.rel != nil {
		return false
	}
	f.held(fn)
	return true
}

func (f *Feed) held(fn func(wire.Entry)) {
	for _, e := range f.b.entries {
		if e.T == wire.EntryUser || e.T == wire.EntrySay {
			fn(e)
		}
	}
}

// SetClientID marks a user entry as the page's message.
func (f *Feed) SetClientID(entryID, clientID string) (wire.Entry, bool) {
	f.ensure()
	i, ok := f.b.byID[entryID]
	if !ok || f.b.entries[i].T != wire.EntryUser {
		return wire.Entry{}, false
	}
	e := f.b.entries[i]
	if e.ClientID != nil && *e.ClientID == clientID {
		return e, false
	}
	e.ClientID, e.Rev = sptr(clientID), e.Rev+1
	f.b.entries[i] = e
	f.clients[entryID] = clientID
	return e, true
}

// Context is the tokens the agent's last answer was asked with, 0 when its transcript does not say.
func (f *Feed) Context() int64 {
	if f.rel != nil {
		return f.rel.context
	}
	return f.b.context
}

// Model is the model the agent last answered with, "" when its transcript does not say.
func (f *Feed) Model() string {
	if f.rel != nil {
		return f.rel.model
	}
	return f.b.model
}

// OpenTurn is the turn still running in the transcript, "" none.
func (f *Feed) OpenTurn() string {
	if f.rel != nil {
		return ""
	}
	return f.b.openID()
}

// Turn is a turn's record by its id.
func (f *Feed) Turn(id string) (wire.Turn, bool) {
	f.ensure()
	i, ok := f.b.byTurn[id]
	if !ok {
		return wire.Turn{}, false
	}
	return f.b.turns[i].rec, true
}

// EndTurn ends a turn the transcript left open (a stop hook ahead of its line, a failure, an interrupt): the record
// as it is now. Ending one already ended changes nothing, but a failure the transcript ended as done.
func (f *Feed) EndTurn(id string, st wire.TurnStatus, failure *wire.TurnFailure, at wire.Ms) (wire.Turn, bool) {
	f.ensure()
	i, ok := f.b.byTurn[id]
	if !ok {
		return wire.Turn{}, false
	}
	t := f.b.turns[i]
	if t.rec.Status != wire.TurnLive && !(st == wire.TurnFailed && t.rec.Status == wire.TurnDone) {
		return t.rec, false
	}
	e := end{st, failure, at}
	f.ends[id] = e
	f.end(id, e)
	return f.b.turns[i].rec, true
}

// Refail names again why a failed turn failed: its last lines came after it ended (Claude fires StopFailure before its
// refusal is on the transcript). The record as it is now; false when the turn did not fail.
func (f *Feed) Refail(id string, failure *wire.TurnFailure) (wire.Turn, bool) {
	f.ensure()
	i, ok := f.b.byTurn[id]
	if !ok || f.b.turns[i].rec.Status != wire.TurnFailed {
		return wire.Turn{}, false
	}
	if e, ok := f.ends[id]; ok {
		e.failure = failure
		f.ends[id] = e
	}
	f.b.turns[i].rec.Failure = failure
	return f.b.turns[i].rec, true
}

func (f *Feed) end(id string, e end) {
	i, ok := f.b.byTurn[id]
	if !ok {
		return
	}
	t := f.b.turns[i]
	if f.b.open == t {
		at := e.at
		// A failure is noticed later than it happened (the agent exited, the runtime saw it on its next look, maybe
		// days on): the turn worked until its last line.
		if n := len(t.entries); e.status == wire.TurnFailed && n > 0 {
			if last := f.b.entries[f.b.byID[t.entries[n-1]]].At; last < at {
				at = last
			}
		}
		var s step
		f.b.endTurn(&s, at, e.status)
		f.b.late = t // the transcript has not ended it: its last lines are still to come
	}
	if t.rec.Status == wire.TurnLive || e.status == wire.TurnFailed {
		t.rec.Status = e.status
	}
	if e.status == wire.TurnFailed {
		t.rec.Failure = e.failure
	}
}

// ErrorEnd is the error a turn ends on ("" the open one): its last line is the agent's own error notice (a call to
// its model refused: the login expired, the service failed). A turn that stops on it answered nothing, it failed.
func (f *Feed) ErrorEnd(id string) (string, bool) {
	f.ensure()
	if id == "" {
		id = f.b.openID()
	}
	i, ok := f.b.byTurn[id]
	if !ok {
		return "", false
	}
	t := f.b.turns[i]
	if len(t.entries) == 0 {
		return "", false
	}
	e, ok := f.Entry(t.entries[len(t.entries)-1])
	if !ok || e.T != wire.EntryNotice || e.Code != "agent-notice" || e.Level != wire.NoticeError || e.Detail == nil {
		return "", false
	}
	return *e.Detail, true
}

// Resume opens again a turn the runtime ended on silence that the transcript has not ended: the agent was working
// all along (its spinner off the screen) and writes on. The record as it is now; false when it is not that turn.
func (f *Feed) Resume(id string) (wire.Turn, bool) {
	f.ensure()
	i, ok := f.b.byTurn[id]
	if !ok || f.b.late != f.b.turns[i] || f.b.open != nil {
		return wire.Turn{}, false
	}
	t := f.b.turns[i]
	if e, ok := f.ends[id]; !ok || e.status == wire.TurnFailed {
		return wire.Turn{}, false
	}
	delete(f.ends, id)
	t.rec.Status, t.rec.EndedAt, t.rec.DurationMs = wire.TurnLive, nil, nil
	f.b.open, f.b.late = t, nil
	return t.rec, true
}

// FindUser is the first user entry whose line is at or after offset, or (an entry read before its offset was known)
// written at or after at: the message typed then.
func (f *Feed) FindUser(offset int64, at wire.Ms) (string, bool) {
	f.ensure()
	for i, e := range f.b.entries {
		if e.T != wire.EntryUser || e.ClientID != nil {
			continue
		}
		if off := f.b.offsets[i]; off >= 0 && off >= offset || off < 0 && e.At >= at-clockSkew {
			return e.ID, true
		}
	}
	return "", false
}

// FindUserText is FindUser for one message: the first user entry at or after offset (or written at or after at) that
// carries its text, or its start when Claude cut the paste short. An entry of someone else — the person typing into the terminal, another tool — is never taken for it.
func (f *Feed) FindUserText(offset int64, at wire.Ms, text string) (string, bool) {
	f.ensure()
	for i, e := range f.b.entries {
		if line := commandLineOf(e); line != "" {
			// A command the person typed («! cmd», «/compact») is written as its command entry, not a user message.
			if !screen.SameMessage(line, text) {
				continue
			}
		} else if e.T != wire.EntryUser || e.ClientID != nil || !screen.SameMessage(e.Text, text) && !screen.CutShort(e.Text, text) {
			continue
		}
		if off := f.b.offsets[i]; off >= 0 && off >= offset || off < 0 && e.At >= at-clockSkew {
			return e.ID, true
		}
	}
	return "", false
}

// clockSkew: the transcript's timestamps and the runtime's clock are the same machine's, a line is written a moment
// after the Enter.
const clockSkew = 2000

// Snapshot is the last turns as a conv snapshot holds them: up to turns full records, oldest first, the steps of the
// last three and the answer entry of each older one; before is the cursor for older turns.
func (f *Feed) Snapshot(turns int) (ts []wire.Turn, es []wire.Entry, before *string) {
	f.ensure()
	return f.page(len(f.b.turns), turns, 3)
}

// lead is the index of the first turn the feed holds for sure: when the read began in the middle of the file, the
// turns before the first one a typed message opened may be the middle of a turn whose start is further back (a
// message delivered into it, a task's notification), and are not shown until that start is read.
func (f *Feed) lead() int {
	if !f.more {
		return 0
	}
	for i, t := range f.b.turns {
		if t.sure {
			return i
		}
	}
	return len(f.b.turns)
}

// page is the turns [end-limit, end) with the steps of the last full of them; entries outside any turn that fall
// within the page go with it.
func (f *Feed) page(end, limit, full int) ([]wire.Turn, []wire.Entry, *string) {
	from := min(end, max(f.lead(), end-limit))
	ts := []wire.Turn{}
	es := []wire.Entry{}
	keep := map[string]bool{}
	for i := from; i < end; i++ {
		t := f.b.turns[i]
		ts = append(ts, t.rec)
		if i >= end-full {
			for _, id := range t.entries {
				keep[id] = true
			}
		} else {
			// A turn read by its face: what opened it (the person's message, a teammate's note) and its answer.
			keep[t.rec.ID] = true
			if t.rec.AnswerEntryID != nil {
				keep[*t.rec.AnswerEntryID] = true
			}
		}
	}
	// at is where turn i's entries begin, the end of the entries for the turn after the last.
	at := func(i int) int {
		if i < len(f.b.turns) {
			return f.b.byID[f.b.turns[i].rec.ID]
		}
		return len(f.b.entries)
	}
	lo, hi := 0, at(end)
	// The entries before the first turn read are the end of a turn whose start is further back (the read began in
	// the middle of it): they join it when it is read, and are not shown as lines outside any turn meanwhile.
	if from > 0 || f.more {
		lo = at(from)
	}
	for _, e := range f.b.entries[lo:hi] {
		if keep[e.ID] || e.TurnID == nil {
			es = append(es, e)
		}
	}
	var before *string
	if (from > 0 || f.more) && from < len(f.b.turns) {
		before = sptr(f.b.turns[from].rec.ID)
	}
	return ts, es, before
}

// After is what follows an entry: the entries after it, and every turn from the entry's own on, as they are now. The
// turns go along whether or not an entry after the cursor touches them: a page that had the chat from elsewhere (the
// cluster's copy) saw the entry's turn running, and has to learn that it ended, or that a new turn began
// with nothing in it yet. ok is false when the feed does not have the entry.
func (f *Feed) After(entryID string) (ts []wire.Turn, es []wire.Entry, ok bool) {
	f.ensure()
	i, has := f.b.byID[entryID]
	if !has {
		return nil, nil, false
	}
	es = append([]wire.Entry{}, f.b.entries[i+1:]...)
	from := len(f.b.turns)
	if id := f.b.entries[i].TurnID; id != nil {
		if at, ok := f.b.byTurn[*id]; ok {
			from = at
		}
	}
	for _, e := range es {
		if e.TurnID != nil {
			if at, ok := f.b.byTurn[*e.TurnID]; ok && at < from {
				from = at
			}
		}
	}
	// A loose entry with nothing after it: the newest turn still says whether the chat is at work.
	if from == len(f.b.turns) && from > 0 {
		from--
	}
	ts = make([]wire.Turn, 0, len(f.b.turns)-from)
	for _, t := range f.b.turns[from:] {
		ts = append(ts, t.rec)
	}
	return ts, es, true
}

// Turns is the page of turns before a turn ("" from the end), at most limit, oldest first, with their answer
// entries. Older turns than the feed holds are read from the transcript first: a turn named that the feed does not
// hold now (a cursor kept from before the runtime restarted, older than the tail it read) is looked for there too,
// so a walk through the chat goes on from where it stopped.
func (f *Feed) Turns(before string, limit int) (wire.TurnsPage, bool) {
	return f.turns(before, limit, false)
}

// TurnsWithSteps is the page Turns gives with the steps of every turn in it, not only of the newest: what the
// cluster's copy is backfilled from, so a folded turn opens from the copy too.
func (f *Feed) TurnsWithSteps(before string, limit int) (wire.TurnsPage, bool) {
	return f.turns(before, limit, true)
}

func (f *Feed) turns(before string, limit int, steps bool) (wire.TurnsPage, bool) {
	f.ensure()
	end := len(f.b.turns)
	if before != "" {
		if _, ok := f.b.byTurn[before]; !ok && !f.reach(before) {
			return wire.TurnsPage{}, false
		}
		end = f.b.byTurn[before]
	}
	for end < limit && f.more {
		if f.older() == 0 && !f.more {
			break
		}
		end = len(f.b.turns)
		if before != "" {
			end = f.b.byTurn[before]
		}
	}
	full := 0
	if steps {
		full = limit
	}
	ts, es, cursor := f.page(end, limit, full)
	return wire.TurnsPage{Turns: ts, Entries: es, BeforeTurnID: cursor}, true
}

// reach reads what precedes the feed until it holds the turn; whether it does. The older windows are gathered as read
// and the feed is built again only when what it holds has doubled: a build per window made the walk to a cursor deep
// in a transcript of 100 MB quadratic, on the chat's actor.
func (f *Feed) reach(turn string) bool {
	var read [][]raw // newest window first
	n, built := 0, len(f.raws)
	add := func() {
		if n == 0 {
			return
		}
		raws := make([]raw, 0, n+len(f.raws))
		for i := len(read) - 1; i >= 0; i-- {
			raws = append(raws, read[i]...)
		}
		f.raws = append(raws, f.raws...)
		read, n = nil, 0
		f.rebuild()
		built = len(f.raws)
	}
	for f.more {
		c := f.before()
		if len(c.Entries) == 0 {
			f.more = false
			break
		}
		window := make([]raw, len(c.Entries))
		for i, o := range c.Entries {
			window[i] = raw{o, -1}
		}
		read, n = append(read, window), n+len(window)
		f.start, f.more = c.StartByteOffset, c.HasMore
		if n < built && f.more {
			continue
		}
		add()
		if _, ok := f.b.byTurn[turn]; ok {
			return true
		}
	}
	add()
	_, ok := f.b.byTurn[turn]
	return ok
}

// older reads the page of the transcript before what the feed holds and builds the feed again over both; the
// number of turns it gained.
func (f *Feed) older() int {
	c := f.before()
	if len(c.Entries) == 0 {
		f.more = false
		return 0
	}
	had := len(f.b.turns)
	raws := make([]raw, 0, len(c.Entries)+len(f.raws))
	for _, o := range c.Entries {
		raws = append(raws, raw{o, -1})
	}
	f.raws = append(raws, f.raws...)
	f.start, f.more = c.StartByteOffset, c.HasMore
	f.rebuild()
	return len(f.b.turns) - had
}

// before is the read of what precedes the feed: the window of bytes before it (doubled while no line fits in it) for
// a windowed source, else up to tail entries.
func (f *Feed) before() transcript.Chunk {
	w, ok := f.src.(windowed)
	if !ok {
		return f.src.Before(f.start, f.tail)
	}
	for window := f.window; ; window *= 2 {
		c := w.BeforeWindow(f.start, window, f.tail)
		if len(c.Entries) > 0 || !c.HasMore {
			return c
		}
	}
}

// TurnEntries is every entry of one turn.
func (f *Feed) TurnEntries(turnID string) (wire.EntriesPage, bool) {
	f.ensure()
	i, ok := f.b.byTurn[turnID]
	if !ok {
		return wire.EntriesPage{}, false
	}
	es := []wire.Entry{}
	for _, id := range f.b.turns[i].entries {
		es = append(es, f.b.entries[f.b.byID[id]])
	}
	return wire.EntriesPage{TurnID: turnID, Entries: es}, true
}

// Output is the whole output of a tool entry.
func (f *Feed) Output(entryID string) (wire.OutputPage, bool) {
	f.ensure()
	e, ok := f.Entry(entryID)
	if !ok || e.T != wire.EntryTool {
		return wire.OutputPage{}, false
	}
	return wire.OutputPage{EntryID: entryID, Output: wire.ToolOutput{Text: f.b.outputs[entryID]}}, true
}

// Touched is whether a turn of the feed wrote, edited or read the file at this path, as the transcript names it.
func (f *Feed) Touched(p string) bool {
	f.ensure()
	for _, t := range f.b.turns {
		for _, file := range t.rec.Files {
			if file.Path == p {
				return true
			}
		}
	}
	return false
}

// Delivered is the sha256 the agent's latest hand-over of the file at this path vouched for (deliver_file's card).
func (f *Feed) Delivered(p string) (string, bool) {
	f.ensure()
	sum, ok := "", false
	for _, t := range f.b.turns {
		for _, o := range t.rec.Outputs {
			if o.Kind == wire.OutputDelivery && o.Ref == p && o.SHA256 != "" {
				sum, ok = o.SHA256, true
			}
		}
	}
	return sum, ok
}

// Tasks is the agent's task list as the entries read so far make it, in the order the tasks were made.
func (f *Feed) Tasks() []wire.Task {
	if f.rel != nil {
		return slices.Clone(f.rel.tasks)
	}
	return f.b.tasks.all()
}

// LastTurn is the newest turn's record, nil before any.
func (f *Feed) LastTurn() *wire.Turn {
	if f.rel != nil {
		if f.rel.lastTurn == nil {
			return nil
		}
		t := *f.rel.lastTurn
		return &t
	}
	if len(f.b.turns) == 0 {
		return nil
	}
	t := f.b.turns[len(f.b.turns)-1].rec
	return &t
}

// Snippet is the first SnippetMax characters of a turn's answer.
func (f *Feed) Snippet(t wire.Turn) string {
	if f.rel != nil && f.rel.lastTurn != nil && f.rel.lastTurn.ID == t.ID {
		return f.rel.snippet
	}
	if t.AnswerEntryID == nil {
		return ""
	}
	e, _ := f.Entry(*t.AnswerEntryID)
	return clip(collapse(e.MD), wire.SnippetMax)
}

func collapse(s string) string {
	out := make([]rune, 0, len(s))
	space := false
	for _, r := range s {
		if r == '\n' || r == '\t' || r == ' ' || r == '\r' {
			if !space && len(out) > 0 {
				out = append(out, ' ')
			}
			space = true
			continue
		}
		space = false
		out = append(out, r)
	}
	return string(slices.Clip(out))
}
