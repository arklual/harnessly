package transcriptindex_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/background"
	"github.com/ly-solutions/harnessly/internal/feed"
	"github.com/ly-solutions/harnessly/internal/sessionmeta"
	"github.com/ly-solutions/harnessly/internal/shots"
	"github.com/ly-solutions/harnessly/internal/transcriptindex"
	"github.com/ly-solutions/harnessly/internal/transcriptindex/tally"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// syntheticLine is line n of a made-up Claude transcript: a person's message, the request it went out with, the
// agent's words, a tool call and its output, the end of the turn, over and over.
func syntheticLine(n int) string {
	at := time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC).Add(time.Duration(n) * time.Second).Format("2006-01-02T15:04:05.000Z")
	id := func(k int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", k) }
	head := fmt.Sprintf(`{"parentUuid":%q,"isSidechain":false,"uuid":%q,"timestamp":%q,"sessionId":"11111111-2222-4333-8444-555555555555","cwd":"/home/agent/project",`, id(n-1), id(n), at)
	switch n % 6 {
	case 0:
		return head + fmt.Sprintf(`"type":"user","message":{"role":"user","content":"step %d: look at the build and say what failed"}}`, n) + "\n"
	case 1:
		return fmt.Sprintf(`{"type":"api-request","timestamp":%q,"querySource":"repl_main_thread","params":{"model":"claude-sonnet-5-5","output_config":{"effort":"high"}}}`, at) + "\n"
	case 2:
		return head + fmt.Sprintf(`"type":"assistant","message":{"id":"msg_%d","type":"message","role":"assistant","model":"claude-sonnet-5-5","content":[{"type":"text","text":"Looking at the build now. %s"}]}}`, n, strings.Repeat("The log is long and says little. ", 12)) + "\n"
	case 3:
		return head + fmt.Sprintf(`"type":"assistant","message":{"id":"msg_%d","type":"message","role":"assistant","model":"claude-sonnet-5-5","content":[{"type":"tool_use","id":"toolu_%d","name":"Bash","input":{"command":"make build 2>&1 | tail -40","description":"Build the project"}}]}}`, n, n) + "\n"
	case 4:
		return head + fmt.Sprintf(`"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_%d","content":"%s"}]}}`, n-1, strings.Repeat(`cc -O2 -c src/module.c -o build/module.o\n`, 40)) + "\n"
	}
	return head + `"type":"system","subtype":"stop_hook_summary","hookCount":4,"stopReason":"","preventedContinuation":false,"hasOutput":true,"level":"suggestion","hookInfos":[],"hookErrors":[]}` + "\n"
}

// chat is the readers one conversation keeps on its transcript, looked at as the conversation's actor does.
type chat struct {
	file string
	feed *feed.Feed
	bg   *background.Reader
	meta *sessionmeta.Reader
	pics *shots.Reader
}

func newChat(t *testing.T, file string, ix *transcriptindex.Index) *chat {
	t.Helper()
	var b strings.Builder
	for n := 0; n < 5000; n++ {
		b.WriteString(syntheticLine(n))
	}
	if err := os.WriteFile(file, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &chat{file: file, bg: &background.Reader{Index: ix}, meta: &sessionmeta.Reader{Index: ix}, pics: &shots.Reader{Index: ix},
		feed: feed.New("c", wire.ProviderClaude, &feed.ClaudeSource{Path: file, Index: ix})}
	// The first reads, each reader's own, are not what is measured.
	c.feed.Load()
	c.look(true, true)
	c.look(true, true)
	return c
}

// look is one turn of the actor: the feed is read, and when they are due, the background work and, with the pictures, the session's
// own words.
func (c *chat) look(bg, meta bool) (steps int) {
	steps = len(c.feed.Poll().Steps)
	if bg {
		c.bg.ReadAll(c.file)
	}
	if meta {
		c.meta.Read(c.file)
		c.pics.Read(c.file)
	}
	return steps
}

// working is ten seconds of an agent at work that writes 100 lines: the feed is read every second (convo.LookEvery),
// the background work and the session's words every third (bgEvery, metaEvery).
func (c *chat) working(t *testing.T, clock *time.Time) (cost tally.Counts, steps int) {
	t.Helper()
	before := tally.Now()
	f, err := os.OpenFile(c.file, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for second := 0; second < 10; second++ {
		for n := 0; n < 10; n++ {
			if _, err := f.WriteString(syntheticLine(5000 + second*10 + n)); err != nil {
				t.Fatal(err)
			}
		}
		*clock = clock.Add(time.Second)
		steps += c.look(second%3 == 0, second%3 == 0)
	}
	return tally.Now().Sub(before), steps
}

// resting is a minute of an agent at its prompt with nothing written: the feed and the background work are looked
// at every three seconds (3 x LookEvery, bgEvery), the session's words once (metaRestEvery: its file did not change).
func (c *chat) resting(clock *time.Time) tally.Counts {
	before := tally.Now()
	for look := 0; look < 20; look++ {
		*clock = clock.Add(3 * time.Second)
		c.look(true, look == 0)
	}
	return tally.Now().Sub(before)
}

// What following one transcript of 5000 lines costs, each reader with a cursor of its own against one shared read:
// the files opened, the bytes read, the lines decoded. The numbers are logged (go test -v); the test holds the gain.
func TestWhatFollowingATranscriptCosts(t *testing.T) {
	dir := t.TempDir()
	clock := time.Unix(1_767_000_000, 0)
	ix := transcriptindex.New(transcriptindex.Options{Now: func() time.Time { return clock }})
	apart := newChat(t, filepath.Join(dir, "apart.jsonl"), nil)
	shared := newChat(t, filepath.Join(dir, "shared.jsonl"), ix)

	apartWork, apartSteps := apart.working(t, &clock)
	sharedWork, sharedSteps := shared.working(t, &clock)
	if apartSteps == 0 || apartSteps != sharedSteps {
		t.Fatalf("the feeds gained %d and %d steps", apartSteps, sharedSteps)
	}
	apartRest, sharedRest := apart.resting(&clock), shared.resting(&clock)

	t.Logf("agent wrote 100 lines in 10 s  | a cursor each: %+v", apartWork)
	t.Logf("agent wrote 100 lines in 10 s  | one index:     %+v", sharedWork)
	t.Logf("a minute at rest               | a cursor each: %+v", apartRest)
	t.Logf("a minute at rest               | one index:     %+v", sharedRest)

	if sharedRest.Opens != 0 || sharedRest.Bytes != 0 || sharedRest.Decodes != 0 {
		t.Errorf("a transcript at rest was read: %+v", sharedRest)
	}
	if apartRest.Opens < 3 || apartRest.Stats < 2*sharedRest.Stats {
		t.Errorf("at rest the index saves too little: %+v against %+v", sharedRest, apartRest)
	}
	if sharedWork.Opens != 10 || sharedWork.Opens*3 > apartWork.Opens*2 {
		t.Errorf("at work the file is opened %d times against %d: want once a look", sharedWork.Opens, apartWork.Opens)
	}
	if sharedWork.Bytes*2 > apartWork.Bytes || sharedWork.Decodes >= apartWork.Decodes {
		t.Errorf("at work the index reads %d bytes and decodes %d lines against %d and %d", sharedWork.Bytes, sharedWork.Decodes, apartWork.Bytes, apartWork.Decodes)
	}
	if n := ix.Counters(); n.Overflows != 0 || n.Gaps != 0 || n.Resets != 0 || n.Files != 1 || n.Subs != 4 {
		t.Errorf("the index lost a reader on a quiet transcript: %+v", n)
	}
}
