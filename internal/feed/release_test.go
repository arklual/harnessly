package feed

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// recorded is the recorded transcripts with their provider and source.
func recorded(t *testing.T) map[string]func() (wire.Provider, Source) {
	t.Helper()
	out := map[string]func() (wire.Provider, Source){}
	claude, _ := filepath.Glob(filepath.Join("..", "transcript", "testdata", "claude", "*.jsonl"))
	codex, _ := filepath.Glob(filepath.Join("..", "transcript", "testdata", "codex", "corpus", "*.jsonl"))
	for _, p := range claude {
		out[p] = func() (wire.Provider, Source) { return wire.ProviderClaude, &ClaudeSource{Path: p} }
	}
	for _, p := range codex {
		out[p] = func() (wire.Provider, Source) { return wire.ProviderCodex, &CodexSource{Path: p} }
	}
	if len(claude) == 0 || len(codex) == 0 {
		t.Fatal("no recorded transcripts")
	}
	return out
}

// endOpen ends the open turn as the agent's stop does.
func endOpen(f *Feed) {
	if id := f.OpenTurn(); id != "" {
		f.EndTurn(id, wire.TurnDone, nil, 1)
	}
}

// view is what a feed shows: its entries and turns, the first page, the summary a look asks for.
func view(f *Feed) string {
	ts, es, before := f.Snapshot(33)
	var b strings.Builder
	b.WriteString(shape(f))
	for _, x := range []any{ts, es, before, f.LastTurn(), f.Tasks(), f.Context(), f.Model(), f.Records()} {
		b.WriteString("\n")
		b.WriteString(fmtAny(x))
	}
	return b.String()
}

// fmtAny is x as JSON; a time within the hour (an entry the transcript gives no time: the read stamps it) reads NOW.
func fmtAny(x any) string {
	raw, _ := json.Marshal(x)
	now := time.Now().UnixMilli()
	text := msRe.ReplaceAllStringFunc(string(raw), func(m string) string {
		if v, err := strconv.ParseInt(m, 10, 64); err == nil && v > now-3600_000 && v < now+3600_000 {
			return "NOW"
		}
		return m
	})
	// A turn that ends NOW lasts until the moment of the read.
	return durRe.ReplaceAllString(text, `$1SINCE`)
}

var (
	msRe  = regexp.MustCompile(`\b1\d{12}\b`)
	durRe = regexp.MustCompile(`("endedAt":NOW,"durationMs":)\d+`)
)

// a released feed holds nothing read, answers a look's questions without reading, and any other read builds
// it again as it was, on every recorded transcript, read whole and read from a window with a page back.
func TestReleasedFeedIsTheSameFeed(t *testing.T) {
	for path, open := range recorded(t) {
		for _, window := range []int64{TailBytes, 2048} {
			read := func() *Feed {
				p, src := open()
				f := New("c1", p, src)
				f.window = window
				f.Load()
				if f.more {
					f.older()
				}
				return f
			}
			f, twin := read(), read()
			if f.OpenTurn() != "" {
				if f.Release() {
					t.Fatalf("%s: released with a turn open", path)
				}
				endOpen(f)
				endOpen(twin)
			}
			last, tasks, ctx, model := f.LastTurn(), f.Tasks(), f.Context(), f.Model()
			var snippet string
			if last != nil {
				snippet = f.Snippet(*last)
			}
			if !f.Release() || !f.Released() || len(f.raws) != 0 || len(f.b.entries) != 0 {
				t.Fatalf("%s: not released (%d raws, %d entries)", path, len(f.raws), len(f.b.entries))
			}
			uses := f.Uses()
			// What a look asks every few seconds is answered from what the feed kept.
			if !reflect.DeepEqual(f.LastTurn(), last) || !reflect.DeepEqual(f.Tasks(), tasks) || f.Context() != ctx ||
				f.Model() != model || f.OpenTurn() != "" || f.Now("", 0) != nil {
				t.Fatalf("%s: the summary of a released feed differs", path)
			}
			if _, ok := f.PendingTool(); ok {
				t.Fatalf("%s: a pending tool without an open turn", path)
			}
			if last != nil && f.Snippet(*last) != snippet {
				t.Fatalf("%s: snippet %q, want %q", path, f.Snippet(*last), snippet)
			}
			// A poll reports what a feed never released reports; one that gains nothing leaves the feed released.
			wantU, gotU := twin.Poll(), f.Poll()
			if fmtAny(gotU.Events) != fmtAny(wantU.Events) {
				t.Fatalf("%s: poll after release\n%s", path, firstDiff(fmtAny(wantU.Events), fmtAny(gotU.Events)))
			}
			if len(wantU.Events) == 0 && (!f.Released() || f.Uses() != uses) {
				t.Fatalf("%s: a poll that gained nothing read the feed again", path)
			}
			if got, want := view(f), view(twin); got != want {
				t.Fatalf("%s (window %d): the feed read again differs\n%s", path, window, firstDiff(want, got))
			}
			if f.Released() || f.Uses() == uses {
				t.Fatalf("%s: a snapshot did not take the feed back", path)
			}
		}
	}
}

