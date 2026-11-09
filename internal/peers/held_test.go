package peers

import (
	"context"
	"errors"
	"testing"
	"time"
)

func heldService(t *testing.T) (*Service, *fakeLocal, *time.Time) {
	t.Helper()
	s, local := newService(t, peter, Chat{Conv: "tab-a", Title: "Релиз", State: ChatIdle})
	now := time.UnixMilli(1_800_000_000_000)
	s.Now = func() time.Time { return now }
	put(t, s, func(st *Settings) { st.Approve = ApproveOthers })
	return s, local, &now
}

func TestHeldUntilTheOwnerAccepts(t *testing.T) {
	s, local, _ := heldService(t)
	ctx := context.Background()
	r := s.Receive(ctx, ivan, Inbound{ID: "msg-001", To: "tab-a", Text: "собери релиз"})
	if r.State != StateHeld || len(local.delivered) != 0 {
		t.Fatalf("a message of another person waits: %+v %v", r, local.delivered)
	}
	held := s.HeldList()
	if len(held) != 1 || held[0].From.Person != ivan.Person || held[0].To.Title != "Релиз" {
		t.Fatalf("the inbox shows who, to whom and what: %+v", held)
	}
	// Sent again, it neither delivers nor waits twice.
	if again := s.Receive(ctx, ivan, Inbound{ID: "msg-001", To: "tab-a", Text: "собери релиз"}); again.State != StateHeld ||
		len(s.HeldList()) != 1 {
		t.Fatalf("a resend: %+v", again)
	}
	got, err := s.Decide(ctx, "msg-001", true, "Пётр")
	if err != nil || got.State != StateDelivered || len(local.delivered) != 1 || local.delivered[0].clientID != "peer-msg-001" {
		t.Fatalf("accepted, it is delivered as it came: %+v %v %v", got, err, local.delivered)
	}
	if after := s.Receive(ctx, ivan, Inbound{ID: "msg-001", To: "tab-a", Text: "собери релиз"}); after.State != StateDelivered ||
		len(local.delivered) != 1 {
		t.Fatalf("the sender learns the outcome, nothing runs twice: %+v", after)
	}
	if _, err := s.Decide(ctx, "msg-001", true, "Пётр"); !errors.Is(err, ErrNotHeld) {
		t.Fatalf("a message is decided on once: %v", err)
	}
	rec := s.Audit.Recent(1)[0]
	if rec.State != StateDelivered || rec.By != "Пётр" || rec.From.Person != ivan.Person {
		t.Fatalf("the audit names the approver and the requester: %+v", rec)
	}
}

func TestHeldOwnerOwnMessagesPass(t *testing.T) {
	s, local, _ := heldService(t)
	same := peter
	same.Machine = "laptop"
	if r := s.Receive(context.Background(), same, Inbound{ID: "msg-002", To: "tab-a", Text: "hi"}); r.State != StateDelivered ||
		len(local.delivered) != 1 {
		t.Fatalf("the owner's own machines do not wait: %+v", r)
	}
	put(t, s, func(st *Settings) { st.Approve = ApproveNone })
	if r := s.Receive(context.Background(), ivan, Inbound{ID: "msg-003", To: "tab-a", Text: "hi"}); r.State != StateDelivered {
		t.Fatalf("without approval nothing waits: %+v", r)
	}
}

func TestHeldRejectRevokeExpire(t *testing.T) {
	s, local, now := heldService(t)
	ctx := context.Background()
	s.Receive(ctx, ivan, Inbound{ID: "msg-010", To: "tab-a", Text: "a"})
	if r, _ := s.Decide(ctx, "msg-010", false, ""); r.State != StateRefused || r.Reason != RefuseRejected {
		t.Fatalf("rejected: %+v", r)
	}
	if r := s.Receive(ctx, ivan, Inbound{ID: "msg-010", To: "tab-a", Text: "a"}); r.Reason != RefuseRejected {
		t.Fatalf("the sender learns of the rejection: %+v", r)
	}

	s.Receive(ctx, ivan, Inbound{ID: "msg-011", To: "tab-a", Text: "b"})
	other := ivan
	other.Person = "mallory@example.com"
	if r := s.Receive(ctx, other, Inbound{ID: "msg-011", Revoke: true}); r.Reason == RefuseRevoked || len(s.HeldList()) != 1 {
		t.Fatalf("only its sender takes a message back: %+v", r)
	}
	if r := s.Receive(ctx, ivan, Inbound{ID: "msg-011", Revoke: true}); r.Reason != RefuseRevoked || len(s.HeldList()) != 0 {
		t.Fatalf("taken back: %+v", r)
	}
	if _, err := s.Decide(ctx, "msg-011", true, ""); !errors.Is(err, ErrNotHeld) {
		t.Fatalf("a message taken back cannot be accepted: %v", err)
	}

	s.Receive(ctx, ivan, Inbound{ID: "msg-012", To: "tab-a", Text: "c", TTL: 60})
	*now = now.Add(2 * time.Minute)
	if len(s.HeldList()) != 0 {
		t.Fatal("a message past its time leaves the inbox")
	}
	if r := s.Receive(ctx, ivan, Inbound{ID: "msg-012", To: "tab-a", Text: "c"}); r.Reason != RefuseExpired || len(local.delivered) != 0 {
		t.Fatalf("expired: %+v", r)
	}
	if heldTTL(0) != HeldTTL || heldTTL(int64(30*24*time.Hour/time.Second)) != MaxHeldTTL {
		t.Fatal("the time a message may wait is capped")
	}
}

func TestSettingsApproveValidated(t *testing.T) {
	st := Defaults()
	st.Approve = "everyone"
	if st.Validate() == nil {
		t.Fatal("approve is none or others")
	}
	if Defaults().Approve != ApproveNone {
		t.Fatal("nothing waits by default")
	}
}
