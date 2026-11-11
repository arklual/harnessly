package sessions

import (
	"fmt"
	"os"
	"testing"

	"github.com/ly-solutions/harnessly/internal/transcriptindex/indextest"
)

// What the list says of a transcript is the same whether the file is opened at every list or its new lines are
// taken from the shared read; a transcript that did not grow is not opened at all.
func TestSessionsOffTheSharedReadAreTheSame(t *testing.T) {
	p := indextest.New(t, 12)
	plain, followed := NewClaudeLister(t.TempDir()), NewClaudeLister(t.TempDir())
	followed.Lines = p.Index.Follower("sessions", 0)
	said := func(l *ClaudeLister, path string) string {
		m := l.meta(path)
		if m == nil {
			return "<nil>"
		}
		return fmt.Sprintf("%s|%s|%d|%s|%s", m.StartedAt, m.FirstMessage, m.TurnCount, m.cwd, m.title)
	}
	same := func(step string) {
		t.Helper()
		if a, b := said(plain, p.Plain), said(followed, p.Followed); a != b {
			t.Fatalf("%s: off the file %s, off the shared read %s", step, a, b)
		}
	}
	same("the first read")
	p.Grow(t, 12, 30, same)
	if m := followed.meta(p.Followed); m == nil || m.TurnCount == 0 || m.title != "Build watch 37" {
		t.Fatalf("the list says %+v", m)
	}
	opens := p.ReaderOpens(func() {
		for n := 42; n < 54; n++ {
			p.Write(t, indextest.Line(n), os.O_APPEND)
			same(fmt.Sprintf("line %d", n))
			same("listed again") // nothing new
		}
	})
	if opens != 24 {
		t.Fatalf("%d opens for 24 lists: want the 24 of the reader of the file and none off the shared read", opens)
	}
	p.Rewrite(t, 5)
	same("rewritten")
	p.Grow(t, 5, 4, same)
}
