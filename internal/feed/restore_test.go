package feed

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// saved writes a feed down and reads it back, as the conversation's file does.
func saved(t *testing.T, f *Feed) (Saved, bool) {
	t.Helper()
	s, ok := f.Save()
	if !ok {
		return Saved{}, false
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var back Saved
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	return back, true
}

// A feed at rest that was written down comes back over the same transcript without a read — the summary a list asks
// for is answered with the transcript out of reach — and the first read of its entries builds the feed it was, on
// every recorded transcript.
func TestRestoredFeedIsTheSameFeedWithoutARead(t *testing.T) {
	restored := map[wire.Provider]int{}
	for recordedPath, open := range recorded(t) {
		raw, err := os.ReadFile(recordedPath)
		if err != nil || !strings.HasSuffix(string(raw), "\n") {
			continue // a last line still being written: the conversation writes no summary over such a file
		}
		// A copy of its own: the test takes the transcript away for a while, and other tests read the recorded ones.
		path := filepath.Join(t.TempDir(), filepath.Base(recordedPath))
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		p, _ := open()
		source := func() Source {
			if p == wire.ProviderCodex {
				return &CodexSource{Path: path}
			}
			return &ClaudeSource{Path: path}
		}
		f := New("c1", p, source())
		f.Load()
		if f.more {
			f.older()
		}
		endOpen(f)
		s, ok := saved(t, f)
		if !ok {
			continue // a source holding a call still running is not at rest: such a chat reads its transcript at a start
		}
		last, tasks, ctx, model := f.LastTurn(), f.Tasks(), f.Context(), f.Model()
		want := view(f)

		g := New("c1", p, source())
		if !g.Restore(s) {
			t.Fatalf("%s: not restored", path)
		}
		away := path + ".away"
		if err := os.Rename(path, away); err != nil {
			t.Fatal(err)
		}
		if !g.Released() || len(g.raws) != 0 || g.Size() != f.Size() {
			t.Fatalf("%s: a restored feed holds %d raws, size %d (want %d)", path, len(g.raws), g.Size(), f.Size())
		}
		if fmtAny(g.LastTurn()) != fmtAny(last) || fmtAny(g.Tasks()) != fmtAny(tasks) || g.Context() != ctx || g.Model() != model {
			t.Fatalf("%s: the summary of the restored feed differs\n got %s %s %d %q\nwant %s %s %d %q", path,
				fmtAny(g.LastTurn()), fmtAny(g.Tasks()), g.Context(), g.Model(), fmtAny(last), fmtAny(tasks), ctx, model)
		}
		if last != nil && g.Snippet(*last) != f.Snippet(*last) {
			t.Fatalf("%s: snippet differs", path)
		}
		if g.OpenTurn() != "" || g.Uses() != 0 {
			t.Fatalf("%s: open turn %q, uses %d", path, g.OpenTurn(), g.Uses())
		}
		if err := os.Rename(away, path); err != nil {
			t.Fatal(err)
		}
		if u := g.Poll(); len(u.Events) != 0 || !g.Released() {
			t.Fatalf("%s: a poll of an unchanged transcript brought %d events", path, len(u.Events))
		}
		if got := view(g); got != want {
			t.Fatalf("%s: the restored feed differs\n%s", path, firstDiff(want, got))
		}
		restored[p]++
	}
	if restored[wire.ProviderClaude] == 0 || restored[wire.ProviderCodex] == 0 {
		t.Fatalf("restored %v: want transcripts of both providers", restored)
	}
}

// A restored feed whose transcript grew reports what it gained as a feed that never left the memory does, and keeps
// what the runtime had set on it.
func TestRestoredFeedPollsWhatTheTranscriptGained(t *testing.T) {
	lines := strings.SplitAfter(conversation(3), "\n")
	path := write(t, strings.Join(lines[:4], ""))
	kept := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
	kept.Load()
	user := kept.b.entries[0].ID
	if _, ok := kept.SetClientID(user, "client-1"); !ok {
		t.Fatal("no user entry")
	}
	endOpen(kept)
	s, ok := saved(t, kept)
	if !ok {
		t.Fatal("not saved")
	}
	back := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
	if !back.Restore(s) {
		t.Fatal("not restored")
	}
	fh, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if _, err := fh.WriteString(strings.Join(lines[4:], "")); err != nil {
		t.Fatal(err)
	}
	fh.Close()
	want, got := kept.Poll(), back.Poll()
	if len(want.Events) == 0 || fmtAny(got.Events) != fmtAny(want.Events) {
		t.Fatalf("poll after restore:\n got %s\nwant %s", fmtAny(got.Events), fmtAny(want.Events))
	}
	if e, ok := back.Entry(user); !ok || e.ClientID == nil || *e.ClientID != "client-1" {
		t.Fatalf("client id lost: %+v", e)
	}
	if back.Released() || view(back) != view(kept) {
		t.Fatalf("the feed after the poll differs\n%s", diff(view(kept), view(back)))
	}
}

// A feed with a turn open, or one that read nothing, is not written down: such a chat reads its transcript at a start.
func TestAFeedNotAtRestIsNotSaved(t *testing.T) {
	path := write(t, conversation(3))
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
	if _, ok := f.Save(); ok {
		t.Fatal("saved before the first read")
	}
	f.Load()
	if f.OpenTurn() != "" {
		if _, ok := f.Save(); ok {
			t.Fatal("saved with a turn open")
		}
	}
}
