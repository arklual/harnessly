package ratelimit

import (
	"testing"
	"time"
)

func TestAllow(t *testing.T) {
	now := time.Unix(1_000, 0)
	l := New().WithClock(func() time.Time { return now })
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("k", 3, time.Hour); !ok {
			t.Fatalf("event %d refused", i)
		}
		now = now.Add(time.Minute)
	}
	ok, retry := l.Allow("k", 3, time.Hour)
	if ok || retry != 57*time.Minute {
		t.Errorf("fourth: %v after %v", ok, retry)
	}
	// a refused event is not recorded: when the oldest leaves, one slot opens
	now = now.Add(57 * time.Minute)
	if ok, _ := l.Allow("k", 3, time.Hour); !ok {
		t.Error("slot did not open")
	}
	if ok, _ := l.Allow("other", 3, time.Hour); !ok {
		t.Error("keys are independent")
	}
}

func TestPeekDoesNotRecord(t *testing.T) {
	l := New()
	for i := 0; i < 10; i++ {
		if ok, _ := l.Peek("k", 1, time.Hour); !ok {
			t.Fatal("peek recorded")
		}
	}
	l.Allow("k", 1, time.Hour)
	if ok, _ := l.Peek("k", 1, time.Hour); ok {
		t.Error("peek missed the event")
	}
	l.Reset("k")
	if ok, _ := l.Peek("k", 1, time.Hour); !ok {
		t.Error("reset")
	}
}

func TestSweep(t *testing.T) {
	now := time.Unix(1_000, 0)
	l := New().WithClock(func() time.Time { return now })
	for i := 0; i < 100; i++ {
		l.Allow(string(rune('a'+i%26))+string(rune('a'+i/26)), 5, time.Minute)
	}
	now = now.Add(time.Hour)
	for i := 0; i < 600; i++ {
		l.Allow("live", 1000, time.Hour)
	}
	if n := l.Size(); n != 1 {
		t.Errorf("%d buckets survived the sweep", n)
	}
}

func TestIPKey(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.9":          "203.0.113.9",
		"2001:db8:1:2:3:4:5:6": "2001:db8:1:2::/64",
		"2001:db8:1:2:ffff::1": "2001:db8:1:2::/64",
		"::ffff:203.0.113.9":   "203.0.113.9",
		"not-an-ip":            "not-an-ip",
	} {
		if got := IPKey(in); got != want {
			t.Errorf("IPKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRetrySeconds(t *testing.T) {
	for d, want := range map[time.Duration]int{0: 1, time.Millisecond: 1, time.Second: 1, 1500 * time.Millisecond: 2, time.Minute: 60} {
		if got := RetrySeconds(d); got != want {
			t.Errorf("%v → %d, want %d", d, got, want)
		}
	}
}
