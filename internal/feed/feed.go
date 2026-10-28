// Package feed is a conversation's feed on top of the agent's transcript: entries with ids that come from the
// transcript itself (a re-read and an incremental read agree), cut into turns by the runtime, read in pages by turn,
// and the signals in it that move the agent's state (a question asked, a gate resolved, a turn ended).
//
// The transcript is read by internal/transcript (Claude and Codex); the feed only reads its timeline entries, through
// Source, so a test gives it files from testdata.
package feed

import (
	"math"
	"slices"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/screen"
	"github.com/ly-solutions/harnessly/internal/transcript"
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

// ClaudeSource reads a Claude Code transcript.
type ClaudeSource struct {
	Path    string
	offset  int64
	pending string
	carry   transcript.Carry
}

func (s *ClaudeSource) Tail(max int) transcript.Chunk {
	c := transcript.ReadTail(s.Path, max)
	s.offset, s.pending, s.carry = c.FileSize, "", c.Carry
	return c
}

func (s *ClaudeSource) Before(beforeByte int64, max int) transcript.Chunk {
	return transcript.ReadBefore(s.Path, beforeByte, max)
}

func (s *ClaudeSource) TailWindow(window int64, max int) transcript.Chunk {
	c := transcript.ReadTailBytes(s.Path, window, max)
	s.offset, s.pending, s.carry = c.FileSize, "", c.Carry
	return c
}

func (s *ClaudeSource) BeforeWindow(beforeByte, window int64, max int) transcript.Chunk {
	return transcript.ReadBeforeBytes(s.Path, beforeByte, window, max)
}

func (s *ClaudeSource) Since() transcript.Incremental {
	inc := transcript.ParseIncrementalCarry(s.Path, s.offset, s.pending, s.carry)
	s.offset, s.pending, s.carry = inc.NewOffset, inc.PendingBuffer, inc.Carry
	return inc
}

// CodexSource reads a Codex transcript.
type CodexSource struct {
	Path string
	p    *transcript.CodexParser
}

func (s *CodexSource) parser() *transcript.CodexParser {
	if s.p == nil {
		s.p = transcript.NewCodexParser(s.Path, transcript.CodexOptions{})
	}
	return s.p
}

func (s *CodexSource) Tail(max int) transcript.Chunk { return s.parser().ParseTail(max) }

func (s *CodexSource) Before(beforeByte int64, max int) transcript.Chunk {
	return transcript.ReadCodexBefore(s.Path, beforeByte, max, -1, transcript.CodexOptions{})
}

func (s *CodexSource) Since() transcript.Incremental { return s.parser().ParseIncremental() }

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
	return f.rebuild()
}

// Poll reads what the transcript gained since the last read. The entries' offset is where the read began: their
// lines start there or later.
func (f *Feed) Poll() Update {
	if !f.loaded {
		return f.Load()
	}
	from := f.size
	inc := f.src.Since()
	if inc.NewOffset < from {
		// The file was cut or replaced: the feed starts over on what it holds now.
		return f.Load()
	}
	f.size = inc.NewOffset - int64(len(inc.PendingBuffer))
	var u Update
	for _, o := range inc.NewEntries {
		f.raws = append(f.raws, raw{o, from})
		f.apply(&u, f.b.add(o, from))
	}
	return u
}

// rebuild builds the feed over the raws read, with what the runtime set on it.
func (f *Feed) rebuild() Update {
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

// Size is how far the transcript is read: an entry written after it is newer than anything the feed has.
func (f *Feed) Size() int64 { return f.size }

// Entry is one entry by its id.
func (f *Feed) Entry(id string) (wire.Entry, bool) {
	i, ok := f.b.byID[id]
	if !ok {
		return wire.Entry{}, false
	}
	return f.b.entries[i], true
}

// Messages calls fn with each of the person's messages and the agent's answers (user and say entries), oldest first.
func (f *Feed) Messages(fn func(wire.Entry)) {
	for _, e := range f.b.entries {
		if e.T == wire.EntryUser || e.T == wire.EntrySay {
			fn(e)
		}
	}
}

// SetClientID marks a user entry as the page's message.
func (f *Feed) SetClientID(entryID, clientID string) (wire.Entry, bool) {
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
func (f *Feed) Context() int64 { return f.b.context }

// Model is the model the agent last answered with, "" when its transcript does not say.
func (f *Feed) Model() string { return f.b.model }

// OpenTurn is the turn still running in the transcript, "" none.
func (f *Feed) OpenTurn() string { return f.b.openID() }

// Turn is a turn's record by its id.
func (f *Feed) Turn(id string) (wire.Turn, bool) {
	i, ok := f.b.byTurn[id]
	if !ok {
		return wire.Turn{}, false
	}
	return f.b.turns[i].rec, true
}

// EndTurn ends a turn the transcript left open (a stop hook ahead of its line, a failure, an interrupt): the record
// as it is now. Ending one already ended changes nothing, but a failure the transcript ended as done.
func (f *Feed) EndTurn(id string, st wire.TurnStatus, failure *wire.TurnFailure, at wire.Ms) (wire.Turn, bool) {
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

// After is what follows an entry: the entries after it and the turns they touch. ok is false when the feed does not
// have the entry.
func (f *Feed) After(entryID string) (ts []wire.Turn, es []wire.Entry, ok bool) {
	i, has := f.b.byID[entryID]
	if !has {
		return nil, nil, false
	}
	ts, es = []wire.Turn{}, []wire.Entry{}
	seen := map[string]bool{}
	for _, e := range f.b.entries[i+1:] {
		es = append(es, e)
		if e.TurnID != nil && !seen[*e.TurnID] {
			seen[*e.TurnID] = true
			ts = append(ts, f.b.turns[f.b.byTurn[*e.TurnID]].rec)
		}
	}
	return ts, es, true
}

// Turns is the page of turns before a turn ("" from the end), at most limit, oldest first, with their answer
// entries. Older turns than the feed holds are read from the transcript first.
func (f *Feed) Turns(before string, limit int) (wire.TurnsPage, bool) {
	end := len(f.b.turns)
	if before != "" {
		i, ok := f.b.byTurn[before]
		if !ok {
			return wire.TurnsPage{}, false
		}
		end = i
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
	ts, es, cursor := f.page(end, limit, 0)
	return wire.TurnsPage{Turns: ts, Entries: es, BeforeTurnID: cursor}, true
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
	e, ok := f.Entry(entryID)
	if !ok || e.T != wire.EntryTool {
		return wire.OutputPage{}, false
	}
	return wire.OutputPage{EntryID: entryID, Output: wire.ToolOutput{Text: f.b.outputs[entryID]}}, true
}

// Touched is whether a turn of the feed wrote, edited or read the file at this path, as the transcript names it.
func (f *Feed) Touched(p string) bool {
	for _, t := range f.b.turns {
		for _, file := range t.rec.Files {
			if file.Path == p {
				return true
			}
		}
	}
	return false
}

// Tasks is the agent's task list as the entries read so far make it, in the order the tasks were made.
func (f *Feed) Tasks() []wire.Task { return f.b.tasks.all() }

// LastTurn is the newest turn's record, nil before any.
func (f *Feed) LastTurn() *wire.Turn {
	if len(f.b.turns) == 0 {
		return nil
	}
	t := f.b.turns[len(f.b.turns)-1].rec
	return &t
}

// Snippet is the first SnippetMax characters of a turn's answer.
func (f *Feed) Snippet(t wire.Turn) string {
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
