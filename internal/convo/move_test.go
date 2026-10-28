package convo

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/outbox"
	"github.com/ly-solutions/harnessly/internal/wire"
)

type blockSet map[string]bool

func (b blockSet) Blocked(conv string) bool { return b[conv] }

func TestMoveSnapshotLeavesNoDraftAndMoveOpenStartsOnItsOwn(t *testing.T) {
	r, _ := handoffRig(t)
	r.twoTurns()
	info, ok := r.rt.MoveInfo(r.id)
	if !ok || info.Busy || !info.HasHistory {
		t.Fatalf("info: %+v %v", info, ok)
	}
	fence, ok := r.rt.MoveFence(r.id)
	if !ok || fence == "" {
		t.Fatal("no fence")
	}
	history, got, err := r.rt.MoveSnapshot(r.id, 1<<20)
	if err != nil || got != fence {
		t.Fatalf("snapshot: %v fence %q/%q", err, got, fence)
	}
	if !bytes.Contains(history, []byte("second answer")) || bytes.Contains(history, []byte(`"sessionId"`)) || bytes.Contains(history, []byte(`"model"`)) {
		t.Fatalf("history is not neutral or not whole: %s", history)
	}
	if _, _, err := r.rt.MoveSnapshot(r.id, 10); err == nil {
		t.Fatal("a history past the limit was read")
	}

	id, code := r.rt.MoveOpen(MoveOpen{Title: "Work", Cwd: r.dir, History: history, Provider: wire.ProviderClaude})
	if code != "" || id == "" {
		t.Fatalf("open: %q %q", id, code)
	}
	launch := r.launcher.launched[len(r.launcher.launched)-1]
	if launch.ID != id || launch.First == "" || len(launch.First) >= outbox.MaxCarriedText || !strings.Contains(launch.First, `mode="history"`) {
		t.Fatalf("launch: %+v", launch)
	}
	// The first prompt and the record name nothing of where the work came from.
	rec := r.recordOf(id)
	if rec.Origin != nil || rec.Title != "Work" {
		t.Fatalf("record: %+v", rec)
	}
	for _, leak := range []string{r.id, "tab-", "claude-code"} {
		if leak == "tab-" {
			continue
		}
		if strings.Contains(launch.First, leak) {
			t.Errorf("the first prompt carries %q", leak)
		}
	}
	if got := claimedArtifact(t, r.dir); !bytes.Contains(got, []byte("second answer")) {
		t.Fatalf("claimed history lost: %d bytes", len(got))
	}

	// A conversation made by a move can be deleted again, and twice.
	r.rt.MoveDelete(id)
	r.rt.MoveDelete(id)
	r.rt.actor(id).publish() // an inline runtime does not publish after a call; a running one does
	if _, ok := r.rt.Record(id); ok {
		t.Fatal("the undone conversation is still there")
	}
}

func TestBlockedConversationTakesNoMessages(t *testing.T) {
	r, _ := handoffRig(t)
	blocked := blockSet{}
	prev := r.deps
	r.deps = func(d *Deps) { prev(d); d.Moves = blocked }
	r.rt = r.runtime()
	r.twoTurns()
	if res := r.send("m1", "still here", wire.ModeQueue); res.R == wire.ResultRefused {
		t.Fatalf("refused while not moved: %+v", res)
	}
	blocked[r.id] = true
	res := r.send("m2", "after the move", wire.ModeQueue)
	if res.R != wire.ResultRefused || res.Code != wire.RefuseConflict {
		t.Fatalf("a moved conversation took a message: %+v", res)
	}
	delete(blocked, r.id)
	if res := r.send("m3", "released", wire.ModeQueue); res.R == wire.ResultRefused {
		t.Fatalf("refused after the hold ended: %+v", res)
	}
}
