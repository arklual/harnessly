package convo

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/feed"
	"github.com/ly-solutions/harnessly/internal/sessionmeta"
	"github.com/ly-solutions/harnessly/internal/transcript"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// countedSource is a Claude transcript whose reads of entries are counted (a poll that finds the file as long as it
// was reads nothing and is not counted).
type countedSource struct {
	*feed.ClaudeSource
	reads *atomic.Int64
}

func (s countedSource) Tail(max int) transcript.Chunk {
	s.reads.Add(1)
	return s.ClaudeSource.Tail(max)
}
func (s countedSource) Before(before int64, max int) transcript.Chunk {
	s.reads.Add(1)
	return s.ClaudeSource.Before(before, max)
}
func (s countedSource) TailWindow(window int64, max int) transcript.Chunk {
	s.reads.Add(1)
	return s.ClaudeSource.TailWindow(window, max)
}
func (s countedSource) BeforeWindow(before, window int64, max int) transcript.Chunk {
	s.reads.Add(1)
	return s.ClaudeSource.BeforeWindow(before, window, max)
}

// restRig is a rig whose transcript reads are counted, with one finished turn in the chat.
func restRig(t *testing.T) (*rig, *atomic.Int64) {
	t.Helper()
	reads := &atomic.Int64{}
	r := newRig(t)
	r.deps = func(d *Deps) {
		d.Source = func(c wire.Conversation) feed.Source {
			return countedSource{&feed.ClaudeSource{Path: c.TranscriptPath}, reads}
		}
	}
	r.rt = r.runtime()
	r.open()
	r.tick(time.Second)
	r.user("hello")
	r.say("the answer")
	r.turnEnd()
	r.tick(time.Second)
	return r, reads
}

func (r *rig) stopAgent() {
	r.t.Helper()
	r.machine.probes["s1"] = Probe{Session: true, Shell: true, Resumable: true}
	r.settle(wire.StateStopped)
	r.tick(time.Second)
}

// A stopped chat whose transcript did not change comes back from a restart without its transcript being read: the
// list shows the record it had, and the first page that opens the chat gets the feed it had.
func TestAStoppedChatRestartsWithoutReadingItsTranscript(t *testing.T) {
	r, reads := restRig(t)
	r.stopAgent()
	want, rec := r.snap(), r.rec()
	if len(want.Entries) < 2 || rec.LastTurn == nil || rec.LastTurn.Snippet == "" {
		t.Fatalf("before the restart: %d entries, last turn %+v", len(want.Entries), rec.LastTurn)
	}
	r.rt.Stop()

	reads.Store(0)
	r.restart()
	for range 5 {
		r.tick(10 * time.Second)
	}
	if n := reads.Load(); n != 0 {
		t.Fatalf("the transcript of a stopped chat was read %d times at the start", n)
	}
	got := r.rec()
	if got.State.K != wire.StateStopped || !reflect.DeepEqual(got.LastTurn, rec.LastTurn) || got.LastPromptAt != rec.LastPromptAt ||
		got.LastActivityAt != rec.LastActivityAt || got.Title != rec.Title || !reflect.DeepEqual(got.Model, rec.Model) {
		t.Fatalf("the record after the restart\n got %+v\nwant %+v", got, rec)
	}
	if c, err := r.rt.Context(t.Context(), r.id); err != nil || reads.Load() != 0 {
		t.Fatalf("the context of the chat: %+v, %v, %d reads", c, err, reads.Load())
	}

	snap := r.snap()
	if reads.Load() == 0 {
		t.Fatal("the page opened the chat and nothing was read")
	}
	if !reflect.DeepEqual(snap.Turns, want.Turns) || !reflect.DeepEqual(snap.Entries, want.Entries) {
		t.Fatalf("the feed after the restart\n got %+v\nwant %+v", snap.Entries, want.Entries)
	}
}

// A transcript that changed while the runtime was away is read again at the start.
func TestAChangedTranscriptIsReadAgainAtRestart(t *testing.T) {
	r, reads := restRig(t)
	r.stopAgent()
	before := r.rec()
	r.rt.Stop()

	r.clock.Sleep(time.Minute)
	r.user("one more")
	r.say("the second answer")
	r.turnEnd()

	reads.Store(0)
	r.restart()
	r.tick(10 * time.Second)
	if reads.Load() == 0 {
		t.Fatal("the transcript changed and was not read at the start")
	}
	got := r.rec()
	if got.LastPromptAt <= before.LastPromptAt || got.LastTurn == nil || got.LastTurn.Snippet != "the second answer" {
		t.Fatalf("the record after the restart: prompt at %d (was %d), last turn %+v", got.LastPromptAt, before.LastPromptAt, got.LastTurn)
	}
	if n := len(r.snap().Entries); n < 4 {
		t.Fatalf("the feed holds %d entries, want the second exchange too", n)
	}
}

// A chat whose agent was running when the runtime stopped reads its transcript at the start, as it always did.
func TestARunningChatReadsItsTranscriptAtRestart(t *testing.T) {
	r, reads := restRig(t)
	if k := r.state().K; k == wire.StateStopped || k == wire.StateGone {
		t.Fatalf("the agent is %s", k)
	}
	r.rt.Stop()
	reads.Store(0)
	r.restart()
	r.tick(time.Second)
	if reads.Load() == 0 {
		t.Fatal("a chat with its agent running was not read at the start")
	}
}

