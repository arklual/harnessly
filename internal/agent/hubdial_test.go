package agent

import (
	"testing"
	"time"
)

func TestHubBackoffIsFullJitterInADoublingWindow(t *testing.T) {
	b := &hubBackoff{rnd: func() float64 { return 0.999999 }}
	var got []time.Duration
	for range 7 {
		got = append(got, b.next().Round(time.Second))
	}
	want := []int{1, 2, 4, 8, 16, 30, 30}
	for i, w := range want {
		if got[i] != time.Duration(w)*time.Second {
			t.Fatalf("top of the windows = %v, want %v s", got, want)
		}
	}
	b = &hubBackoff{rnd: func() float64 { return 0 }}
	for range 4 {
		if d := b.next(); d != 0 {
			t.Fatalf("bottom of a window = %v, want 0: the pause is drawn from the whole window", d)
		}
	}
	b = &hubBackoff{window: 8 * time.Second, rnd: func() float64 { return 0.5 }}
	if d := b.next(); d != 4*time.Second {
		t.Errorf("middle of an 8 s window = %v", d)
	}
}

func TestHubBackoffStartsOverOnlyAfterALinkThatSettled(t *testing.T) {
	b := &hubBackoff{rnd: func() float64 { return 0.999999 }}
	for range 4 {
		b.next()
	}
	b.ended(5 * time.Second) // the hub took the call and dropped the link at once
	if d := b.next().Round(time.Second); d != 16*time.Second {
		t.Errorf("after a short link the pause = %v, want it to keep growing (16s)", d)
	}
	b.ended(hubLinkSettled)
	if d := b.next().Round(time.Second); d != time.Second {
		t.Errorf("after a settled link the pause = %v, want 1s", d)
	}
}

func TestReplacedLinkWaitsLongerEachTime(t *testing.T) {
	b := &hubBackoff{rnd: func() float64 { return 0 }}
	prev := time.Duration(0)
	for i := range 12 {
		d := b.nextReplaced()
		if d < hubPauseMin/2 || d > hubReplacedPauseMax {
			t.Fatalf("pause %d = %v", i, d)
		}
		if d < prev {
			t.Fatalf("pause %d = %v is shorter than the one before (%v)", i, d, prev)
		}
		prev = d
	}
	if prev != hubReplacedPauseMax/2 {
		t.Errorf("the longest pause starts at %v, want %v", prev, hubReplacedPauseMax/2)
	}
	b.ended(time.Second) // a link taken over at once does not start the count over
	if d := b.nextReplaced(); d != hubReplacedPauseMax/2 {
		t.Errorf("after a short link = %v", d)
	}
	b.ended(hubLinkSettled)
	if d := b.nextReplaced(); d != hubPauseMin/2 {
		t.Errorf("after a settled link = %v, want %v", d, hubPauseMin/2)
	}
}
