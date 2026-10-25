package feed

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// turnShape is one turn as a whole read makes it: its record's counters and status, and its entries with their revs.
func turnShape(f *Feed, id string) string {
	i, ok := f.b.byTurn[id]
	if !ok {
		return "missing"
	}
	t := f.b.turns[i]
	var b strings.Builder
	fmt.Fprintf(&b, "turn %s steps=%d tools=%d status=%s\n", t.rec.ID, t.rec.StepCount, t.rec.ToolCount, t.rec.Status)
	for _, eid := range t.entries {
		e := f.b.entries[f.b.byID[eid]]
		fmt.Fprintf(&b, "  %s %s rev=%d ok=%v\n", e.ID, e.T, e.Rev, e.OK != nil && *e.OK)
	}
	return b.String()
}

// The first read takes the transcript from its end by bytes, wherever the window falls: inside a line, between a
// tool's call and its result, in the middle of a turn. The turns it holds are whole and the same as a whole read's,
// the newest is in the snapshot with all its steps, the cut turn's tail is not shown as lines outside any turn, and
// paging back by the cursor gives the feed of a whole read.
func TestTailWindowCutAnywhereKeepsTheNewestTurnsWhole(t *testing.T) {
	text := conversation(24)
	path := write(t, text)
	whole := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
	whole.window = 1 << 30
	whole.Load()
	if len(whole.b.turns) != 24 {
		t.Fatalf("whole read: %d turns", len(whole.b.turns))
	}
	newest := whole.b.turns[len(whole.b.turns)-1].rec.ID
	// Every cut from inside the last line back to the middle of the file: each line's start, its middle, its end.
	for cut := int64(1); cut < int64(len(text))/2; cut += 89 {
		f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
		f.window = cut
		f.Load()
		if len(f.b.turns) < TailTurns {
			t.Fatalf("cut %d: %d turns, want at least %d", cut, len(f.b.turns), TailTurns)
		}
		if !f.more {
			t.Fatalf("cut %d: the tail read the whole file", cut)
		}
		for _, tr := range f.b.turns {
			if got, want := turnShape(f, tr.rec.ID), turnShape(whole, tr.rec.ID); got != want {
				t.Fatalf("cut %d: turn differs from a whole read\n%s", cut, diff(want, got))
			}
		}
		ts, es, before := f.Snapshot(33)
		if before == nil || *before != f.b.turns[0].rec.ID {
			t.Fatalf("cut %d: cursor %v, want the first turn read", cut, before)
		}
		if ts[len(ts)-1].ID != newest {
			t.Fatalf("cut %d: the snapshot's last turn is %s, want the newest %s", cut, ts[len(ts)-1].ID, newest)
		}
		inNewest := 0
		for _, e := range es {
			if e.TurnID == nil {
				t.Fatalf("cut %d: the snapshot shows %s %s outside any turn: the tail of a cut turn", cut, e.ID, e.T)
			}
			if *e.TurnID == newest {
				inNewest++
			}
		}
		if n := len(whole.b.turns[len(whole.b.turns)-1].entries); inNewest != n {
			t.Fatalf("cut %d: the snapshot has %d entries of the newest turn, want %d", cut, inNewest, n)
		}
		cursor := *before
		for range 30 {
			page, ok := f.Turns(cursor, 5)
			if !ok {
				t.Fatalf("cut %d: cursor %s unknown", cut, cursor)
			}
			if page.BeforeTurnID == nil {
				break
			}
			cursor = *page.BeforeTurnID
		}
		if shape(f) != shape(whole) {
			t.Fatalf("cut %d: after paging back the feed differs from a whole read\n%s", cut, diff(shape(whole), shape(f)))
		}
	}
}

