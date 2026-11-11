package transcriptindex

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/transcriptindex/tally"
)

func appendTo(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// collector keeps the lines it was handed, as text with their offsets.
type collector struct {
	got   []string
	types []string
}

func (c *collector) Lines(b *Batch) bool {
	b.Each(func(l *Line) {
		c.got = append(c.got, fmt.Sprintf("%d:%s", l.Offset, l.Text()))
		if f := l.Fields(); f != nil {
			c.types = append(c.types, f.Type)
		}
	})
	return true
}

func line(typ string, n int) string { return fmt.Sprintf(`{"type":%q,"n":%d}`, typ, n) + "\n" }

// Three readers of one transcript cost one open, one read of the new bytes and one decode of each line.
func TestOneReadServesEveryReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendTo(t, path, line("user", 0))
	ix := New(Options{Fresh: -1})
	var a, b, c collector
	subs := []*Sub{ix.Subscribe(path, "a", &a), ix.Subscribe(path, "b", &b), ix.Subscribe(path, "c", &c)}
	for _, s := range subs {
		s.Seat(0, 0)
	}
	appendTo(t, path, line("assistant", 1)+line("user", 2)+`{"type":"assi`)
	before := tally.Now()
	for _, s := range subs {
		if st, partial := s.Sync(false); st != Fresh || !partial {
			t.Fatalf("state %v partial %v", st, partial)
		}
	}
	cost := tally.Now().Sub(before)
	if cost.Opens != 1 || cost.Decodes != 3 || cost.Stats != 3 {
		t.Fatalf("cost %+v", cost)
	}
	second, third := len(line("user", 0)), len(line("user", 0))+len(line("assistant", 1))
	want := []string{`0:{"type":"user","n":0}`, fmt.Sprintf(`%d:{"type":"assistant","n":1}`, second), fmt.Sprintf(`%d:{"type":"user","n":2}`, third)}
	for _, got := range []collector{a, b, c} {
		if !reflect.DeepEqual(got.got, want) || !reflect.DeepEqual(got.types, []string{"user", "assistant", "user"}) {
			t.Fatalf("got %v %v", got.got, got.types)
		}
	}
	// The line being written comes whole, once, when it ends; a look at a file that did not grow opens nothing.
	appendTo(t, path, `stant","n":3}`+"\n")
	before = tally.Now()
	subs[0].Sync(false)
	subs[0].Sync(false)
	subs[1].Sync(false)
	if cost := tally.Now().Sub(before); cost.Opens != 1 || cost.Decodes != 1 {
		t.Fatalf("cost %+v", cost)
	}
	if got := a.got[len(a.got)-1]; !strings.HasSuffix(got, `:{"type":"assistant","n":3}`) || len(a.got) != 4 || len(b.got) != 4 {
		t.Fatalf("got %v / %v", a.got, b.got)
	}
}

// A look shortly after another reader's takes that one's word; a reader's own next look, and a forced one, do not.
func TestALookAnswersTheOtherReadersForAWhile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendTo(t, path, line("user", 0))
	now := time.Unix(1000, 0)
	ix := New(Options{Now: func() time.Time { return now }})
	var a, b collector
	sa, sb := ix.Subscribe(path, "a", &a), ix.Subscribe(path, "b", &b)
	sa.Seat(0, 0)
	sb.Seat(0, 0)
	stats := func(f func()) int64 {
		before := tally.Now()
		f()
		return tally.Now().Sub(before).Stats
	}
	if n := stats(func() { sa.Sync(false); sb.Sync(false) }); n != 1 {
		t.Fatalf("%d stats, want the second look to take the first's word", n)
	}
	if len(b.got) != 1 {
		t.Fatalf("the reader that did not stat got %v", b.got)
	}
	if n := stats(func() { sb.Sync(true); sb.Sync(false) }); n != 2 {
		t.Fatalf("%d stats, want a forced look and a reader's own next look to stat", n)
	}
	now = now.Add(DefaultFresh)
	if n := stats(func() { sa.Sync(false) }); n != 1 {
		t.Fatalf("%d stats, want an old look not to answer", n)
	}
}

