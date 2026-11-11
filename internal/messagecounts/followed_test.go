package messagecounts

import (
	"fmt"
	"os"
	"testing"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/transcriptindex/indextest"
)

// The counts of a transcript a conversation is live on are the same whether the file is read for them or its new
// lines are taken from the shared read, and then the file is not opened.
func TestCountsOffTheSharedReadAreTheSame(t *testing.T) {
	p := indextest.New(t, 12)
	plain, followed := &Route{}, &Route{Lines: p.Index.Follower("counts", 0)}
	said := func(rt *Route, path string) string { return string(jsvalue.Stringify(rt.Count(path).Value(), "")) }
	same := func(step string) {
		t.Helper()
		if a, b := said(plain, p.Plain), said(followed, p.Followed); a != b {
			t.Fatalf("%s: off the file %s, off the shared read %s", step, a, b)
		}
	}
	same("the first read")
	p.Grow(t, 12, 30, same)
	if c := followed.Count(p.Followed); c.User == 0 || c.Assistant == 0 {
		t.Fatalf("the counts are %+v", c)
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
