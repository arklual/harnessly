package outbox

import (
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The picture path of the capture claude-2.1.310-picture-path-left-in-box.
const leftPath = "/home/user/.acme-ai/v2/uploads/up_OHCCK3DYVHEK5WVG.png"

// The prod case: a message with a picture whose paste Claude read too late for its try. The try did not end (here its
// Enter failed), the path landed afterwards as plain text, and the next message waited on «text in the terminal's
// input line». The path is one the harness pasted: it is cleared, and the next message goes in.
func TestAPicturePathLeftInTheBoxIsClearedNotWaitedOn(t *testing.T) {
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	left := tui(t, "claude-2.1.310-picture-path-left-in-box.ansi.txt")
	r := newRig(t, idle, wire.StateIdle)
	// The paste shows nothing while the typist waits for its reference.
	r.pane.onSend = func(string) {}
	ch, code := r.box.Send(Draft{ClientID: "m1", Text: "what is this", Images: []string{leftPath}, Kind: wire.KindPrompt,
		Mode: wire.ModeQueue}, r.agent.State, r.clock.Now())
	if code != "" {
		t.Fatal(code)
	}
	r.record(ch)
	// The try ends without its Enter.
	r.pane.fail = "keys:Enter"
	r.pump()
	r.pane.fail = ""
	if st := r.state("m1"); st.K != wire.MsgQueued {
		t.Fatalf("m1: %+v", st)
	}
	// It lands now, as text; the person sends another message.
	r.pane.screen = left
	r.send("m2", "and this too", wire.ModeQueue)
	r.box.Cancel("m1", r.clock.Now()) // whatever became of m1, its path is the harness's
	r.pane.onSend = func(call string) {
		if call == "keys:C-u" || call == "keys:Enter" || strings.HasPrefix(call, "line:") {
			r.pane.screen = idle
		}
	}
	r.pump()
	if st := r.state("m2"); st.K != wire.MsgInInput {
		t.Fatalf("m2 waits on our own path: %s %v", r.trail("m2"), r.typed())
	}
	if got := r.typed(); got[len(got)-1] != "line:and this too" {
		t.Fatalf("typed: %v", got)
	}
}

// Only the harness's paths: a draft of the person that holds a path, or a path and words, is not touched.
func TestOnlyOurPathsAreLeftovers(t *testing.T) {
	left := typist.Read(tui(t, "claude-2.1.310-picture-path-left-in-box.ansi.txt"), wire.ProviderClaude)
	if !typist.PathLeftover(left, wire.ProviderClaude, []string{"/elsewhere/a.png", leftPath}) {
		t.Fatalf("our path is not seen: %q", left.Box)
	}
	if typist.PathLeftover(left, wire.ProviderClaude, []string{"/home/user/.acme-ai/v2/uploads/up_OTHER.png"}) {
		t.Fatal("someone's path taken for ours")
	}
	words := typist.Read(withInput(t, leftPath+" look at this"), wire.ProviderClaude)
	if typist.PathLeftover(words, wire.ProviderClaude, []string{leftPath}) {
		t.Fatal("a path with words taken for a leftover")
	}
}
