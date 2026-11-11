package economics

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/ly-solutions/harnessly/internal/nodejson"
	"github.com/ly-solutions/harnessly/internal/transcriptindex/indextest"
)

var ownFile = regexp.MustCompile(`"(path|dev|ino)":("[^"]*"|[0-9.e+]+)`)

// The counters of a transcript a conversation is live on are the same whether the file is read for them or its new
// lines are taken from the shared read, and then the file is not opened.
func TestCountersOffTheSharedReadAreTheSame(t *testing.T) {
	p := indextest.New(t, 12)
	plain, followed := &Store{Dir: t.TempDir()}, &Store{Dir: t.TempDir(), Lines: p.Index.Follower("economics", 0)}
	said := func(s *Store, path string) string {
		st, err := s.Refresh(path, "claude")
		if err != nil || st == nil {
			return fmt.Sprint(st, err)
		}
		return ownFile.ReplaceAllString(string(nodejson.Marshal(st)), `"$1":0`)
	}
	var last string
	same := func(step string) {
		t.Helper()
		a := said(plain, p.Plain)
		if last = said(followed, p.Followed); a != last {
			t.Fatalf("%s:\noff the file        %s\noff the shared read %s", step, a, last)
		}
	}
	same("the first read")
	p.Grow(t, 12, 30, same)
	if !regexp.MustCompile(`"offset":[1-9]`).MatchString(last) || !regexp.MustCompile(`claude-sonnet-5-5`).MatchString(last) {
		t.Fatalf("the counters are %s", last)
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
	plain.Flush()
	followed.Flush()
}
