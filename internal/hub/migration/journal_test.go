package migration

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const move = "0001-data-to-hub"

func openJournal(t *testing.T) *Journal {
	t.Helper()
	db, err := store.Open(context.Background(), "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	return New(db, func() time.Time { at = at.Add(time.Second); return at })
}

func obj(kind, id, state string) hubproto.MigrationObject {
	o := hubproto.MigrationObject{Kind: kind, Object: id, State: state, Sha256: strings.Repeat("a", 64), Size: 10}
	if state == hubproto.MigrationFailed {
		o.Code, o.Error = "MIG-READ", "permission denied"
	}
	return o
}

func report(t *testing.T, j *Journal, machine string, total int64, objs ...hubproto.MigrationObject) hubproto.MigrationAnswer {
	t.Helper()
	ans, err := j.Report(context.Background(), machine, "", hubproto.MigrationReport{Migration: move, Version: "0.17.0", Total: total, Objects: objs})
	if err != nil {
		t.Fatal(err)
	}
	return ans
}

func status(t *testing.T, j *Journal, machine string) hubproto.MigrationStatus {
	t.Helper()
	st, err := j.Status(context.Background(), machine, "")
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestTheJournalCountsWhatIsMovedAndWhatWaits(t *testing.T) {
	j := openJournal(t)
	report(t, j, "m1", 4, obj("uploads", "up_1", hubproto.MigrationVerified), obj("files", "f1", hubproto.MigrationPending))
	st := status(t, j, "m1")
	if st.Total != 4 || st.Verified != 1 || st.Pending != 3 || st.Failed != 0 || st.FinishedAt != "" {
		t.Fatalf("status = %+v, want 1 of 4 verified, 3 waiting", st)
	}
	if st.Doc != "docs/migrations/"+move+".md" {
		t.Fatalf("doc = %q", st.Doc)
	}
	report(t, j, "m1", 4, obj("files", "f1", hubproto.MigrationVerified), obj("files", "f2", hubproto.MigrationVerified),
		obj("handoff", "h1", hubproto.MigrationVerified))
	st = status(t, j, "m1")
	if st.Verified != 4 || st.Pending != 0 || st.FinishedAt == "" || !st.MigrationDone() {
		t.Fatalf("status = %+v, want the move over", st)
	}
	if line := Line(st); line != "m1: 4 of 4, done" {
		t.Fatalf("line = %q", line)
	}
}

func TestAFailureCountsAttemptsAndAPersonAsksToRetry(t *testing.T) {
	j := openJournal(t)
	ctx := context.Background()
	report(t, j, "m1", 2, obj("uploads", "up_1", hubproto.MigrationVerified), obj("uploads", "up_2", hubproto.MigrationFailed))
	report(t, j, "m1", 2, obj("uploads", "up_2", hubproto.MigrationFailed))
	st := status(t, j, "m1")
	if st.Failed != 1 || len(st.Failures) != 1 || st.Failures[0].Attempts != 2 || st.Failures[0].Code != "MIG-READ" {
		t.Fatalf("status = %+v, want one failure after two attempts", st)
	}
	if line := Line(st); line != "m1: 1 of 2, 1 failed (MIG-READ ×1)" {
		t.Fatalf("line = %q", line)
	}
	if n, err := j.Ask(ctx, "m1", AskRetry, nil); err != nil || n != 1 {
		t.Fatalf("retry = %d, %v", n, err)
	}
	if st := status(t, j, "m1"); st.Retrying != 1 {
		t.Fatalf("retrying = %d, want 1", st.Retrying)
	}
	ans := report(t, j, "m1", 2)
	if len(ans.Retry) != 1 || ans.Retry[0] != (hubproto.MigrationKey{Kind: "uploads", Object: "up_2"}) {
		t.Fatalf("answer = %+v, want up_2 to retry", ans)
	}
	ans = report(t, j, "m1", 2, obj("uploads", "up_2", hubproto.MigrationVerified))
	if len(ans.Retry) != 0 {
		t.Fatalf("answer = %+v: a reported object is asked nothing more", ans)
	}
	if st := status(t, j, "m1"); !st.MigrationDone() || st.Failed != 0 || len(st.Failures) != 0 {
		t.Fatalf("status = %+v, want done", st)
	}
}

func TestASkippedObjectStaysSkippedAndTheMoveIsOverWithoutIt(t *testing.T) {
	j := openJournal(t)
	ctx := context.Background()
	report(t, j, "m1", 2, obj("files", "f1", hubproto.MigrationVerified), obj("files", "f2", hubproto.MigrationFailed))
	if n, err := j.Ask(ctx, "m1", AskSkip, []hubproto.MigrationKey{{Kind: "files", Object: "f2"}}); err != nil || n != 1 {
		t.Fatalf("skip = %d, %v", n, err)
	}
	st := status(t, j, "m1")
	if st.Skipped != 1 || st.Failed != 0 || st.FinishedAt == "" {
		t.Fatalf("status = %+v, want the move over with one left on the machine", st)
	}
	ans := report(t, j, "m1", 2, obj("files", "f2", hubproto.MigrationFailed))
	if len(ans.Skip) != 1 {
		t.Fatalf("answer = %+v, want f2 skipped", ans)
	}
	if st := status(t, j, "m1"); st.Skipped != 1 || st.Failed != 0 {
		t.Fatalf("status = %+v: a failure reported after the skip keeps it skipped", st)
	}
	report(t, j, "m1", 2, obj("files", "f2", hubproto.MigrationVerified))
	if st := status(t, j, "m1"); st.Verified != 2 || st.Skipped != 0 {
		t.Fatalf("status = %+v: an object moved after all is verified", st)
	}
}

func TestMachinesKeepJournalsOfTheirOwn(t *testing.T) {
	j := openJournal(t)
	report(t, j, "m1", 1, obj("files", "f1", hubproto.MigrationFailed))
	report(t, j, "m2", 1, obj("files", "f1", hubproto.MigrationVerified))
	if n, err := j.Ask(context.Background(), "m2", AskRetry, nil); err != nil || n != 0 {
		t.Fatalf("retry on m2 = %d, %v: m2 has nothing failed", n, err)
	}
	sum, err := j.Summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sum) != 2 || sum[0].Machine != "m1" || sum[1].Machine != "m2" {
		t.Fatalf("summary = %+v, want the unfinished m1 first", sum)
	}
	if _, err := j.Status(context.Background(), "m3", ""); err != ErrNoMove {
		t.Fatalf("status of a machine that never moved = %v", err)
	}
}

func TestABadReportIsRefused(t *testing.T) {
	j := openJournal(t)
	for name, rep := range map[string]hubproto.MigrationReport{
		"no id":      {Total: 1},
		"bad id":     {Migration: "../x", Total: 1},
		"no kind":    {Migration: move, Objects: []hubproto.MigrationObject{{Object: "x", State: hubproto.MigrationPending}}},
		"bad state":  {Migration: move, Objects: []hubproto.MigrationObject{{Kind: "files", Object: "x", State: "lost"}}},
		"bad code":   {Migration: move, Objects: []hubproto.MigrationObject{{Kind: "files", Object: "x", State: "failed", Code: "\x1b[2J"}}},
		"bad kind":   {Migration: move, Objects: []hubproto.MigrationObject{{Kind: "Files\n", Object: "x", State: "failed"}}},
		"bad object": {Migration: move, Objects: []hubproto.MigrationObject{{Kind: "files", Object: "x\x1b", State: "failed"}}},
	} {
		if _, err := j.Report(context.Background(), "m1", "", rep); err == nil {
			t.Errorf("%s: taken", name)
		}
	}
	if _, err := j.Ask(context.Background(), "m1", "forget", nil); err == nil {
		t.Error("an unknown ask was taken")
	}
}
