package background

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/transcriptindex"
	"github.com/ly-solutions/harnessly/internal/transcriptindex/tally"
)

// follower is a reader over a transcript of its own, written by the test.
type follower struct {
	r    *Reader
	file string
}

func (f follower) write(t *testing.T, s string, flag int) {
	t.Helper()
	fh, err := os.OpenFile(f.file, os.O_CREATE|os.O_WRONLY|flag, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fh.WriteString(s)
	fh.Close()
}

// The reader says the same of the same transcript whether it reads the file itself or takes its lines from the
// index: line by line, with a line cut in two writes, after a rewrite, and with the file gone and back.
func TestTheIndexedReaderSaysWhatTheReaderSays(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "teammates.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	ts := "2026-01-02T03:04:05Z"
	lines := strings.SplitAfter(strings.TrimRight(string(raw), "\n")+"\n", "\n")
	lines = append(lines,
		line(`{"type":"tool_use","id":"t1","name":"Agent","input":{"description":"Research"}}`, ts)+"\n",
		result("t1", "Async agent launched successfully. agentId: a1aaaaaaa (internal ID)", ts)+"\n",
		line(`{"type":"tool_use","id":"t2","name":"ScheduleWakeup","input":{"reason":"check the build"}}`, ts)+"\n",
		result("t2", "Wakeup scheduled for 03:10 (in 300s)", ts)+"\n",
		`{"type":"user","isSidechain":true,"timestamp":"`+ts+`","message":{"content":"aside"}}`+"\n",
		`not json at all`+"\n",
		`{"type":"assistant","timestamp":12,"aiTitle":7,"message":{"content":[{"type":"tool_use","id":"t3","name":"Agent","input":{"description":"Odd line"}}]}}`+"\n",
		result("t3", "Async agent launched. agentId: a3ccccccc", ts)+"\n",
		`{"type":"attachment","attachment":{"prompt":"<task-notification><task-id>a1aaaaaaa</task-id><status>completed</status></task-notification>"}}`+"\n",
	)
	now := func() float64 { return 1767323100000 } // a minute after ts
	dir := t.TempDir()
	plain := follower{&Reader{Now: now}, filepath.Join(dir, "plain", "s.jsonl")}
	ix := transcriptindex.New(transcriptindex.Options{Fresh: -1})
	indexed := follower{&Reader{Now: now, Index: ix}, filepath.Join(dir, "indexed", "s.jsonl")}
	for _, f := range []follower{plain, indexed} {
		if err := os.MkdirAll(filepath.Dir(f.file), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	same := func(step string) {
		t.Helper()
		pr, pe := plain.r.ReadAll(plain.file)
		ir, ie := indexed.r.ReadAll(indexed.file)
		if !reflect.DeepEqual(pr, ir) || !reflect.DeepEqual(pe, ie) {
			t.Fatalf("%s: running %+v / %+v, ended %+v / %+v", step, pr, ir, pe, ie)
		}
		if p, i := plain.r.Teammates(plain.file), indexed.r.Teammates(indexed.file); !reflect.DeepEqual(p, i) {
			t.Fatalf("%s: teammates %+v / %+v", step, p, i)
		}
	}
	both := func(s string, flag int) {
		plain.write(t, s, flag)
		indexed.write(t, s, flag)
	}
	same("no file yet")
	for i, l := range lines {
		// Every other line arrives in two writes, and is looked at in between.
		if i%2 == 1 && len(l) > 10 {
			both(l[:len(l)/2], os.O_APPEND)
			same("half of line " + l)
			both(l[len(l)/2:], os.O_APPEND)
		} else {
			both(l, os.O_APPEND)
		}
		same("line " + l)
	}
	if running, ended := indexed.r.ReadAll(indexed.file); len(running) < 2 || len(ended) == 0 {
		t.Fatalf("the transcript shows no work: %+v %+v", running, ended)
	}
	before := tally.Now()
	same("at rest")
	if cost := tally.Now().Sub(before); cost.Opens != 0 || cost.Bytes != 0 {
		t.Fatalf("a look at a transcript at rest cost %+v", cost)
	}
	both(lines[len(lines)-4], os.O_TRUNC)
	same("rewritten")
	both(lines[len(lines)-2], os.O_APPEND)
	same("grown after the rewrite")
	os.Remove(plain.file)
	os.Remove(indexed.file)
	same("gone")
	both(strings.Join(lines, ""), os.O_APPEND)
	same("back")
	indexed.r.Forget(indexed.file)
	if n := ix.Counters(); n.Files != 0 || n.Subs != 0 {
		t.Fatalf("a forgotten transcript is still followed: %+v", n)
	}
}

// With the feed's reader beside it, the transcript is opened once for both, and each line is decoded once for the
// readers that take the index's fields.
func TestTheIndexedReaderSharesTheRead(t *testing.T) {
	ts := "2026-01-02T03:04:05Z"
	file := filepath.Join(t.TempDir(), "s.jsonl")
	f := follower{file: file}
	f.write(t, line(`{"type":"text","text":"hello"}`, ts)+"\n", os.O_APPEND)
	ix := transcriptindex.New(transcriptindex.Options{Fresh: -1})
	a, b := &Reader{Index: ix}, &Reader{Index: ix}
	a.Read(file)
	b.Read(file)
	f.write(t, line(`{"type":"tool_use","id":"t1","name":"Agent","input":{"description":"Research"}}`, ts)+"\n"+
		result("t1", "Async agent launched successfully. agentId: a1aaaaaaa (internal ID)", ts)+"\n", os.O_APPEND)
	before := tally.Now()
	got := [][]string{ids(a.Read(file)), ids(b.Read(file))}
	cost := tally.Now().Sub(before)
	if !reflect.DeepEqual(got, [][]string{{"a1aaaaaaa"}, {"a1aaaaaaa"}}) {
		t.Fatal(got)
	}
	if cost.Opens != 1 || cost.Decodes != 2 {
		t.Fatalf("two readers of two new lines cost %+v", cost)
	}
}
