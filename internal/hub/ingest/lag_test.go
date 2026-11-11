package ingest

import (
	"errors"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
)

func TestLagIsTheLongestWaitOfSpendNotStoredYet(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := New(Deps{Now: func() time.Time { return now }})
	defer s.Close()
	if lag, n := s.Lag(); lag != 0 || n != 0 {
		t.Fatalf("lag of an idle hub = %v, %d", lag, n)
	}
	s.mu.Lock()
	s.waiting["old"], s.waiting["new"], s.waiting["gone"] = now.Add(-90*time.Second), now.Add(-5*time.Second), now.Add(-time.Hour)
	// A machine that went away is not the hub being behind; a failed pull is.
	s.pulled("gone", httpx.ErrMachineOffline)
	s.pulled("old", errors.New("database is down"))
	s.mu.Unlock()
	if lag, n := s.Lag(); lag != 90*time.Second || n != 2 {
		t.Fatalf("lag = %v of %d machines, want 1m30s of 2", lag, n)
	}
	s.mu.Lock()
	s.again["new"] = true
	s.pulled("old", nil) // emptied
	s.pulled("new", nil) // emptied, and told of more meanwhile: the wait starts anew
	s.mu.Unlock()
	if lag, n := s.Lag(); lag != 0 || n != 1 {
		t.Fatalf("lag = %v of %d machines, want 0 of 1", lag, n)
	}
}

// A failed pull is tried again after the link's pause, growing while it keeps failing, without a new usage.available;
// a machine that went away is not tried until it dials again.
func TestFailedPullIsTriedAgainAfterAPause(t *testing.T) {
	s := New(Deps{})
	defer s.Close()
	var waits []int
	s.retryWait = func(failures int) time.Duration {
		waits = append(waits, failures)
		return time.Hour
	}
	s.mu.Lock()
	s.pulled("m", errors.New("database is down"))
	s.pulled("m", errors.New("database is down")) // one retry armed at a time
	armed := s.retry["m"] != nil
	s.pulled("gone", httpx.ErrMachineOffline)
	_, goneArmed := s.retry["gone"]
	s.mu.Unlock()
	if !armed || goneArmed {
		t.Fatalf("retry armed %v, for a machine that went away %v", armed, goneArmed)
	}
	if len(waits) != 1 || waits[0] != 1 {
		t.Fatalf("pauses asked for %v, want one after the first failure", waits)
	}
	s.mu.Lock()
	s.retry["m"].Stop()
	delete(s.retry, "m")
	s.pulled("m", errors.New("still down"))
	s.pulled("m", nil)
	failures := s.failures["m"]
	s.mu.Unlock()
	if len(waits) != 2 || waits[1] != 3 {
		t.Fatalf("pauses asked for %v, want the third failure's pause next", waits)
	}
	if failures != 0 {
		t.Fatalf("failures after a stored pull = %d", failures)
	}
}
