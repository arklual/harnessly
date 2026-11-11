package wire

import (
	"testing"
	"time"
)

// A ring grows with its channel, drops what is past RingAge when the next event comes, and gives its buffer back once
// empty (a buffer of RingSize per channel was ~45 MB for a machine of hundreds of chats).
func TestRingGrowsTrimsAndReleases(t *testing.T) {
	var r ring
	t0 := time.Unix(1000, 0)
	for i := 1; i <= 5; i++ {
		r.push(ringItem{seq: int64(i), at: t0})
	}
	if len(r.buf) > 8 || r.n != 5 {
		t.Fatalf("buf %d n %d", len(r.buf), r.n)
	}
	for i := 6; i <= RingSize+50; i++ {
		r.push(ringItem{seq: int64(i), at: t0})
	}
	if len(r.buf) != RingSize || r.n != RingSize || r.at(0).seq != 51 || r.at(r.n-1).seq != RingSize+50 {
		t.Fatalf("buf %d n %d first %d", len(r.buf), r.n, r.at(0).seq)
	}
	late := t0.Add(RingAge + time.Second)
	r.push(ringItem{seq: 9999, at: late})
	if r.n != 1 || len(r.buf) > 8 {
		t.Fatalf("after age: n %d buf %d", r.n, len(r.buf))
	}
	r.trim(late.Add(RingAge + time.Second))
	if r.buf != nil || r.n != 0 {
		t.Fatalf("not released: n %d", r.n)
	}
}
