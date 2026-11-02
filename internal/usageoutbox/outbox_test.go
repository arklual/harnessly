package usageoutbox

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func call(key string) hubproto.UsageRecord {
	return hubproto.UsageRecord{Kind: hubproto.UsageCall, Call: &hubproto.UsageCallFact{Provider: "claude", MsgKey: key, SessionKey: "s", Model: "m", Input: 1}}
}

func fill(t *testing.T, o *Outbox, n int) {
	t.Helper()
	recs := make([]hubproto.UsageRecord, n)
	for i := range recs {
		recs[i] = call(string(rune('a' + i%26)))
	}
	if _, err := o.Append(recs); err != nil {
		t.Fatal(err)
	}
}

func open(t *testing.T, dir string) *Outbox {
	t.Helper()
	o, err := Open(dir, func() time.Time { return time.UnixMilli(1_700_000_000_000) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { o.Close() })
	return o
}

func TestReadIsSequentialAndRepeatable(t *testing.T) {
	o := open(t, t.TempDir())
	if o.Epoch() == "" {
		t.Fatal("no epoch")
	}
	// Nothing yet: an empty batch whose last is the cursor.
	res, err := o.Read("", 0, 10)
	if err != nil || len(res.Records) != 0 || res.Last != 0 || res.More || res.Epoch != o.Epoch() {
		t.Fatalf("%+v %v", res, err)
	}
	fill(t, o, 5)
	first, err := o.Read(o.Epoch(), 0, 3)
	if err != nil || len(first.Records) != 3 || first.Last != 3 || !first.More {
		t.Fatalf("%+v %v", first, err)
	}
	for i, r := range first.Records {
		if r.Seq != int64(i+1) || r.At != 1_700_000_000_000 || r.Call == nil {
			t.Fatalf("%d: %+v", i, r)
		}
	}
	// The same read gives the same answer until it is acknowledged.
	again, _ := o.Read(o.Epoch(), 0, 3)
	if !reflect.DeepEqual(first, again) {
		t.Fatalf("%+v\n%+v", first, again)
	}
	rest, _ := o.Read(o.Epoch(), 3, 10)
	if len(rest.Records) != 2 || rest.Records[0].Seq != 4 || rest.Last != 5 || rest.More {
		t.Fatalf("%+v", rest)
	}
	// Past the end: empty, last = after.
	end, _ := o.Read(o.Epoch(), 5, 10)
	if len(end.Records) != 0 || end.Last != 5 || end.More {
		t.Fatalf("%+v", end)
	}
	// A record never changes: appending more does not alter what was read.
	fill(t, o, 2)
	if again, _ := o.Read(o.Epoch(), 0, 3); !reflect.DeepEqual(first.Records, again.Records) || !again.More {
		t.Fatalf("%+v", again)
	}
}

func TestReadClampsMax(t *testing.T) {
	o := open(t, t.TempDir())
	fill(t, o, hubproto.UsageReadMax+50)
	for _, c := range []struct{ max, want int }{{0, 1}, {-5, 1}, {1, 1}, {7, 7}, {hubproto.UsageReadMax, hubproto.UsageReadMax}, {1 << 20, hubproto.UsageReadMax}} {
		res, err := o.Read(o.Epoch(), 0, c.max)
		if err != nil || len(res.Records) != c.want || !res.More {
			t.Errorf("max %d: got %d (%v)", c.max, len(res.Records), err)
		}
	}
}

func TestAnotherEpochReadsFromTheStart(t *testing.T) {
	o := open(t, t.TempDir())
	fill(t, o, 5)
	// The hub's cursor belongs to an epoch that is gone: the batch starts at the new epoch's beginning.
	res, _ := o.Read("some-old-epoch", 4, 10)
	if res.Epoch != o.Epoch() || len(res.Records) != 5 || res.Records[0].Seq != 1 || res.Last != 5 {
		t.Fatalf("%+v", res)
	}
	res, _ = o.Read("", 4, 10)
	if len(res.Records) != 5 {
		t.Fatalf("%+v", res)
	}
}

func TestAckDropsAndSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	o := open(t, dir)
	epoch := o.Epoch()
	fill(t, o, 6)
	// An ack of another epoch changes nothing.
	if err := o.Ack("other", 6); err != nil || o.Pending() != 6 {
		t.Fatalf("%v %d", err, o.Pending())
	}
	if err := o.Ack(epoch, 4); err != nil || o.Pending() != 2 {
		t.Fatalf("%v %d", err, o.Pending())
	}
	// A late, smaller ack is a no-op.
	if err := o.Ack(epoch, 2); err != nil || o.Pending() != 2 {
		t.Fatalf("%v %d", err, o.Pending())
	}
	// A cursor behind the ack reads from the first record that is left.
	res, _ := o.Read(epoch, 0, 10)
	if len(res.Records) != 2 || res.Records[0].Seq != 5 {
		t.Fatalf("%+v", res)
	}
	o.Close()

	o = open(t, dir)
	if o.Epoch() != epoch || o.Last() != 6 || o.Pending() != 2 {
		t.Fatalf("epoch %s last %d pending %d", o.Epoch(), o.Last(), o.Pending())
	}
	res, _ = o.Read(epoch, 4, 10)
	if len(res.Records) != 2 || res.Records[1].Seq != 6 {
		t.Fatalf("%+v", res)
	}
	// New records go on from the old seq.
	fill(t, o, 1)
	if o.Last() != 7 {
		t.Fatalf("last %d", o.Last())
	}
	// Everything acknowledged: the seq goes on, never back to 1.
	if err := o.Ack(epoch, 100); err != nil || o.Pending() != 0 || o.Last() != 7 {
		t.Fatalf("%v %d %d", err, o.Pending(), o.Last())
	}
	o.Close()
	o = open(t, dir)
	fill(t, o, 1)
	if o.Last() != 8 || o.Pending() != 1 {
		t.Fatalf("last %d pending %d", o.Last(), o.Pending())
	}
}

