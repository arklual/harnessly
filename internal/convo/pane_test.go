package convo

import (
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/feed"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/sources"
	"github.com/ly-solutions/harnessly/internal/transcript"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// everyRead counts every read of the transcript a conversation's source makes, the polls that find nothing included.
type everyRead struct {
	*feed.ClaudeSource
	reads *int
}

func (s everyRead) Tail(max int) transcript.Chunk { *s.reads++; return s.ClaudeSource.Tail(max) }
func (s everyRead) Before(before int64, max int) transcript.Chunk {
	*s.reads++
	return s.ClaudeSource.Before(before, max)
}
func (s everyRead) TailWindow(window int64, max int) transcript.Chunk {
	*s.reads++
	return s.ClaudeSource.TailWindow(window, max)
}
func (s everyRead) BeforeWindow(before, window int64, max int) transcript.Chunk {
	*s.reads++
	return s.ClaudeSource.BeforeWindow(before, window, max)
}
func (s everyRead) Since() transcript.Incremental { *s.reads++; return s.ClaudeSource.Since() }

// paneRig is a rig whose runtime gathers the pane's data (on) or not, with the reads of its transcript counted.
func paneRig(t *testing.T, on bool) (*rig, *int) {
	t.Helper()
	r := newRig(t)
	reads := new(int)
	r.deps = func(d *Deps) {
		if on {
			d.PaneLinks = &sources.Links{}
		}
		d.Source = func(c wire.Conversation) feed.Source {
			return everyRead{&feed.ClaudeSource{Path: c.TranscriptPath}, reads}
		}
	}
	r.rt = r.runtime()
	return r, reads
}

// panes are the pane parts the conversation's channel carried, in order.
func (r *rig) panes() []wire.Pane {
	var out []wire.Pane
	for _, p := range r.events(wire.EvQueueChanged) {
		if e, ok := p.e.(wire.QueueChangedEv); ok && e.Pane != nil {
			out = append(out, *e.Pane)
		}
	}
	return out
}

// paneScript plays a short conversation: an answer with a link before any page looks, a snapshot, then a command and a
// second link while the page is there. It answers the snapshot.
func paneScript(r *rig) wire.ConvSnapshot {
	r.open()
	r.settle(wire.StateReady)
	r.user("write the report")
	r.say("Done: [the report](https://docs.example.com/a/report-x1).")
	r.tick(5 * time.Second)
	snap := r.snap()
	r.tool("toolu_1", "cat /w/notes.md")
	r.tick(5 * time.Second)
	r.say("And the changelog: https://example.com/changelog")
	r.tick(5 * time.Second)
	return snap
}

// What the right pane reads of a chat comes with the chat's snapshot and then with every read of its transcript, in the
// chat's own channel; nothing of it goes out before a page asked for the chat.
func TestPaneComesWithTheConversation(t *testing.T) {
	r, _ := paneRig(t, true)
	r.open()
	r.settle(wire.StateReady)
	r.user("write the report")
	r.say("Done: [the report](https://docs.example.com/a/report-x1).")
	r.tick(5 * time.Second)
	if got := r.panes(); len(got) != 0 {
		t.Fatalf("pane events before a page asked: %+v", got)
	}
	snap := r.snap()
	if snap.Pane == nil || len(snap.Pane.Links) != 1 || snap.Pane.Links[0].URL != "https://docs.example.com/a/report-x1" ||
		snap.Pane.Links[0].Label == nil || *snap.Pane.Links[0].Label != "the report" || snap.Pane.Count == 0 {
		t.Fatalf("snapshot pane: %+v", snap.Pane)
	}
	if err := snap.Validate(); err != nil {
		t.Fatal(err)
	}

	r.tool("toolu_1", "cat /w/notes.md")
	r.tick(5 * time.Second)
	got := r.panes()
	if len(got) != 1 || got[0].Reset || len(got[0].Links) != 0 || len(got[0].Items) != 1 || got[0].Count <= snap.Pane.Count {
		t.Fatalf("after a command: %+v", got)
	}
	if it := got[0].Items[0]; it.K != "tool" || it.Tool != "Bash" || it.Use != "toolu_1" || !strings.Contains(it.Text, "/w/notes.md") {
		t.Fatalf("item: %+v", it)
	}

	r.say("And the changelog: https://example.com/changelog")
	r.tick(5 * time.Second)
	got = r.panes()
	if len(got) != 2 || len(got[1].Items) != 0 || len(got[1].Links) != 2 || got[1].Links[1].URL != "https://docs.example.com/a/report-x1" {
		t.Fatalf("after a second link: %+v", got)
	}
	// A page that comes now is given the same: both links and the command.
	again := r.snap().Pane
	if again == nil || len(again.Links) != 2 || len(again.Items) != 1 || again.Count != got[1].Count {
		t.Fatalf("second snapshot: %+v", again)
	}
}

// The pane's data is taken from the reads the feed makes anyway: a runtime that gathers it reads the transcript
// exactly as many times as one that does not, and asking for the snapshot reads nothing.
func TestPaneReadsTheTranscriptNoSecondTime(t *testing.T) {
	with, withReads := paneRig(t, true)
	without, withoutReads := paneRig(t, false)
	if snap := paneScript(with); snap.Pane == nil {
		t.Fatal("no pane in the snapshot")
	}
	if snap := paneScript(without); snap.Pane != nil {
		t.Fatalf("pane in the snapshot of a runtime that gathers none: %+v", snap.Pane)
	}
	if len(with.panes()) == 0 || len(without.panes()) != 0 {
		t.Fatalf("pane events: %d with, %d without", len(with.panes()), len(without.panes()))
	}
	if *withReads == 0 || *withReads != *withoutReads {
		t.Fatalf("reads of the transcript: %d with the pane, %d without", *withReads, *withoutReads)
	}
	before := *withReads
	with.snap()
	if *withReads != before {
		t.Fatalf("a snapshot read the transcript: %d → %d", before, *withReads)
	}
}

// An item carries only what the pane's tools read, and only the lines they read.
func TestPaneKeepsWhatItsToolsRead(t *testing.T) {
	p := newPaneFeed(&sources.Links{})
	obj := jsvalue.NewObject
	items, links := p.add([]*jsvalue.Object{
		obj("id", "a1", "type", "assistant-message", "timestamp", 10.0, "markdown", "plain words, https://example.com/x"),
		obj("id", "a2", "type", "assistant-message", "timestamp", 11.0, "markdown", "![shot](https://example.com/shot.png)"),
		obj("id", "u1", "type", "user-message", "timestamp", 12.0, "text", "just words"),
		obj("id", "u2", "type", "user-message", "timestamp", 13.0, "text", "see", "images", []any{"data:image/png;base64,AAAA", "/home/u/.harnessly/uploads/1-a.png"}),
		obj("id", "u3", "type", "user-message", "timestamp", 14.0, "text", `<teammate-message teammate_id="scout">done</teammate-message>`),
		obj("id", "t1", "type", "tool-call", "timestamp", 15.0, "toolName", "Write", "toolUseId", "w1", "filePath", "/w/a.md", "summary", "Create /w/a.md", "input", obj("content", "long")),
		obj("id", "t2", "type", "tool-call", "timestamp", 16.0, "toolName", "Grep", "toolUseId", "g1", "summary", "Grep foo"),
		obj("id", "t3", "type", "tool-call", "timestamp", 17.0, "toolName", "Agent", "toolUseId", "ag1", "summary", "Agent look"),
		obj("id", "r1", "type", "tool-result", "timestamp", 18.0, "toolUseId", "g1", "summary", "3 lines"),
		obj("id", "r2", "type", "tool-result", "timestamp", 19.0, "toolUseId", "ag1", "summary", "found"),
		obj("id", "t4", "type", "tool-call", "timestamp", 20.0, "toolName", "SendMessage", "toolUseId", "s1", "summary", "SendMessage scout",
			"peerMessage", obj("to", "scout", "text", "go on")),
		obj("id", "th", "type", "thinking", "timestamp", 21.0, "text", "hm"),
	})
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID+":"+it.K)
	}
	if got, want := strings.Join(ids, " "), "a2:answer u2:user u3:user t1:tool t3:tool r2:result t4:tool"; got != want {
		t.Fatalf("items:\n got %s\nwant %s", got, want)
	}
	if !links || len(p.sent) == 0 || p.count != 12 {
		t.Fatalf("links %v %+v, count %d", links, p.sent, p.count)
	}
	if u := items[1]; len(u.Images) != 1 || u.Images[0] != "/home/u/.harnessly/uploads/1-a.png" {
		t.Fatalf("pictures of a message: %+v", u)
	}
	if w := items[3]; w.Path != "/w/a.md" || w.Text != "Create /w/a.md" || w.At != 15 {
		t.Fatalf("a written file: %+v", w)
	}
	if s := items[6]; s.Peer == nil || s.Peer.To != "scout" || s.Peer.Text != "go on" || s.Text != "" {
		t.Fatalf("a message to another agent: %+v", s)
	}
	for _, it := range items {
		if err := it.Validate(); err != nil {
			t.Fatalf("%s: %v", it.ID, err)
		}
	}
}

