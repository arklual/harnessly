package convo

import (
	"context"
	"fmt"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

func (r *rig) draftFrom(device string, opID, key, text string, rev int64) wire.CmdResult {
	r.t.Helper()
	return r.rt.Do(context.Background(), device, wire.DraftSetCmd{OpID: opID, Key: key, Text: text, Rev: rev})
}

func (r *rig) draftsNow() map[string]wire.Draft {
	r.t.Helper()
	snap, err := r.rt.Conversations(context.Background())
	if err != nil {
		r.t.Fatal(err)
	}
	out := map[string]wire.Draft{}
	for _, d := range snap.Drafts {
		out[d.Key] = d
	}
	return out
}

func TestADraftTakesOnlyTheNextRevision(t *testing.T) {
	r := newRig(t)
	first := r.draftFrom("dev-a", "d1", "tab-one", "Привет", 1)
	if first.R != wire.ResultAccepted || first.Ch != wire.ChannelConvs || first.Rev == nil || *first.Rev != 1 {
		t.Fatalf("first write: %+v", first)
	}
	evs := r.events(wire.EvDraftUpsert)
	if len(evs) != 1 || evs[0].seq != first.Seq {
		t.Fatalf("answer at %d, events %+v", first.Seq, evs)
	}
	d := evs[0].e.(wire.DraftUpsertEv).Draft
	if d.Key != "tab-one" || d.Text != "Привет" || d.Rev != 1 || d.DeviceID != "dev-a" || d.At != 1_800_000_000_000 {
		t.Fatalf("event: %+v", d)
	}

	// Another device that has not seen rev 1 cannot write rev 1 again, nor jump ahead.
	if res := r.draftFrom("dev-b", "d2", "tab-one", "старое", 1); res.R != wire.ResultRefused || res.Code != wire.RefuseConflict {
		t.Fatalf("stale write: %+v", res)
	}
	if res := r.draftFrom("dev-b", "d3", "tab-one", "вперёд", 3); res.R != wire.ResultRefused || res.Code != wire.RefuseConflict {
		t.Fatalf("a revision ahead: %+v", res)
	}
	if got := r.draftsNow()["tab-one"]; got.Text != "Привет" || got.Rev != 1 {
		t.Fatalf("a refused write changed the draft: %+v", got)
	}
	if len(r.events(wire.EvDraftUpsert)) != 1 {
		t.Fatal("a refused write was published")
	}
}

func TestAClearKeepsItsRevisionSoAStaleDeviceCannotBringTheTextBack(t *testing.T) {
	r := newRig(t)
	r.draftFrom("dev-a", "d1", "tab-one", "Проверь миграцию", 1)
	clear := r.draftFrom("dev-a", "d2", "tab-one", "", 2)
	if clear.R != wire.ResultAccepted {
		t.Fatalf("clear: %+v", clear)
	}
	// dev-b still holds rev 1 and writes the text it had as rev 2: refused.
	if res := r.draftFrom("dev-b", "d3", "tab-one", "Проверь миграцию", 2); res.Code != wire.RefuseConflict {
		t.Fatalf("resurrection: %+v", res)
	}
	got := r.draftsNow()["tab-one"]
	if got.Text != "" || got.Rev != 2 {
		t.Fatalf("after the clear: %+v", got)
	}
	// Having seen rev 2, the same device writes rev 3: what it types now wins.
	if res := r.draftFrom("dev-b", "d4", "tab-one", "новое", 3); res.R != wire.ResultAccepted {
		t.Fatalf("next write: %+v", res)
	}
}

func TestTheSameWriteAgainGetsTheSameAnswer(t *testing.T) {
	r := newRig(t)
	one := r.draftFrom("dev-a", "d1", "new", "черновик", 1)
	// A retry (same device, revision and text; the opId may differ) is the write already taken.
	again := r.draftFrom("dev-a", "d1-retry", "new", "черновик", 1)
	if again.R != wire.ResultAccepted || again.Seq != one.Seq || *again.Rev != 1 {
		t.Fatalf("retry: %+v vs %+v", again, one)
	}
	if len(r.events(wire.EvDraftUpsert)) != 1 {
		t.Fatal("a retry published again")
	}
	// The same revision and text from another device is not its write.
	if res := r.draftFrom("dev-b", "d2", "new", "черновик", 1); res.Code != wire.RefuseConflict {
		t.Fatalf("another device: %+v", res)
	}
}

func TestDraftsDoNotGoThroughTheOpRecords(t *testing.T) {
	r := newRig(t)
	for i := 1; i <= 3; i++ {
		r.draftFrom("dev-a", "same-op", "new", fmt.Sprintf("v%d", i), int64(i))
	}
	if got := r.draftsNow()["new"]; got.Text != "v3" || got.Rev != 3 {
		t.Fatalf("an opId used again is not a replay for a draft: %+v", got)
	}
	r.rt.opsMu.Lock()
	n := len(r.rt.ops)
	r.rt.opsMu.Unlock()
	if n != 0 {
		t.Fatalf("op records: %d", n)
	}
}

func TestABadDraftIsRefused(t *testing.T) {
	r := newRig(t)
	long := make([]rune, wire.DraftTextMax+1)
	for i := range long {
		long[i] = 'я'
	}
	cases := map[string]wire.DraftSetCmd{
		"key":  {OpID: "d1", Key: "chat-1", Text: "x", Rev: 1},
		"rev":  {OpID: "d2", Key: "new", Text: "x", Rev: 0},
		"text": {OpID: "d3", Key: "new", Text: string(long), Rev: 1},
		"opId": {Key: "new", Text: "x", Rev: 1},
	}
	for name, cmd := range cases {
		if res := r.rt.Do(context.Background(), "dev-a", cmd); res.Code != wire.RefuseBadRequest {
			t.Errorf("%s: %+v", name, res)
		}
	}
	if res := r.rt.Do(context.Background(), "", wire.DraftSetCmd{OpID: "d4", Key: "new", Text: "x", Rev: 1}); res.Code != wire.RefuseBadRequest {
		t.Errorf("no device: %+v", res)
	}
	if len(r.draftsNow()) != 0 {
		t.Fatal("a bad write was kept")
	}
}

func TestDraftsAreKeptOverARestart(t *testing.T) {
	r := newRig(t)
	r.draftFrom("dev-a", "d1", "tab-one", "до перезапуска", 1)
	r.draftFrom("dev-a", "d2", "new", "", 1)
	r.rt.Stop()
	r.rt = r.runtime()
	if err := r.rt.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := r.draftsNow()
	if got["tab-one"].Text != "до перезапуска" || got["tab-one"].Rev != 1 || got["new"].Rev != 1 || got["new"].Text != "" {
		t.Fatalf("after the restart: %+v", got)
	}
	// The revisions go on from where they were.
	if res := r.draftFrom("dev-b", "d3", "tab-one", "x", 1); res.Code != wire.RefuseConflict {
		t.Fatalf("stale after the restart: %+v", res)
	}
	// The write retried after the restart is shown again, under this run's seq.
	before := len(r.events(wire.EvDraftUpsert))
	again := r.draftFrom("dev-a", "d1", "tab-one", "до перезапуска", 1)
	if again.R != wire.ResultAccepted || again.Seq == 0 || len(r.events(wire.EvDraftUpsert)) != before+1 {
		t.Fatalf("retry after the restart: %+v", again)
	}
}

func TestTheBookKeepsTheMostRecentDrafts(t *testing.T) {
	b := draftBook{}
	for i := 0; i < wire.DraftsMax+3; i++ {
		key := fmt.Sprintf("tab-k%04d", i)
		b[key] = keptDraft{Draft: wire.Draft{Key: key, Rev: 1, DeviceID: "d", At: int64(i)}}
	}
	b.prune()
	if len(b) != wire.DraftsMax {
		t.Fatalf("kept %d", len(b))
	}
	for i := 0; i < 3; i++ {
		if _, ok := b[fmt.Sprintf("tab-k%04d", i)]; ok {
			t.Fatalf("the oldest %d stayed", i)
		}
	}
}

func TestADraftCarriesItsFilesAndAClearDropsThem(t *testing.T) {
	r := newRig(t)
	shot := []wire.AttachmentRef{{ID: "up_1", Name: "CleanShot.png", Kind: wire.AttachmentImage, Mime: "image/png"}}
	res := r.rt.Do(context.Background(), "dev-a", wire.DraftSetCmd{OpID: "d1", Key: "tab-one", Text: "смотри", Attachments: shot, Rev: 1})
	if res.R != wire.ResultAccepted {
		t.Fatalf("with files: %+v", res)
	}
	if got := r.draftsNow()["tab-one"]; len(got.Attachments) != 1 || got.Attachments[0].ID != "up_1" {
		t.Fatalf("files not kept: %+v", got)
	}
	// The same text without the files is another write, not the one already taken.
	if res := r.draftFrom("dev-a", "d2", "tab-one", "смотри", 1); res.Code != wire.RefuseConflict {
		t.Fatalf("same rev, other files: %+v", res)
	}
	r.draftFrom("dev-a", "d3", "tab-one", "", 2)
	if got := r.draftsNow()["tab-one"]; got.Text != "" || len(got.Attachments) != 0 || got.Rev != 2 {
		t.Fatalf("after the clear: %+v", got)
	}
	evs := r.events(wire.EvDraftUpsert)
	if last := evs[len(evs)-1].e.(wire.DraftUpsertEv).Draft; len(last.Attachments) != 0 {
		t.Fatalf("the clear's event still has files: %+v", last)
	}
}
