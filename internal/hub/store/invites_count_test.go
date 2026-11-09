package store

import (
	"context"
	"testing"
	"time"
)

// The tenant's deletion plan counts exactly the invites the invitations screen lists (GitHub issue 36): a used-up
// email or link invite, an expired one and a revoked one keep nothing; an open one does.
func TestCountActiveInvitesMatchesActiveInvites(t *testing.T) {
	for name, open := range map[string]func(*testing.T) *DB{"sqlite": openTest, "postgres": openPostgres} {
		t.Run(name, func(t *testing.T) {
			s := open(t).S()
			ctx := context.Background()
			now := time.Now()
			u := &User{Email: "owner@example.com"}
			if err := s.CreateUser(ctx, u); err != nil {
				t.Fatal(err)
			}
			sp := &Space{Slug: "acme", Name: "Acme", CreatedBy: u.ID}
			if err := s.CreateSpace(ctx, sp); err != nil {
				t.Fatal(err)
			}
			one := 1
			invite := func(kind, hash string, max *int, expires time.Time) *Invite {
				iv := &Invite{SpaceID: sp.ID, Kind: kind, TokenHash: hash, Role: RoleMember, MaxUses: max, ExpiresAt: expires, CreatedBy: u.ID}
				if kind == "email" {
					iv.Email = hash + "@example.com"
				}
				if err := s.CreateInvite(ctx, iv); err != nil {
					t.Fatal(err)
				}
				return iv
			}
			count := func() int {
				n, err := s.CountActiveInvitesOfSpace(ctx, sp.ID, Ms(now))
				if err != nil {
					t.Fatal(err)
				}
				list, err := s.ActiveInvites(ctx, sp.ID, now)
				if err != nil {
					t.Fatal(err)
				}
				if n != len(list) {
					t.Fatalf("the plan counts %d, the screen lists %d", n, len(list))
				}
				return n
			}
			use := func(iv *Invite, who string) {
				guest := &User{Email: who}
				if err := s.CreateUser(ctx, guest); err != nil {
					t.Fatal(err)
				}
				if ok, err := s.UseInvite(ctx, iv.ID, guest.ID, now); err != nil || !ok {
					t.Fatalf("use: %v %v", ok, err)
				}
			}

			use(invite("email", "used-email", &one, now.Add(time.Hour)), "used-email@example.com")
			use(invite("link", "used-link", &one, now.Add(time.Hour)), "used-link@example.com")
			invite("link", "expired", nil, now.Add(-time.Minute))
			revoked := invite("link", "revoked", nil, now.Add(time.Hour))
			if ok, err := s.RevokeInvite(ctx, sp.ID, revoked.ID, now); err != nil || !ok {
				t.Fatalf("revoke: %v %v", ok, err)
			}
			if n := count(); n != 0 {
				t.Fatalf("used, expired and revoked invites count %d", n)
			}
			invite("link", "open-link", &one, now.Add(time.Hour))
			invite("email", "open-email", &one, now.Add(time.Hour))
			if n := count(); n != 2 {
				t.Fatalf("open invites count %d, want 2", n)
			}
		})
	}
}