// A snapshot's pane holds the newest items only, within its count and its weight.
func TestPaneSnapshotIsBounded(t *testing.T) {
	p := newPaneFeed(&sources.Links{})
	var entries []*jsvalue.Object
	for i := range 3 * wire.PaneItemsMax {
		entries = append(entries, jsvalue.NewObject("id", "n"+itoa(int64(i)), "type", "task-notification", "timestamp", float64(i+1)))
	}
	p.add(entries)
	w := p.whole()
	if len(w.Items) != wire.PaneItemsMax || w.Items[len(w.Items)-1].ID != "n"+itoa(int64(3*wire.PaneItemsMax-1)) || w.Count != int64(len(entries)) {
		t.Fatalf("items: %d, last %+v, count %d", len(w.Items), w.Items[len(w.Items)-1], w.Count)
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("я", paneTextMax)
	p.add([]*jsvalue.Object{
		jsvalue.NewObject("id", "e1", "type", "exec-command-stream", "timestamp", 1.0, "command", big),
		jsvalue.NewObject("id", "e2", "type", "exec-command-stream", "timestamp", 2.0, "command", big),
		jsvalue.NewObject("id", "e3", "type", "exec-command-stream", "timestamp", 3.0, "command", big),
		jsvalue.NewObject("id", "e4", "type", "exec-command-stream", "timestamp", 4.0, "command", big),
	})
	w = p.whole()
	if n := len(w.Items); n == 0 || n > 3 || w.Items[n-1].ID != "e4" || len(w.Items[n-1].Text) > paneTextMax {
		t.Fatalf("heavy items kept: %d", n)
	}
}
