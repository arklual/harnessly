package machines

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/ly-solutions/harnessly/internal/transcriptindex/indextest"
)

// The rows of a transcript a conversation is live on are the same whether the file is read for them or its new
// lines are taken from the shared read, and then the file is not opened.
func TestUsageRowsOffTheSharedReadAreTheSame(t *testing.T) {
	p := indextest.New(t, 12)
	lines := p.Index.Follower("usage", 0)
	plain, followed := &transcript{provider: "claude"}, &transcript{provider: "claude"}
	// bring is what Build does for one transcript.
	bring := func(tr *transcript, path string, withLines bool) []msgRow {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Size() != tr.size || !st.ModTime().Equal(tr.mtime) {
			if !withLines || !tr.follow(lines, path, st, 0) {
				if !tr.update(path, st, 0) {
					t.Fatal("the transcript was not read")
				}
				if withLines && tr.size == tr.offset {
					lines.Seat(path, tr.offset)
				}
			}
		}
		return tr.rowsNow(path, 0)
	}
	same := func(step string) {
		t.Helper()
		a, b := bring(plain, p.Plain, false), bring(followed, p.Followed, true)
		if !reflect.DeepEqual(a, b) || plain.offset != followed.offset {
			t.Fatalf("%s: off the file %d rows to %d, off the shared read %d rows to %d", step, len(a), plain.offset, len(b), followed.offset)
		}
	}
	same("the first read")
	p.Grow(t, 12, 30, same)
	if len(followed.rows) < 10 {
		t.Fatalf("%d rows", len(followed.rows))
	}
	opens := p.ReaderOpens(func() {
		for n := 42; n < 54; n++ {
			p.Write(t, indextest.Line(n), os.O_APPEND)
			same(fmt.Sprintf("line %d", n))
		}
	})
	if opens != 12 {
		t.Fatalf("%d opens for 12 new lines: want the 12 of the reader of the file and none off the shared read", opens)
	}
	p.Rewrite(t, 5)
	same("rewritten")
	p.Grow(t, 5, 4, same)
}