// What the runtime set on the feed (a message's client id, a turn it ended) stays through a release.
func TestReleaseKeepsWhatTheRuntimeSet(t *testing.T) {
	path := write(t, conversation(3))
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
	f.Load()
	user := f.b.entries[0].ID
	if _, ok := f.SetClientID(user, "client-1"); !ok {
		t.Fatal("no user entry")
	}
	endOpen(f)
	want := view(f)
	if !f.Release() {
		t.Fatal("not released")
	}
	if e, ok := f.Entry(user); !ok || e.ClientID == nil || *e.ClientID != "client-1" {
		t.Fatalf("client id lost: %+v", e)
	}
	if got := view(f); got != want {
		t.Fatalf("differs\n%s", diff(want, got))
	}
}

// A released feed whose transcript grows reports what it gained as a feed that was never released does.
func TestReleasedFeedPollsWhatTheTranscriptGained(t *testing.T) {
	lines := strings.SplitAfter(conversation(3), "\n")
	path := write(t, strings.Join(lines[:4], ""))
	kept := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
	kept.Load()
	endOpen(kept)
	rel := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
	rel.Load()
	endOpen(rel)
	if !rel.Release() {
		t.Fatal("not released")
	}
	fh, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if _, err := fh.WriteString(strings.Join(lines[4:], "")); err != nil {
		t.Fatal(err)
	}
	fh.Close()
	want, got := kept.Poll(), rel.Poll()
	if len(want.Events) == 0 || fmtAny(got.Events) != fmtAny(want.Events) {
		t.Fatalf("poll after release:\n got %s\nwant %s", fmtAny(got.Events), fmtAny(want.Events))
	}
	if rel.Released() || view(rel) != view(kept) {
		t.Fatalf("the feed after the poll differs\n%s", diff(view(kept), view(rel)))
	}
}

// firstDiff is the first place two views differ, with a little around it.
func firstDiff(want, got string) string {
	i := 0
	for i < len(want) && i < len(got) && want[i] == got[i] {
		i++
	}
	lo := max(0, i-150)
	return fmt.Sprintf("at %d:\n want …%s\n got  …%s", i, want[lo:min(len(want), i+150)], got[lo:min(len(got), i+150)])
}

// The actor's own looks on the clock (the pictures a chat names) must not keep an idle chat's transcript in memory:
// Held neither reads a released feed again nor counts a use.
func TestHeldLeavesAReleasedFeedAlone(t *testing.T) {
	path := write(t, conversation(3))
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
	f.Load()
	endOpen(f)
	uses := f.Uses()
	n := 0
	if !f.Held(func(wire.Entry) { n++ }) || n == 0 {
		t.Fatalf("held feed gave %d messages", n)
	}
	if f.Uses() != uses {
		t.Fatalf("Held counted a use: %d → %d", uses, f.Uses())
	}
	if !f.Release() {
		t.Fatal("not released")
	}
	if f.Held(func(wire.Entry) { t.Fatal("a released feed gave a message") }) {
		t.Fatal("Held said a released feed holds its messages")
	}
	if !f.Released() || f.Uses() != uses {
		t.Fatalf("Held read the feed again: released %v, uses %d → %d", f.Released(), uses, f.Uses())
	}
}