func TestWipedFolderIsANewEpoch(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "usage-outbox")
	o := open(t, dir)
	old := o.Epoch()
	fill(t, o, 3)
	o.Close()
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	o = open(t, dir)
	if o.Epoch() == old || o.Last() != 0 {
		t.Fatalf("epoch %s (was %s) last %d", o.Epoch(), old, o.Last())
	}
	// Losing only the meta file is the same: the records belong to an epoch nobody can name.
	fill(t, o, 2)
	o.Close()
	cur := o.Epoch()
	if err := os.Remove(filepath.Join(dir, metaFile)); err != nil {
		t.Fatal(err)
	}
	o = open(t, dir)
	if o.Epoch() == cur || o.Last() != 0 || o.Pending() != 0 {
		t.Fatalf("epoch %s last %d pending %d", o.Epoch(), o.Last(), o.Pending())
	}
}

func TestTornLastLineIsCut(t *testing.T) {
	dir := t.TempDir()
	o := open(t, dir)
	fill(t, o, 3)
	o.Close()
	f, err := os.OpenFile(filepath.Join(dir, recordsFile), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"seq":4,"kind":"ca`)
	f.Close()
	o = open(t, dir)
	if o.Last() != 3 {
		t.Fatalf("last %d", o.Last())
	}
	fill(t, o, 1)
	res, err := o.Read(o.Epoch(), 0, 10)
	if err != nil || len(res.Records) != 4 || res.Records[3].Seq != 4 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestCompactionKeepsTheRest(t *testing.T) {
	dir := t.TempDir()
	o := open(t, dir)
	epoch := o.Epoch()
	// Enough records that the acknowledged head passes compactBytes.
	fillRec(t, o, call("k"), 8000)
	total := o.Last()
	if err := o.Ack(epoch, total-5); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(filepath.Join(dir, recordsFile))
	if st.Size() > 4096 {
		t.Fatalf("file still %d bytes", st.Size())
	}
	res, err := o.Read(epoch, total-5, 100)
	if err != nil || len(res.Records) != 5 || res.Records[0].Seq != total-4 {
		t.Fatalf("%+v %v", res, err)
	}
	fill(t, o, 1)
	o.Close()
	o = open(t, dir)
	if res, _ := o.Read(epoch, total-5, 100); len(res.Records) != 6 || res.Last != total+1 {
		t.Fatalf("%+v", res)
	}
}

func fillRec(t *testing.T, o *Outbox, r hubproto.UsageRecord, n int) {
	t.Helper()
	recs := make([]hubproto.UsageRecord, n)
	for i := range recs {
		recs[i] = r
	}
	if _, err := o.Append(recs); err != nil {
		t.Fatal(err)
	}
}

func TestAskedMarkerIsSetByTheFirstReadAndKept(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "usage-outbox")
	o := open(t, dir)
	fill(t, o, 2)
	if o.Asked() {
		t.Fatal("asked before any read")
	}
	o.Close()
	if o = open(t, dir); o.Asked() {
		t.Fatal("writing records marks the outbox as asked")
	}
	if _, err := o.Read("", 0, 10); err != nil || !o.Asked() {
		t.Fatalf("%v %v", err, o.Asked())
	}
	// Acks keep the marker; a restart still knows the hub read.
	if err := o.Ack(o.Epoch(), 1); err != nil || !o.Asked() {
		t.Fatalf("%v %v", err, o.Asked())
	}
	o.Close()
	if o = open(t, dir); !o.Asked() {
		t.Fatal("marker lost on restart")
	}
	// A new epoch is a new outbox the hub has not read.
	o.Close()
	if err := os.Remove(filepath.Join(dir, metaFile)); err != nil {
		t.Fatal(err)
	}
	if o = open(t, dir); o.Asked() {
		t.Fatal("marker survived a new epoch")
	}
}

func TestPendingBytesFollowsAcks(t *testing.T) {
	o := open(t, t.TempDir())
	if o.PendingBytes() != 0 {
		t.Fatal(o.PendingBytes())
	}
	fill(t, o, 4)
	all := o.PendingBytes()
	if all <= 0 {
		t.Fatal(all)
	}
	if err := o.Ack(o.Epoch(), 2); err != nil || o.PendingBytes() != all/2 {
		t.Fatalf("%v %d of %d", err, o.PendingBytes(), all)
	}
	_ = o.Ack(o.Epoch(), 4)
	if o.PendingBytes() != 0 {
		t.Fatal(o.PendingBytes())
	}
}
