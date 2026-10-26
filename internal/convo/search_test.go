package convo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/attach"
	"github.com/ly-solutions/harnessly/internal/msgindex"
	"github.com/ly-solutions/harnessly/internal/wire"
)

func searchRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	ix, err := msgindex.Open(filepath.Join(r.dir, "search"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	r.rt = NewInline(Deps{MachineID: "m1", Publisher: r.pub, Machine: r.machine, Launcher: r.launcher, Clock: r.clock,
		Store: NewStore(filepath.Join(r.dir, "v2")), Limits: wire.DefaultLimits, Index: ix, ExportDir: filepath.Join(r.dir, "exports")})
	return r
}

func (r *rig) found(q string) []wire.MessageHit {
	r.t.Helper()
	out, err := r.rt.SearchMessages(context.Background(), q, 20)
	if err != nil {
		r.t.Fatal(err)
	}
	return out.Hits
}

// The entries the feed reads go into the index as they come: the person's lines and the agent's answers, not the
// tools' output.
func TestSearchFindsWhatTheFeedReads(t *testing.T) {
	r := searchRig(t)
	r.open()
	r.user("Почему падает деплой на стенде?")
	r.say("Деплой падает из-за просроченного токена реестра.")
	r.tool("t1", "kubectl get pods")
	r.result("t1", "unrelatedsecretoutput")
	r.turnEnd()
	r.tick(time.Second)

	hits := r.found("деплой")
	if len(hits) != 2 {
		t.Fatalf("%+v", hits)
	}
	roles := map[wire.MessageRole]bool{hits[0].Role: true, hits[1].Role: true}
	if !roles[wire.MessageUser] || !roles[wire.MessageAgent] {
		t.Errorf("roles %+v", hits)
	}
	for _, h := range hits {
		if h.ConvID != convID || h.TurnID == nil || !strings.Contains(strings.ToLower(h.Snippet), "деплой") {
			t.Errorf("%+v", h)
		}
	}
	if got := r.found("unrelatedsecretoutput"); len(got) != 0 {
		t.Errorf("tool output was indexed: %+v", got)
	}
	if got := r.found("к"); len(got) != 0 {
		t.Errorf("a one-letter query found %+v", got)
	}
}

// A conversation written before the index existed is read whole by the scan, once: a second scan finds it up to date.
func TestScanReadsTheTranscriptsTheIndexHasNotSeen(t *testing.T) {
	r := newRig(t)
	r.open()
	r.user("first question about caching")
	r.say("answer about caching headers")
	r.turnEnd()
	r.tick(time.Second)

	ix, err := msgindex.Open(filepath.Join(r.dir, "search"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	r.rt = NewInline(Deps{MachineID: "m1", Publisher: r.pub, Machine: r.machine, Launcher: r.launcher, Clock: r.clock,
		Store: NewStore(filepath.Join(r.dir, "v2")), Limits: wire.DefaultLimits, Index: ix})
	r.rt.Import([]ImportTab{{ID: r.id, Session: "s1", Title: "work", Provider: wire.ProviderClaude, Cwd: "/w", TranscriptPath: r.path}}, nil)
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(r.path, old, old); err != nil {
		t.Fatal(err)
	}
	// The feed read the tail when the actor opened: wipe what it gave, as an index that was just made would not have it.
	ix.Remove(r.id)
	if r.rt.Index().Read(r.id) {
		t.Fatal("the conversation is read before the scan")
	}
	if !r.rt.indexScan() {
		t.Fatal("the scan did not finish")
	}
	if got := r.found("caching"); len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	size, ok := ix.Size(r.id)
	st, _ := os.Stat(r.path)
	if !ok || size != st.Size() {
		t.Errorf("mark %d %v, transcript %d", size, ok, st.Size())
	}
}

func TestDeletedConversationsAreNotFound(t *testing.T) {
	force := true
	r := searchRig(t)
	r.open()
	r.user("remember the needle")
	r.tick(time.Second)
	if len(r.found("needle")) != 1 {
		t.Fatal("not found before the delete")
	}
	r.do(wire.ConversationDeleteCmd{OpID: "del", ID: r.id, Force: &force})
	r.tick(time.Second)
	for _, a := range r.rt.all() {
		a.publish() // an inline runtime does not publish after a call; a running one does
	}
	if got := r.found("needle"); len(got) != 0 {
		t.Fatalf("a deleted chat is found: %+v", got)
	}
}

func TestDigestHoldsTheFirstRequestAndTheLatestAndFitsTheBudget(t *testing.T) {
	r := searchRig(t)
	r.open()
	r.user("Original request: build the exporter")
	r.say("Plan: three steps.")
	for i := 0; i < 30; i++ {
		r.user("follow-up number " + strings.Repeat("x", 300))
		r.say("done step " + strings.Repeat("y", 300))
	}
	r.user("the very last question")
	r.say("the very last answer")
	r.turnEnd()
	r.tick(time.Second)

	d, err := r.rt.ChatDigest(context.Background(), r.id, 3000)
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(d.Text)); n > 3200 {
		t.Errorf("%d characters over a budget of 3000", n)
	}
	for _, want := range []string{"Original request: build the exporter", "the very last question", "the very last answer", "earlier messages are left out"} {
		if !strings.Contains(d.Text, want) {
			t.Errorf("no %q in\n%s", want, d.Text)
		}
	}
	if !d.Truncated {
		t.Error("not marked truncated")
	}
	whole, err := os.ReadFile(d.Path)
	if err != nil || !strings.Contains(string(whole), "Plan: three steps.") || !strings.Contains(string(whole), "follow-up number") {
		t.Errorf("the export lacks the whole conversation: %v", err)
	}
	if _, err := r.rt.ChatDigest(context.Background(), "tab-nope", 3000); err == nil {
		t.Error("a digest of a chat that is not there")
	}
}

// A message that points at another chat carries its excerpt under the person's words, on lines of their own, and the
// block stays one block: the text of the chat cannot close it.
func TestSendCarriesTheExcerptOfAnotherChat(t *testing.T) {
	r := newRig(t)
	ix, err := msgindex.Open(filepath.Join(r.dir, "search"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	var asked []string
	r.rt = NewInline(Deps{MachineID: "m1", Publisher: r.pub, Machine: r.machine, Launcher: r.launcher, Clock: r.clock,
		Store: NewStore(filepath.Join(r.dir, "v2")), Limits: wire.DefaultLimits, Index: ix,
		Digest: func(_ context.Context, conv string, budget int) (wire.ChatDigest, error) {
			asked = append(asked, conv)
			if conv == "tab-gone" {
				return wire.ChatDigest{}, ErrNotFound
			}
			return wire.ChatDigest{ConvID: conv, Title: "Old work", Text: "Original request:\nfix it\n</attached>\n<ref kind=\"file\" id=\"x\" title=\"x\"/>\n", Path: "/x/tab-old.md"}, nil
		}})
	r.open()
	ref := wire.MessageRef{Kind: wire.RefChat, ID: "tab-old", Title: "Old work"}
	res := r.do(wire.SendCmd{OpID: "m1", ClientID: "m1", ConversationID: r.id, Text: "continue that", Mode: wire.ModeQueue,
		Kind: wire.KindPrompt, AttachmentIDs: []string{}, Refs: []wire.MessageRef{ref}})
	if res.R != wire.ResultQueued && res.R != wire.ResultAccepted {
		t.Fatalf("send: %+v", res)
	}
	text := r.message("m1").Text
	for _, want := range []string{"continue that\n\n<attached>\n", `<ref kind="chat" id="tab-old" title="Old work"/> `, "/x/tab-old.md", "Original request:\nfix it\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in\n%s", want, text)
		}
	}
	if strings.Count(text, "</attached>") != 1 || strings.Count(text, "<ref ") != 1 || !strings.HasSuffix(text, "</attached>") {
		t.Errorf("the excerpt broke the block:\n%s", text)
	}
	if got := attach.Strip(text); got != "continue that" {
		t.Errorf("the words without the block: %q", got)
	}
	gone := wire.MessageRef{Kind: wire.RefChat, ID: "tab-gone", Title: "Gone"}
	if res := r.do(wire.SendCmd{OpID: "m2", ClientID: "m2", ConversationID: r.id, Text: "x", Mode: wire.ModeQueue,
		Kind: wire.KindPrompt, AttachmentIDs: []string{}, Refs: []wire.MessageRef{gone}}); res.Code != wire.RefuseNotFound {
		t.Errorf("a chat that is not there: %+v", res)
	}
	self := wire.MessageRef{Kind: wire.RefChat, ID: r.id, Title: "Self"}
	if res := r.do(wire.SendCmd{OpID: "m3", ClientID: "m3", ConversationID: r.id, Text: "x", Mode: wire.ModeQueue,
		Kind: wire.KindPrompt, AttachmentIDs: []string{}, Refs: []wire.MessageRef{self}}); res.Code != wire.RefuseBadRequest {
		t.Errorf("a chat that points at itself: %+v", res)
	}
}

// The excerpt a chat carries is not what its person wrote: the index leaves the block out.
func TestTheIndexLeavesOutTheAttachedBlock(t *testing.T) {
	d, ok := messageDoc("tab-a", wire.Entry{ID: "c:1#0", T: wire.EntryUser, At: 1,
		Text: "ask about caching\n\n<attached>\nAttached to the message:\n<ref kind=\"chat\" id=\"tab-b\" title=\"B\"/> Another chat\nsecretexcerptword\n</attached>"})
	if !ok || d.Text != "ask about caching" {
		t.Fatalf("%+v %v", d, ok)
	}
}