// A stopped chat that came back unread leaves its transcript alone on the clock, too: the session's word and the
// pictures are read once a page opens it.
func TestAnUnreadChatIsNotWatchedUntilOpened(t *testing.T) {
	r, _ := restRig(t)
	r.stopAgent()
	r.rt.Stop()
	r.deps = func(d *Deps) { d.Meta = &sessionmeta.Reader{} }
	r.restart()
	for range 20 {
		r.tick(10 * time.Second)
	}
	c := r.rt.actor(r.id).c
	if !c.metaFullAt.IsZero() || !c.unread {
		t.Fatalf("the transcript of an unopened stopped chat was asked for its session (unread %v)", c.unread)
	}
	r.snap()
	for range 3 {
		r.tick(10 * time.Second)
	}
	if c.metaFullAt.IsZero() || c.unread {
		t.Fatalf("the chat was opened and its transcript still not asked (unread %v)", c.unread)
	}
}

// stoppedChats is a store of n stopped chats, each over a transcript of about size bytes, written by a runtime that
// ran them and stopped. It answers the deps a runtime over the same store is made with, and the transcripts.
func stoppedChats(t *testing.T, n, size int) (Deps, []string) {
	t.Helper()
	dir := t.TempDir()
	clock := &fakeClock{now: time.UnixMilli(1_800_000_000_000)}
	m := &fakeMachine{panes: map[string]*fakePane{}, probes: map[string]Probe{}}
	filler := strings.Repeat("lorem ipsum dolor sit amet ", 40)
	var tabs []ImportTab
	var paths []string
	for i := range n {
		id, session := fmt.Sprintf("tab-%03d", i), fmt.Sprintf("s%03d", i)
		path := filepath.Join(dir, id+".jsonl")
		var b strings.Builder
		for k := 0; b.Len() < size; k++ {
			u := fmt.Sprintf("00000000-0000-4000-8000-%06d%06d", i, k)
			ts := clock.Now().Add(time.Duration(k) * time.Second).UTC().Format("2006-01-02T15:04:05.000Z")
			switch k % 3 {
			case 0:
				fmt.Fprintf(&b, `{"uuid":%q,"timestamp":%q,"type":"user","message":{"role":"user","content":%q}}`+"\n", u, ts, "question "+filler)
			case 1:
				fmt.Fprintf(&b, `{"uuid":%q,"timestamp":%q,"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":%q}]}}`+"\n", u, ts, "answer "+filler)
			default:
				fmt.Fprintf(&b, `{"uuid":%q,"timestamp":%q,"type":"system","subtype":"stop_hook_summary"}`+"\n", u, ts)
			}
		}
		if err := writeFile(path, b.String()); err != nil {
			t.Fatal(err)
		}
		m.panes[session] = &fakePane{screen: "$ "}
		m.probes[session] = Probe{Session: true, Shell: true, Resumable: true}
		tabs = append(tabs, ImportTab{ID: id, Session: session, Title: id, Provider: wire.ProviderClaude, Cwd: "/w", TranscriptPath: path})
		paths = append(paths, path)
	}
	clock.Sleep(time.Hour)
	d := Deps{MachineID: "m1", Publisher: &fakePub{t: t, seqs: map[wire.Channel]int64{}}, Machine: m, Launcher: &fakeLauncher{},
		Clock: clock, Store: NewStore(filepath.Join(dir, "v2"))}
	rt := NewInline(d)
	rt.Import(tabs, nil)
	for range 6 {
		clock.Sleep(10 * time.Second)
		rt.Advance()
	}
	for _, a := range rt.all() {
		if k := a.c.rec.State.K; k != wire.StateStopped {
			t.Fatalf("chat %s is %s, not stopped", a.c.rec.ID, k)
		}
	}
	rt.Stop()
	return d, paths
}

// startAllocs is the bytes a start of the runtime over the store allocates, with the first looks of every chat.
func startAllocs(t *testing.T, d Deps) uint64 {
	t.Helper()
	d.Publisher = &fakePub{t: t, seqs: map[wire.Channel]int64{}}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	rt := NewInline(d)
	if err := rt.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	rt.Advance()
	runtime.ReadMemStats(&after)
	for _, a := range rt.all() {
		if k := a.c.rec.State.K; k != wire.StateStopped {
			t.Fatalf("chat %s is %s after the start", a.c.rec.ID, k)
		}
	}
	rt.Stop()
	return after.TotalAlloc - before.TotalAlloc
}

// A start over 150 stopped chats allocates a small part of what reading their transcripts does: the measure of the
// lazy start, with the same store read in full (every transcript touched) beside it.
func TestStartOverStoppedChatsAllocatesLittle(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 150 transcripts")
	}
	const chats, size = 150, 64 << 10
	d, paths := stoppedChats(t, chats, size)
	lazy := startAllocs(t, d)
	touched := time.Now().Add(time.Hour)
	for _, p := range paths {
		if err := os.Chtimes(p, touched, touched); err != nil {
			t.Fatal(err)
		}
	}
	full := startAllocs(t, d)
	t.Logf("start over %d stopped chats of %d KB: %.1f MB allocated at rest, %.1f MB with every transcript read",
		chats, size>>10, float64(lazy)/(1<<20), float64(full)/(1<<20))
	if lazy*5 > full {
		t.Fatalf("the start at rest allocated %d bytes, reading every transcript %d: want under a fifth", lazy, full)
	}
	if perChat := lazy / chats; perChat > 128<<10 {
		t.Fatalf("the start at rest allocated %d bytes a chat", perChat)
	}
}
