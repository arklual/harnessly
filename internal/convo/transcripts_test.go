package convo

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/ly-solutions/harnessly/internal/sessionmeta"
	"github.com/ly-solutions/harnessly/internal/shots"
	"github.com/ly-solutions/harnessly/internal/transcriptindex"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// newSharedReadRig is the unload rig with one shared read of the transcript under the conversation's feed and the
// readers beside it.
func newSharedReadRig(t *testing.T) (*unloadRig, *transcriptindex.Index) {
	t.Helper()
	base := newRig(t)
	ix := transcriptindex.New(transcriptindex.Options{Now: base.clock.Now})
	r := &unloadRig{reapRig: &reapRig{rig: base, terms: &fakeTerms{owned: true, tree: idleTree()}, log: &fakeLog{}},
		machine: &forgettingMachine{fakeMachine: base.machine}, pub: &forgettingPub{fakePub: base.pub}, bg: &forgettingBg{},
		meta: &sessionmeta.Reader{Index: ix}, shots: &shots.Reader{Index: ix}}
	r.terms.onStop = func() { base.machine.probes["s1"] = Probe{} }
	r.rt = NewInline(Deps{MachineID: "m1", Publisher: r.pub, Machine: r.machine, Launcher: r.launcher, Clock: r.clock,
		Store: NewStore(filepath.Join(r.dir, "v2")), Limits: wire.DefaultLimits, Log: r.log, Terminals: r.terms,
		Background: r.bg, Meta: r.meta, Shots: r.shots, Transcripts: ix})
	r.open()
	r.settle(wire.StateReady)
	return r, ix
}

// said reports whether the conversation's snapshot has an answer with the text.
func (r *unloadRig) said(text string) bool {
	r.t.Helper()
	return slices.ContainsFunc(r.snap().Entries, func(e wire.Entry) bool { return e.T == wire.EntrySay && e.MD == text })
}

// The feed and the readers beside it take a conversation's lines from the one shared read, and a deleted
// conversation leaves nothing of itself in it.
func TestConversationReadsItsTranscriptThroughTheSharedRead(t *testing.T) {
	r, ix := newSharedReadRig(t)
	r.user("build it")
	r.say("Built.")
	for range 5 {
		r.tick(metaEvery)
	}
	if !r.said("Built.") {
		t.Fatalf("the answer never reached the conversation: %+v", r.snap().Entries)
	}
	c := ix.Counters()
	if c.Files != 1 || c.Subs < 2 {
		t.Fatalf("one transcript with the feed and the readers beside it, got %d files and %d readers", c.Files, c.Subs)
	}
	if c.Opens == 0 || c.Bytes == 0 {
		t.Fatalf("the shared read read nothing: %+v", c)
	}
	if c.Overflows+c.Gaps != 0 {
		t.Fatalf("a reader lost its place: %+v", c)
	}

	// More lines cost one read of what was appended, whoever asks.
	opens := c.Opens
	r.say("And tested.")
	for range 5 {
		r.tick(metaEvery)
	}
	if !r.said("And tested.") {
		t.Fatal("the appended answer never reached the conversation")
	}
	if got := ix.Counters().Opens - opens; got != 1 {
		t.Fatalf("the appended line was read %d times, want once for every reader", got)
	}

	r.turnEnd()
	r.settle(wire.StateReady)
	if res := r.do(wire.ConversationDeleteCmd{OpID: "del-1", ID: r.id}); res.R != wire.ResultAccepted {
		t.Fatalf("delete: %+v", res)
	}
	if c := ix.Counters(); c.Files != 0 || c.Subs != 0 {
		t.Fatalf("a deleted conversation is still followed: %d files, %d readers", c.Files, c.Subs)
	}
}

// A parked conversation is not followed; woken, it is followed again and has what its transcript gained meanwhile.
func TestParkedConversationLeavesTheSharedRead(t *testing.T) {
	r, ix := newSharedReadRig(t)
	r.user("build it")
	r.say("Built.")
	r.turnEnd()
	r.settle(wire.StateReady)
	r.stopAndRest()
	if got := r.rt.Unload(); !slices.Equal(got, []string{r.id}) {
		t.Fatalf("parked %v (kept by: %s)", got, r.why())
	}
	if c := ix.Counters(); c.Files != 0 || c.Subs != 0 {
		t.Fatalf("a parked conversation is still followed: %d files, %d readers", c.Files, c.Subs)
	}
	looks := ix.Counters().Looks
	r.tick(metaRestEvery)
	if got := ix.Counters().Looks - looks; got != 0 {
		t.Fatalf("%d looks at the transcript of a parked conversation", got)
	}

	r.say("Written while parked.")
	if !r.said("Built.") {
		t.Fatal("the woken conversation lost what it had")
	}
	for range 5 {
		r.tick(metaEvery)
	}
	if !r.said("Written while parked.") {
		t.Fatalf("the woken conversation misses what its transcript gained: %+v", r.snap().Entries)
	}
	if c := ix.Counters(); c.Files != 1 || c.Subs == 0 {
		t.Fatalf("the woken conversation is not followed: %d files, %d readers", c.Files, c.Subs)
	}
}