// A line longer than a block is read whole, and the blocks before and after it keep their order.
func TestALineLongerThanABlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	long := `{"type":"user","text":"` + strings.Repeat("x", 500) + `"}`
	appendTo(t, path, line("a", 1)+long+"\n"+line("b", 2))
	ix := New(Options{Block: 64})
	var c collector
	s := ix.Subscribe(path, "c", &c)
	s.Seat(0, 0)
	if st, partial := s.Sync(true); st != Fresh || partial {
		t.Fatal(st, partial)
	}
	if !reflect.DeepEqual(c.types, []string{"a", "user", "b"}) || !strings.HasSuffix(c.got[1], long) {
		t.Fatalf("got %v", c.types)
	}
}

// A queue holds a bounded stretch of the transcript: lines that do not fit are not kept, the loss is counted and
// logged, and the reader is served again from where it seats itself.
func TestAFullQueueIsLostNotGrown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendTo(t, path, line("user", 0))
	var warned []string
	ix := New(Options{Fresh: -1, Warn: func(msg string, kv ...any) { warned = append(warned, fmt.Sprint(kv...)) }})
	st0, _ := os.Stat(path)
	q := ix.Queue(path, "slow", 64)
	q.Seat(st0.Size())
	var fast collector
	fs := ix.Subscribe(path, "fast", &fast)
	fs.Seat(st0.Size(), 0)

	appendTo(t, path, line("assistant", 1))
	fs.Sync(false) // another reader's look fills the queue
	data, from, _, st := q.Take()
	if st != Fresh || from != st0.Size() || string(data) != line("assistant", 1) {
		t.Fatalf("take %q from %d: %v", data, from, st)
	}
	for i := 0; i < 10; i++ {
		appendTo(t, path, line("assistant", i))
	}
	fs.Sync(false)
	if len(q.buf) != 0 {
		t.Fatalf("a full queue kept %d bytes", len(q.buf))
	}
	if _, _, _, st := q.Take(); st != Lost {
		t.Fatalf("state %v, want lost", st)
	}
	if n := ix.Counters().Overflows; n != 1 || len(warned) != 1 || !strings.Contains(warned[0], "slow") {
		t.Fatalf("overflows %d, warned %v", n, warned)
	}
	if len(fast.got) != 11 {
		t.Fatalf("the reader beside it got %d lines", len(fast.got))
	}
	// The reader read the file itself up to its end and seats there.
	end, _ := os.Stat(path)
	q.Seat(end.Size())
	appendTo(t, path, line("user", 99))
	if data, from, _, st := q.Take(); st != Fresh || from != end.Size() || string(data) != line("user", 99) {
		t.Fatalf("after seating again: %q from %d: %v", data, from, st)
	}
}

// A file that was cut or replaced is nobody's to go on with: every reader reads its own start again.
func TestACutOrReplacedFileLosesItsReaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendTo(t, path, line("user", 0)+line("user", 1))
	ix := New(Options{Fresh: -1})
	var c collector
	s := ix.Subscribe(path, "c", &c)
	st0, _ := os.Stat(path)
	s.Seat(st0.Size(), 0)
	if st, _ := s.Sync(false); st != Fresh {
		t.Fatal(st)
	}
	if err := os.WriteFile(path, []byte(line("user", 7)), 0o600); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.Sync(false); st != Lost {
		t.Fatalf("a cut file: %v", st)
	}
	s.Seat(0, 0)
	if st, _ := s.Sync(false); st != Fresh || len(c.got) != 1 {
		t.Fatalf("after seating again: %v %v", st, c.got)
	}
	other := path + ".new"
	appendTo(t, other, line("user", 8)+line("user", 9)+line("user", 10))
	if err := os.Rename(other, path); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.Sync(false); st != Lost {
		t.Fatalf("a replaced file: %v", st)
	}
	os.Remove(path)
	s.Seat(0, 0)
	if st, _ := s.Sync(false); st != Gone {
		t.Fatalf("a removed file: %v", st)
	}
	if n := ix.Counters().Resets; n != 3 {
		t.Fatalf("resets %d", n)
	}
}

