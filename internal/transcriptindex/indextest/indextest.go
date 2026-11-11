// Package indextest helps a reader's tests hold that it says the same of a transcript whether it reads the file
// itself or takes the lines from the shared read: two copies of one made-up transcript grow by the same writes, one
// of them followed by the index as a live conversation's transcript is.
package indextest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/transcriptindex"
	"github.com/ly-solutions/harnessly/internal/transcriptindex/tally"
)

// Line is line n of a made-up Claude transcript: a person's message, the agent's words with their usage, a tool call
// and its output, a title, the end of a turn, over and over.
func Line(n int) string {
	at := time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC).Add(time.Duration(n) * time.Second).Format("2006-01-02T15:04:05.000Z")
	id := func(k int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", k) }
	head := fmt.Sprintf(`{"parentUuid":%q,"isSidechain":false,"uuid":%q,"timestamp":%q,"sessionId":"11111111-2222-4333-8444-555555555555","cwd":"/home/agent/project",`, id(n-1), id(n), at)
	usage := `"usage":{"input_tokens":120,"output_tokens":45,"cache_read_input_tokens":900,"cache_creation_input_tokens":30}`
	switch n % 6 {
	case 0:
		return head + fmt.Sprintf(`"type":"user","message":{"role":"user","content":"step %d: look at the build and say what failed"}}`, n) + "\n"
	case 1:
		return fmt.Sprintf(`{"type":"ai-title","aiTitle":"Build watch %d","sessionId":"11111111-2222-4333-8444-555555555555"}`, n) + "\n"
	case 2:
		return head + fmt.Sprintf(`"type":"assistant","requestId":"req_%d","message":{"id":"msg_%d","type":"message","role":"assistant","model":"claude-sonnet-5-5","content":[{"type":"text","text":"Looking at the build now."}],%s}}`, n, n, usage) + "\n"
	case 3:
		return head + fmt.Sprintf(`"type":"assistant","requestId":"req_%d","message":{"id":"msg_%d","type":"message","role":"assistant","model":"claude-sonnet-5-5","content":[{"type":"tool_use","id":"toolu_%d","name":"Bash","input":{"command":"make build","description":"Build the project"}}],%s}}`, n, n, n, usage) + "\n"
	case 4:
		return head + fmt.Sprintf(`"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_%d","content":"cc -O2 -c src/module.c -o build/module.o"}]}}`, n-1) + "\n"
	}
	return head + `"type":"system","subtype":"turn_duration","durationMs":4200}` + "\n"
}

// Pair is two copies of one transcript: Plain is read by a reader on its own, Followed is followed by the index, as
// the transcript of a live conversation, and read by a reader that takes its lines from there.
type Pair struct {
	Plain, Followed string
	Index           *transcriptindex.Index
	live            *transcriptindex.Sub
}

// New makes the two copies with their first lines written, the index following the second from its end.
func New(t *testing.T, first int) *Pair {
	t.Helper()
	dir := t.TempDir()
	p := &Pair{Plain: filepath.Join(dir, "plain", "s.jsonl"), Followed: filepath.Join(dir, "followed", "s.jsonl"),
		Index: transcriptindex.New(transcriptindex.Options{Fresh: -1})}
	var b strings.Builder
	for n := 0; n < first; n++ {
		b.WriteString(Line(n))
	}
	for _, f := range []string{p.Plain, p.Followed} {
		if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	p.Write(t, b.String(), os.O_APPEND)
	p.live = p.Index.Subscribe(p.Followed, "live", transcriptindex.SinkFunc(func(*transcriptindex.Batch) bool { return true }))
	p.live.Seat(int64(b.Len()), 0)
	return p
}

// Write writes s to both copies, and the live conversation looks at its own.
func (p *Pair) Write(t *testing.T, s string, flag int) {
	t.Helper()
	for _, f := range []string{p.Plain, p.Followed} {
		fh, err := os.OpenFile(f, os.O_CREATE|os.O_WRONLY|flag, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fh.WriteString(s)
		fh.Close()
	}
	if p.live != nil {
		if st, _ := p.live.Sync(true); st != transcriptindex.Fresh {
			st0, err := os.Stat(p.Followed)
			if err == nil {
				p.live.Seat(st0.Size(), 0)
			}
		}
	}
}

// Grow writes lines first..first+count-1 to both copies, every other one in two writes, and calls same after every
// write: there the test holds that its two readers agree.
func (p *Pair) Grow(t *testing.T, first, count int, same func(step string)) {
	t.Helper()
	for n := first; n < first+count; n++ {
		l := Line(n)
		if n%2 == 1 {
			p.Write(t, l[:len(l)/2], os.O_APPEND)
			same(fmt.Sprintf("half of line %d", n))
			p.Write(t, l[len(l)/2:], os.O_APPEND)
		} else {
			p.Write(t, l, os.O_APPEND)
		}
		same(fmt.Sprintf("line %d", n))
	}
}

// Rewrite replaces both copies with the lines 0..count-1.
func (p *Pair) Rewrite(t *testing.T, count int) {
	t.Helper()
	var b strings.Builder
	for n := 0; n < count; n++ {
		b.WriteString(Line(n))
	}
	p.Write(t, b.String(), os.O_TRUNC)
}

// ReaderOpens is how many times the readers opened a file in f: every open but the index's own, which are the live
// conversation's.
func (p *Pair) ReaderOpens(f func()) int64 {
	before, shared := tally.Now(), p.Index.Counters().Opens
	f()
	return tally.Now().Sub(before).Opens - (p.Index.Counters().Opens - shared)
}
