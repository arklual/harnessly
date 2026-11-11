package feed

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/transcriptindex"
	"github.com/ly-solutions/harnessly/internal/transcriptindex/tally"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A Codex feed gives the same events whether its source reads the file itself or takes the lines from the index: every
// transcript of the corpus, grown line by line after a first read of its start, with a line that arrives in two
// writes, a last line without its newline, and the file cut and written again.
func TestACodexFeedOverTheIndexGivesTheSameEvents(t *testing.T) {
	recorded, err := filepath.Glob(filepath.Join("..", "transcript", "testdata", "codex", "corpus", "*.jsonl"))
	if err != nil || len(recorded) < 10 {
		t.Fatal("no Codex transcripts", err)
	}
	recorded = append(recorded, filepath.Join("testdata", "codex_image_gen.jsonl"))
	for _, name := range recorded {
		t.Run(filepath.Base(name), func(t *testing.T) {
			raw, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if len(strings.TrimSpace(string(raw))) == 0 {
				t.Skip("empty")
			}
			lines := strings.SplitAfter(strings.TrimRight(string(raw), "\n")+"\n", "\n")
			if lines[len(lines)-1] == "" {
				lines = lines[:len(lines)-1]
			}
			dir := t.TempDir()
			plainFile, indexedFile := filepath.Join(dir, "plain.jsonl"), filepath.Join(dir, "indexed.jsonl")
			ix := transcriptindex.New(transcriptindex.Options{Fresh: -1})
			plainSrc, indexedSrc := &CodexSource{Path: plainFile}, &CodexSource{Path: indexedFile, Index: ix}
			plain, indexed := New("c", wire.ProviderCodex, plainSrc), New("c", wire.ProviderCodex, indexedSrc)
			both := func(s string, flag int) {
				appendFile(t, plainFile, s, flag)
				appendFile(t, indexedFile, s, flag)
			}
			place := func(s *CodexSource) string {
				if s.p == nil {
					return "-"
				}
				return fmt.Sprint(s.p.Offset(), s.p.Holding(), s.p.AtRest())
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
				if plain.Size() != indexed.Size() || place(plainSrc) != place(indexedSrc) {
					t.Fatalf("%s: the sources stand apart: %d/%d, %s / %s", step, plain.Size(), indexed.Size(), place(plainSrc), place(indexedSrc))
				}
				// The whole file written again at once may be more than the feed's queue holds: the source then reads it
				// itself, which is what an overflow is for.
				if n := ix.Counters().Overflows; n != 0 && step != "written again" {
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
			if cost := tally.Now().Sub(before); cost.Opens != 0 || cost.Bytes != 0 {
				t.Fatalf("a look at a transcript at rest cost %+v", cost)
			}
			both(lines[0], os.O_TRUNC)
			same("cut")
			both(strings.Join(lines[1:], ""), os.O_APPEND)
			same("written again")
			indexed.Close()
			if n := ix.Counters(); n.Files != 0 || n.Subs != 0 {
				t.Fatalf("a closed feed still follows its transcript: %+v", n)
			}
		})
	}
}

// The lines a Codex transcript gained are read off the index: the feed and another reader of the file cost one open.
func TestACodexFeedOverTheIndexDoesNotOpenTheFile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "transcript", "testdata", "codex", "corpus", "basic.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(strings.TrimRight(string(raw), "\n")+"\n", "\n")
	lines = lines[:len(lines)-1]
	if len(lines) < 3 {
		t.Fatalf("basic.jsonl has %d lines", len(lines))
	}
	file := filepath.Join(t.TempDir(), "s.jsonl")
	appendFile(t, file, lines[0], os.O_APPEND)
	ix := transcriptindex.New(transcriptindex.Options{Fresh: -1})
	f := New("c", wire.ProviderCodex, &CodexSource{Path: file, Index: ix})
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
		t.Fatalf("the feed gained nothing: %+v", ix.Counters())
	}
	if cost.Opens != 1 || cost.Bytes != int64(len(raw)-len(lines[0])) {
		t.Fatalf("the feed and a reader beside it cost %+v for %d new bytes", cost, len(raw)-len(lines[0]))
	}
}