// A transcript of large lines is read only as far back as its newest turns need, not until it gives TailEntries
// entries: a tool output of megabytes does not make the first read parse the whole file.
func TestTailWindowStopsAtTheTurnsItNeeds(t *testing.T) {
	var b strings.Builder
	big := strings.Repeat("x", 200_000)
	for i := range 40 {
		ts := fmt.Sprintf("2026-10-12T10:%02d:00.000Z", i)
		fmt.Fprintf(&b, `{"type":"user","timestamp":%q,"message":{"role":"user","content":"question %d"}}`+"\n", ts, i)
		fmt.Fprintf(&b, `{"type":"assistant","timestamp":%q,"message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_%04d","name":"Bash","input":{"command":"cat %d"}}]}}`+"\n", ts, i, i)
		fmt.Fprintf(&b, `{"type":"user","timestamp":%q,"message":{"role":"user","content":[{"tool_use_id":"toolu_%04d","type":"tool_result","content":%q,"is_error":false}]}}`+"\n", ts, i, big)
		fmt.Fprintf(&b, `{"type":"assistant","timestamp":%q,"message":{"role":"assistant","content":[{"type":"text","text":"answer %d"}]}}`+"\n", ts, i)
	}
	path := write(t, b.String())
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
	f.window = 300_000 // less than two turns: doubled until it holds TailTurns
	f.Load()
	if n := len(f.b.turns); n < TailTurns || n > 2*TailTurns {
		t.Fatalf("%d turns read, want about %d", n, TailTurns)
	}
	whole := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
	whole.window = 1 << 30
	whole.Load()
	if got, want := f.LastTurn(), whole.LastTurn(); got == nil || got.ID != want.ID || got.StepCount != want.StepCount {
		t.Fatalf("last turn %+v, want %+v", got, want)
	}
	if !f.more || f.start == 0 {
		t.Fatalf("read from %d, more %v: the whole file was read", f.start, f.more)
	}
}

// A line longer than the window (a tool output larger than it) is not lost: the window grows until the line fits.
func TestTailWindowGrowsOverALineLongerThanIt(t *testing.T) {
	text := conversation(6) + `{"type":"user","timestamp":"2026-10-12T11:00:00.000Z","message":{"role":"user","content":"` +
		strings.Repeat("y", 50_000) + `"}}` + "\n"
	path := write(t, text)
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
	f.window = 1000
	f.Load()
	last := f.LastTurn()
	if last == nil || last.Status != wire.TurnLive {
		t.Fatalf("the newest turn, opened by the long message, is not read: %+v", last)
	}
	e, ok := f.Entry(last.ID)
	if !ok || len(e.Text) != 50_000 {
		t.Fatalf("the long message: %d characters", len(e.Text))
	}
	if f.Size() != int64(len(text)) {
		t.Fatalf("read to %d, the file has %d", f.Size(), len(text))
	}
}

// The recorded transcripts (subagents, teammates, queued and pasted messages, commands) cut by the first read at
// every few bytes: the turns it holds are the whole read's, and paging back gives the whole read's feed.
func TestTailWindowCutsRecordedTranscripts(t *testing.T) {
	paths, _ := filepath.Glob(filepath.Join("..", "transcript", "testdata", "claude", "*.jsonl"))
	if len(paths) == 0 {
		t.Fatal("no recorded transcripts")
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		whole := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
		whole.window = 1 << 30
		whole.Load()
		if len(whole.b.turns) < 2 {
			continue
		}
		step := max(int64(len(raw))/20, 1)
		for cut := int64(1); cut < int64(len(raw)); cut += step {
			f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
			f.window = cut
			f.Load()
			name := fmt.Sprintf("%s cut %d", filepath.Base(path), cut)
			if got, want := f.LastTurn(), whole.LastTurn(); got == nil || got.ID != want.ID {
				t.Fatalf("%s: last turn %v, want %s", name, got, want.ID)
			}
			ts, es, before := f.Snapshot(33)
			for _, tr := range ts {
				if got, want := turnShape(f, tr.ID), turnShape(whole, tr.ID); got != want {
					t.Fatalf("%s: a turn of the snapshot differs from a whole read\n%s", name, diff(want, got))
				}
			}
			for _, e := range es {
				if w, ok := whole.Entry(e.ID); e.TurnID == nil && (!ok || w.TurnID != nil) {
					t.Fatalf("%s: %s %s shown outside any turn, a whole read has it in one", name, e.ID, e.T)
				}
			}
			for cursor := before; cursor != nil; {
				page, ok := f.Turns(*cursor, 5)
				if !ok {
					t.Fatalf("%s: cursor %s unknown", name, *cursor)
				}
				cursor = page.BeforeTurnID
			}
			if shape(f) != shape(whole) {
				t.Fatalf("%s: after paging back the feed differs from a whole read\n%s", name, diff(shape(whole), shape(f)))
			}
		}
	}
}
