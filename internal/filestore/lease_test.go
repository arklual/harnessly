package filestore

import (
	"context"
	"testing"
)

func leaseRoom(t *testing.T, r *rig) RoomMeta {
	t.Helper()
	rm, err := r.meta.Room(context.Background(), DefaultRoom)
	if err != nil {
		t.Fatal(err)
	}
	return rm
}

func TestLeaseFirstEditTakesItAndTheOtherIsRefusedWithTheHolder(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		rm, ctx := leaseRoom(t, r), context.Background()
		const t0 = int64(1_000_000)
		got, held, err := rm.LeaseTake(ctx, LeaseRequest{Key: "p/Оплата", Chat: "c1", User: "Анна", ChatTitle: "Оплата", Edit: true}, "vm-a", t0)
		if err != nil || held {
			t.Fatalf("first take: held=%v err=%v", held, err)
		}
		if got.Machine != "vm-a" || got.Since != t0 || got.Edit != t0 {
			t.Fatalf("lease = %+v", got)
		}
		other, held, err := rm.LeaseTake(ctx, LeaseRequest{Key: "p/Оплата", Chat: "c9", User: "Борис"}, "vm-b", t0+5_000)
		if err != nil || !held {
			t.Fatalf("second chat must be refused: held=%v err=%v", held, err)
		}
		if other.Machine != "vm-a" || other.Chat != "c1" || other.User != "Анна" || other.ChatTitle != "Оплата" {
			t.Fatalf("refusal must name the holder: %+v", other)
		}
		// Another chat of the same machine is a different holder too.
		if _, held, _ = rm.LeaseTake(ctx, LeaseRequest{Key: "p/Оплата", Chat: "c2"}, "vm-a", t0+6_000); !held {
			t.Fatal("same machine, other chat: must be refused")
		}
	})
}

func TestLeaseRefreshKeepsSinceAndMovesEditOnlyOnEdit(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		rm, ctx := leaseRoom(t, r), context.Background()
		const t0 = int64(1_000_000)
		_, _, _ = rm.LeaseTake(ctx, LeaseRequest{Key: "k", Chat: "c1", Edit: true}, "m", t0)
		l, held, _ := rm.LeaseTake(ctx, LeaseRequest{Key: "k", Chat: "c1", Turn: "working"}, "m", t0+30_000)
		if held || l.Since != t0 || l.Beat != t0+30_000 || l.Edit != t0 || l.Turn != "working" {
			t.Fatalf("heartbeat: %+v held=%v", l, held)
		}
		l, _, _ = rm.LeaseTake(ctx, LeaseRequest{Key: "k", Chat: "c1", Edit: true}, "m", t0+60_000)
		if l.Edit != t0+60_000 || l.Since != t0 {
			t.Fatalf("edit: %+v", l)
		}
	})
}

func TestLeaseExpiresAfterTwoMinutesOfSilence(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		rm, ctx := leaseRoom(t, r), context.Background()
		const t0 = int64(1_000_000)
		_, _, _ = rm.LeaseTake(ctx, LeaseRequest{Key: "k", Chat: "c1"}, "m1", t0)
		if _, held, _ := rm.LeaseTake(ctx, LeaseRequest{Key: "k", Chat: "c2"}, "m2", t0+LeaseTTLMs-1); !held {
			t.Fatal("still live just before the TTL")
		}
		l, held, err := rm.LeaseTake(ctx, LeaseRequest{Key: "k", Chat: "c2"}, "m2", t0+LeaseTTLMs)
		if err != nil || held || l.Machine != "m2" || l.Since != t0+LeaseTTLMs {
			t.Fatalf("after the TTL the board is free: %+v held=%v err=%v", l, held, err)
		}
	})
}

func TestLeaseReleaseByOwnerAndForce(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		rm, ctx := leaseRoom(t, r), context.Background()
		const t0 = int64(1_000_000)
		_, _, _ = rm.LeaseTake(ctx, LeaseRequest{Key: "k", Chat: "c1"}, "m1", t0)
		if dropped, err := rm.LeaseDrop(ctx, LeaseRelease{Key: "k", Chat: "c2"}, "m2", t0+1); err != nil || dropped != nil {
			t.Fatalf("a stranger cannot release: %+v %v", dropped, err)
		}
		dropped, err := rm.LeaseDrop(ctx, LeaseRelease{Key: "k", Force: true}, "m1", t0+2)
		if err != nil || dropped == nil || dropped.Chat != "c1" {
			t.Fatalf("force must drop and name the old holder: %+v %v", dropped, err)
		}
		if ls, _ := rm.Leases(ctx, t0+3); len(ls) != 0 {
			t.Fatalf("leases = %+v", ls)
		}
		_, _, _ = rm.LeaseTake(ctx, LeaseRequest{Key: "k", Chat: "c3"}, "m3", t0+4)
		if dropped, _ := rm.LeaseDrop(ctx, LeaseRelease{Key: "k", Chat: "c3"}, "m3", t0+5); dropped == nil {
			t.Fatal("the owner releases its own lease")
		}
	})
}

func TestLeasesListsOnlyLiveOnes(t *testing.T) {
	eachCombo(t, func(t *testing.T, r *rig) {
		rm, ctx := leaseRoom(t, r), context.Background()
		const t0 = int64(1_000_000)
		_, _, _ = rm.LeaseTake(ctx, LeaseRequest{Key: "a", Chat: "c1"}, "m", t0)
		_, _, _ = rm.LeaseTake(ctx, LeaseRequest{Key: "b", Chat: "c1"}, "m", t0+100_000)
		ls, err := rm.Leases(ctx, t0+LeaseTTLMs+1)
		if err != nil || len(ls) != 1 || ls[0].Key != "b" {
			t.Fatalf("leases = %+v err=%v", ls, err)
		}
	})
}
