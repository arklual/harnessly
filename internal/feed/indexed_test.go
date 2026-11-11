package feed

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/transcriptindex"
	"github.com/ly-solutions/harnessly/internal/transcriptindex/tally"
	"github.com/ly-solutions/harnessly/internal/wire"
)

func appendFile(t *testing.T, file, s string, flag int) {
	t.Helper()
	fh, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|flag, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fh.WriteString(s)
	fh.Close()
}

func said(t *testing.T, u Update) string {
	t.Helper()
	raw, err := json.Marshal(struct {
		Events  []wire.Event
		Signals []Signal
	}{u.Events, u.Signals})
	if err != nil {
		t.Fatal(err)
	}
	// A line without a time of its own is given the time it was read at: the two feeds read a moment apart.
	out := clockTime.ReplaceAllStringFunc(string(raw), func(ms string) string {
		if n, _ := strconv.ParseInt(ms, 10, 64); n >= started {
			return "now"
		}
		return ms
	})
	return clockSpan.ReplaceAllString(out, `"endedAt":now,"durationMs":now`)
}

var (
	clockSpan = regexp.MustCompile(`"endedAt":now,"durationMs":\d+`)
	clockTime = regexp.MustCompile(`\b\d{13}\b`)
	started   = time.Now().UnixMilli()
)

// A feed gives the same events for the same transcript whether its source reads the file itself or takes the lines
// from the index: every recorded transcript, grown line by line after a first read of its start, with a line that
// arrives in two writes, a last line without its newline, and the file cut and written again.
func TestAFeedOverTheIndexGivesTheSameEvents(t *testing.T) {
	recorded, err := filepath.Glob(filepath.Join("..", "transcript", "testdata", "claude", "*.jsonl"))
	if err != nil || len(recorded) < 10 {
		t.Fatal("no recorded transcripts", err)
	}
	for _, name := range recorded {
		t.Run(filepath.Base(name), func(t *testing.T) {
			raw, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.SplitAfter(strings.TrimRight(string(raw), "\n")+"\n", "\n")
			if lines[len(lines)-1] == "" {
				lines = lines[:len(lines)-1]
			}
			dir := t.TempDir()
			plainFile, indexedFile := filepath.Join(dir, "plain.jsonl"), filepath.Join(dir, "indexed.jsonl")
			ix := transcriptindex.New(transcriptindex.Options{Fresh: -1})
			plainSrc, indexedSrc := &ClaudeSource{Path: plainFile}, &ClaudeSource{Path: indexedFile, Index: ix}
			plain, indexed := New("c", wire.ProviderClaude, plainSrc), New("c", wire.ProviderClaude, indexedSrc)
			both := func(s string, flag int) {
				appendFile(t, plainFile, s, flag)
				appendFile(t, indexedFile, s, flag)
			}
			same := func(step string) {
				t.Helper()
				p, i := said(t, plain.Poll()), said(t, indexed.Poll())
				if p != i {
					at := 0
					for at < len(p) && at < len(i) && p[at] == i[at] {
						at++
					}
					from := max(0, at-200)
					t.Fatalf("%s: apart at byte %d:\nthe feed says  …%s\nover the index …%s", step, at, p[from:min(len(p), at+300)], i[from:min(len(i), at+300)])
				}
				if plain.Size() != indexed.Size() || plainSrc.offset != indexedSrc.offset || plainSrc.pending != indexedSrc.pending || plainSrc.carry != indexedSrc.carry {
					t.Fatalf("%s: the sources stand apart: %d/%d, %+v / %+v", step, plain.Size(), indexed.Size(), plainSrc, indexedSrc)
				}
				if n := ix.Counters().Overflows; n != 0 {
					t.Fatalf("%s: the feed's queue overflowed %d times", step, n)
				}
			}
			head := min(2, len(lines)-1)
			both(strings.Join(lines[:head], ""), os.O_APPEND)
			same("the first read")
			for i, l := range lines[head:] {
				switch {
				case i%3 == 1 && len(l) > 10:
					both(l[:len(l)/2], os.O_APPEND)
					same("half of line " + l)
					both(l[len(l)/2:], os.O_APPEND)
				case i%3 == 2:
					// The line is whole, its newline comes later: a read of the file takes it now.
					both(strings.TrimSuffix(l, "\n"), os.O_APPEND)
					same("line without its newline " + l)
					both("\n", os.O_APPEND)
				default:
					both(l, os.O_APPEND)
				}
				same("line " + l)
			}
			same("settled")
			before := tally.Now()
			if u := indexed.Poll(); len(u.Events) != 0 {
				t.Fatalf("a transcript at rest gave %s", said(t, u))
			}
			if cost := tally.Now().Sub(before); cost.Opens != 0 || cost.Bytes != 0 || cost.Decodes != 0 {
				t.Fatalf("a look at a transcript at rest cost %+v", cost)
			}
			same("at rest")
			both(lines[0], os.O_TRUNC)
			same("cut")
			both(strings.Join(lines[1:], ""), os.O_APPEND)
			same("written again")
			if got, want := len(indexed.b.entries), len(plain.b.entries); got != want || got == 0 && len(lines) > 3 {
				t.Fatalf("%d entries over the index, %d without", got, want)
			}
			indexed.Close()
			if n := ix.Counters(); n.Files != 0 || n.Subs != 0 {
				t.Fatalf("a closed feed still follows its transcript: %+v", n)
			}
			both(lines[0], os.O_APPEND)
			same("after the feed let go of the index and took its place again")
		})
	}
}

// The lines a transcript gained are read off the index, not off the file: a feed and the other readers of its
// transcript cost one open between them.
func TestAFeedOverTheIndexDoesNotOpenTheFile(t *testing.T) {
	recorded := []string{filepath.Join("..", "transcript", "testdata", "claude", "recorded-2.1.284-replies-after-idle-prompts.jsonl")}
	raw, err := os.ReadFile(recorded[0])
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(strings.TrimRight(string(raw), "\n")+"\n", "\n")
	file := filepath.Join(t.TempDir(), "s.jsonl")
	appendFile(t, file, lines[0], os.O_APPEND)
	ix := transcriptindex.New(transcriptindex.Options{Fresh: -1})
	f := New("c", wire.ProviderClaude, &ClaudeSource{Path: file, Index: ix})
	f.Poll()
	other := ix.Subscribe(file, "other", transcriptindex.SinkFunc(func(*transcriptindex.Batch) bool { return true }))
	st, _ := os.Stat(file)
	other.Seat(st.Size(), 0)
	appendFile(t, file, strings.Join(lines[1:], ""), os.O_APPEND)
	before := tally.Now()
	other.Sync(false)
	u := f.Poll()
	cost := tally.Now().Sub(before)
	if len(u.Steps) == 0 {
		t.Fatalf("the feed gained nothing of %s: %+v", recorded[0], ix.Counters())
	}
	if cost.Opens != 1 || cost.Bytes != int64(len(raw)-len(lines[0])) {
		t.Fatalf("the feed and a reader beside it cost %+v for %d new bytes", cost, len(raw)-len(lines[0]))
	}
}
