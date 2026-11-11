package claude

import (
	"fmt"
	"os"
	"testing"

	"github.com/ly-solutions/harnessly/internal/transcriptindex/indextest"
)

// The last conversation time of a transcript a conversation is live on is the same whether its tail is read again
// at every change or the answer moves on by the lines the shared read holds, and then the file is not opened.
func TestActivityOffTheSharedReadIsTheSame(t *testing.T) {
	p := indextest.New(t, 12)
	plain, followed := &ActivityReader{}, &ActivityReader{Lines: p.Index.Follower("activity", 0)}
	at := func(r *ActivityReader, path string) string {
		if v := r.At(path); v != nil {
			return fmt.Sprint(*v)
		}
		return "<nil>"
	}
	same := func(step string) {
		t.Helper()
		if a, b := at(plain, p.Plain), at(followed, p.Followed); a != b {
			t.Fatalf("%s: off the tail %s, off the shared read %s", step, a, b)
		}
	}
	same("the first read")
	p.Grow(t, 12, 30, same)
	if got := at(followed, p.Followed); got == "<nil>" {
		t.Fatalf("the last conversation time is %s", got)
	}
	opens := p.ReaderOpens(func() {
		for n := 42; n < 54; n++ {
			p.Write(t, indextest.Line(n), os.O_APPEND)
			same(fmt.Sprintf("line %d", n))
		}
	})
	if opens != 12 {
		t.Fatalf("%d opens for 12 new lines: want the 12 of the reader of the tail and none off the shared read", opens)
	}
	p.Rewrite(t, 5)
	same("rewritten")
	p.Grow(t, 5, 4, same)
	followed.Forget(p.Followed)
	if n := p.Index.Counters().Subs; n != 1 {
		t.Fatalf("a forgotten transcript keeps its place: %d subscriptions", n)
	}
}
