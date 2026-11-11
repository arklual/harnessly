package launch

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/transcriptindex/indextest"
)

func headlineText(h Headline) string {
	title, at := "<nil>", "<nil>"
	if h.Title != nil {
		title = *h.Title
	}
	if h.LastPromptAt != nil {
		at = fmt.Sprint(*h.LastPromptAt)
	}
	return title + " @ " + at
}

// The headline of a transcript a conversation is live on is the same whether the file is read for it or its new
// lines are taken from the shared read, and then the file is not opened.
func TestHeadlinesOffTheSharedReadAreTheSame(t *testing.T) {
	p := indextest.New(t, 12)
	plain, followed := &Headlines{}, &Headlines{Lines: p.Index.Follower("headlines", 0)}
	same := func(step string) {
		t.Helper()
		if a, b := headlineText(plain.Read(p.Plain)), headlineText(followed.Read(p.Followed)); a != b {
			t.Fatalf("%s: read off the file %s, off the shared read %s", step, a, b)
		}
	}
	same("the first read")
	p.Grow(t, 12, 30, same)
	if got := headlineText(followed.Read(p.Followed)); !strings.HasPrefix(got, "Build watch 37 @ 1.7673") {
		t.Fatalf("the headline is %s", got)
	}
	opens := p.ReaderOpens(func() {
		for n := 42; n < 54; n++ {
			p.Write(t, indextest.Line(n), os.O_APPEND)
			if a, b := headlineText(plain.Read(p.Plain)), headlineText(followed.Read(p.Followed)); a != b {
				t.Fatalf("line %d: %s / %s", n, a, b)
			}
		}
	})
	if opens != 12 {
		t.Fatalf("%d opens for 12 new lines: want the 12 of the reader of the file and none off the shared read", opens)
	}
	p.Rewrite(t, 5)
	same("rewritten")
	p.Grow(t, 5, 4, same)
}