// A transcript that is not there yet is asked for again and again by its readers: that is not a file that went away,
// so it is not counted. A file that was there and went away is counted once.
func TestAFileThatWasNeverThereIsNotCounted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	warned := 0
	ix := New(Options{Fresh: -1, Warn: func(string, ...any) { warned++ }})
	var c collector
	s := ix.Subscribe(path, "c", &c)
	for i := 0; i < 3; i++ {
		s.Seat(0, 0)
		if st, _ := s.Sync(false); st != Gone {
			t.Fatalf("a file that is not there: %v", st)
		}
	}
	if n := ix.Counters().Resets; n != 0 || warned != 0 {
		t.Fatalf("resets %d, warned %d", n, warned)
	}
	appendTo(t, path, line("user", 0))
	s.Seat(0, 0)
	if st, _ := s.Sync(false); st != Fresh || len(c.got) != 1 {
		t.Fatalf("once it is there: %v %v", st, c.got)
	}
	os.Remove(path)
	for i := 0; i < 3; i++ {
		if st, _ := s.Sync(false); st != Gone && st != Lost {
			t.Fatalf("a removed file: %v", st)
		}
		s.Seat(0, 0)
	}
	if n := ix.Counters().Resets; n != 1 || warned != 1 {
		t.Fatalf("after it went away: resets %d, warned %d", n, warned)
	}
}

// A reader that seats before the index's place is read to from there; the others are not handed a line twice.
func TestAReaderBehindIsReadToFromItsPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendTo(t, path, line("a", 0)+line("b", 1)+line("c", 2))
	ix := New(Options{Fresh: -1})
	var ahead, behind collector
	sa, sb := ix.Subscribe(path, "ahead", &ahead), ix.Subscribe(path, "behind", &behind)
	sa.Seat(int64(len(line("a", 0))), 0)
	sa.Sync(false)
	sb.Seat(0, 0)
	appendTo(t, path, line("d", 3))
	sb.Sync(false)
	if !reflect.DeepEqual(ahead.types, []string{"b", "c", "d"}) || !reflect.DeepEqual(behind.types, []string{"a", "b", "c", "d"}) {
		t.Fatalf("ahead %v behind %v", ahead.types, behind.types)
	}
	// A place inside a line is none.
	var odd collector
	so := ix.Subscribe(path, "odd", &odd)
	end, _ := os.Stat(path)
	so.Seat(end.Size()+3, 0)
	appendTo(t, path, line("e", 4))
	sa.Sync(false)
	if st, _ := so.Sync(false); st != Lost || len(odd.got) != 0 || ix.Counters().Gaps != 1 {
		t.Fatalf("state %v got %v gaps %d", st, odd.got, ix.Counters().Gaps)
	}
}

// A transcript nobody follows is not known to the index and is never opened; a reader not seated costs no look.
func TestNothingIsKeptOfAFileWithoutReaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendTo(t, path, line("user", 0))
	ix := New(Options{})
	var c collector
	s := ix.Subscribe(path, "c", &c)
	q := ix.Queue(path, "q", 0)
	before := tally.Now()
	if st, _ := s.Sync(true); st != Lost {
		t.Fatalf("an unseated reader: %v", st)
	}
	if cost := tally.Now().Sub(before); cost != (tally.Counts{}) {
		t.Fatalf("an unseated reader cost %+v", cost)
	}
	if n := ix.Counters(); n.Files != 1 || n.Subs != 2 {
		t.Fatalf("%+v", n)
	}
	s.Close()
	q.Close()
	q.Close()
	if n := ix.Counters(); n.Files != 0 || n.Subs != 0 {
		t.Fatalf("after close: %+v", n)
	}
	before = tally.Now()
	s.Seat(0, 0)
	if st, _ := s.Sync(true); st != Lost {
		t.Fatalf("a closed reader: %v", st)
	}
	if cost := tally.Now().Sub(before); cost != (tally.Counts{}) || len(c.got) != 0 {
		t.Fatalf("a closed reader cost %+v, got %v", cost, c.got)
	}
}

