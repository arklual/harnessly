package convo

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// fakeRefs names a design by its id and remembers which conversation pointed at which design.
type fakeRefs struct {
	linked []string
}

func (f *fakeRefs) Line(conv string, r wire.MessageRef) (string, wire.RefuseCode) {
	if r.ID == "gone" {
		return "", wire.RefuseNotFound
	}
	f.linked = append(f.linked, conv+"→"+r.ID)
	return "Design \"" + r.Title + "\": the folder /lib/" + r.ID, ""
}

func refsRig(t *testing.T, refs Refs) *rig {
	t.Helper()
	r := newRig(t)
	r.rt = NewInline(Deps{MachineID: "m1", Publisher: r.pub, Machine: r.machine, Launcher: r.launcher, Clock: r.clock,
		Store: NewStore(filepath.Join(r.dir, "v2")), Limits: wire.DefaultLimits, Refs: refs})
	r.open()
	return r
}

func (r *rig) sendRefs(id, text string, refs ...wire.MessageRef) wire.CmdResult {
	r.t.Helper()
	return r.do(wire.SendCmd{OpID: id, ClientID: id, ConversationID: r.id, Text: text, Mode: wire.ModeQueue,
		Kind: wire.KindPrompt, AttachmentIDs: []string{}, Refs: refs})
}

var (
	designRef = wire.MessageRef{Kind: wire.RefDesign, ID: "landing", Title: "Landing"}
	docRef    = wire.MessageRef{Kind: wire.RefDoc, ID: "pay", Title: "Pay", Target: "https://docs.example/a/pay"}
)

// A message with references reaches the agent as the person's words with the block under them; the message keeps its
// references, so the page draws them as chips, and the design names this conversation among its chats.
func TestSendWritesTheReferencesUnderTheWords(t *testing.T) {
	refs := &fakeRefs{}
	r := refsRig(t, refs)
	if res := r.sendRefs("m1", "Compare them", designRef, docRef); res.R != wire.ResultQueued && res.R != wire.ResultAccepted {
		t.Fatalf("send: %+v", res)
	}
	m := r.message("m1")
	want := "Compare them\n\n<attached>\nAttached to the message:\n" +
		`<ref kind="design" id="landing" title="Landing"/> Design "Landing": the folder /lib/landing` + "\n" +
		`<ref kind="doc" id="pay" title="Pay"/> Document "Pay" — https://docs.example/a/pay (read it before you answer).` +
		"\n</attached>"
	if m.Text != want {
		t.Fatalf("text:\n%s\nwant:\n%s", m.Text, want)
	}
	if !slices.Equal(m.Refs, []wire.MessageRef{designRef, docRef}) {
		t.Fatalf("refs: %+v", m.Refs)
	}
	if !slices.Equal(refs.linked, []string{convID + "→landing"}) {
		t.Fatalf("linked: %v", refs.linked)
	}
	r.tick(time.Second)
	typed := strings.Join(r.pane.calls, "\n")
	if !strings.Contains(typed, "<attached>") || !strings.Contains(typed, "Compare them") {
		t.Fatalf("typed: %q", typed)
	}
}

// The same client id again is the same message: the block is not appended twice, the design not linked again.
func TestSendWithReferencesIsIdempotent(t *testing.T) {
	refs := &fakeRefs{}
	r := refsRig(t, refs)
	first := r.sendRefs("m1", "", docRef)
	again := r.sendRefs("m1", "", docRef)
	if first.Seq != again.Seq || first.R != again.R {
		t.Fatalf("again: %+v, first %+v", again, first)
	}
	n := 0
	for _, m := range r.snap().Messages {
		if m.ClientID == "m1" {
			n++
			if strings.Count(m.Text, "<attached>") != 1 || !strings.HasPrefix(m.Text, "<attached>") {
				t.Fatalf("text: %q", m.Text)
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d copies", n)
	}
}

// A design the library does not have is refused, and so is any design where the machine has no library.
func TestSendRefusesADesignItCannotName(t *testing.T) {
	r := refsRig(t, &fakeRefs{})
	if res := r.sendRefs("m1", "x", wire.MessageRef{Kind: wire.RefDesign, ID: "gone", Title: "Gone"}); res.Code != wire.RefuseNotFound {
		t.Fatalf("gone: %+v", res)
	}
	plain := refsRig(t, nil)
	if res := plain.sendRefs("m2", "x", designRef); res.Code != wire.RefuseUnsupported {
		t.Fatalf("no library: %+v", res)
	}
	if res := plain.sendRefs("m3", "x", docRef); res.R != wire.ResultQueued && res.R != wire.ResultAccepted {
		t.Fatalf("a document needs no library: %+v", res)
	}
}
