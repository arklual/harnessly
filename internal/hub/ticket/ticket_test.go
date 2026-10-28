package ticket

import (
	"testing"
	"time"
)

func TestTicketIsSingleUseAndBoundToAMachine(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	s := New(func() time.Time { return now })
	tk := s.Issue("u1", "m1")
	if tk == "" || len(tk) < 20 {
		t.Fatalf("ticket %q", tk)
	}
	if _, ok := s.Redeem(tk, "m2"); ok {
		t.Error("redeemed for another machine")
	}
	// A failed try spends the ticket: a guess at the machine cannot be repeated.
	if _, ok := s.Redeem(tk, "m1"); ok {
		t.Error("redeemed after a wrong try")
	}
	tk = s.Issue("u1", "m1")
	if u, ok := s.Redeem(tk, "m1"); !ok || u != "u1" {
		t.Errorf("redeem: %q %v", u, ok)
	}
	if _, ok := s.Redeem(tk, "m1"); ok {
		t.Error("redeemed twice")
	}
}

func TestTicketExpires(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	s := New(func() time.Time { return now })
	tk := s.Issue("u1", "m1")
	now = now.Add(TTL + time.Second)
	if _, ok := s.Redeem(tk, "m1"); ok {
		t.Error("an old ticket was accepted")
	}
	s.Issue("u", "m")
	now = now.Add(time.Hour)
	s.Sweep()
	if s.Len() != 0 {
		t.Errorf("%d tickets left", s.Len())
	}
	if _, ok := s.Redeem("nonsense", "m1"); ok {
		t.Error("nonsense accepted")
	}
}