// A reader that reads when asked rides on the reads made for a live conversation: it gets the lines the file gained
// without opening it, never makes the index follow a file, holds a bounded stretch for a bounded time, and ends with
// the file's last other reader.
func TestAFollowerRidesAlong(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendTo(t, path, line("user", 0))
	now := time.Unix(1000, 0)
	var warned int
	ix := New(Options{Fresh: -1, Now: func() time.Time { return now }, Warn: func(string, ...any) { warned++ }})
	fo := ix.Follower("list", 64)
	size := func() int64 { st, _ := os.Stat(path); return st.Size() }

	fo.Seat(path, size())
	if _, _, ok := fo.Take(path, size()); ok || fo.Places() != 0 || ix.Counters().Files != 0 {
		t.Fatalf("a file nobody follows was followed for the reader: %+v", ix.Counters())
	}
	var live collector
	s := ix.Subscribe(path, "live", &live)
	s.Seat(size(), 0)
	at := size()
	fo.Seat(path, at)
	if data, partial, ok := fo.Take(path, at); !ok || partial || len(data) != 0 {
		t.Fatalf("a file at rest: %q %v %v", data, partial, ok)
	}
	appendTo(t, path, line("assistant", 1)+`{"type":"us`)
	s.Sync(false)
	before := tally.Now()
	data, partial, ok := fo.Take(path, at)
	if cost := tally.Now().Sub(before); !ok || !partial || string(data) != line("assistant", 1) || cost.Opens != 0 {
		t.Fatalf("take %q partial %v ok %v cost %+v", data, partial, ok, cost)
	}
	at += int64(len(data))
	// A place that is not the follower's own is not answered.
	if _, _, ok := fo.Take(path, at-1); ok {
		t.Fatal("a take from another place was answered")
	}
	appendTo(t, path, `er","n":2}`+"\n")
	fo.Seat(path, size())
	at = size()
	// More than it holds: the place is given up quietly, and taken again by a seat.
	for i := 0; i < 5; i++ {
		appendTo(t, path, line("assistant", i))
	}
	s.Sync(false)
	if _, _, ok := fo.Take(path, at); ok || fo.Places() != 0 {
		t.Fatal("lines that did not fit were answered")
	}
	fo.Seat(path, size())
	at = size()
	// A reader that does not come back holds nothing.
	now = now.Add(DefaultFollowIdle + time.Second)
	appendTo(t, path, line("user", 9))
	s.Sync(false)
	fo.mu.Lock()
	held := len(fo.places[path].buf)
	fo.mu.Unlock()
	if _, _, ok := fo.Take(path, at); ok || held != 0 {
		t.Fatalf("an idle reader was kept %d bytes", held)
	}
	if n := ix.Counters(); n.Overflows != 0 || n.Gaps != 0 || warned != 0 {
		t.Fatalf("a follower's ordinary course was counted or logged: %+v, %d warnings", n, warned)
	}
	// The last reader that keeps the file ends the follower's place with it.
	fo.Seat(path, size())
	s.Close()
	if n := ix.Counters(); n.Files != 0 || n.Subs != 0 {
		t.Fatalf("a follower kept the file: %+v", n)
	}
	if _, _, ok := fo.Take(path, size()); ok || fo.Places() != 0 {
		t.Fatal("a place outlived its file")
	}
	var nobody *Follower
	nobody.Seat(path, 0)
	nobody.Forget(path)
	if _, _, ok := nobody.Take(path, 0); ok || (*Index)(nil).Follower("x", 0) != nil {
		t.Fatal("a nil follower answered")
	}
}
