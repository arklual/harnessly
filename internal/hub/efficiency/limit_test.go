package efficiency

import (
	"context"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The person's limit is the fullest week of Claude among their machines that reported lately; none, or only stale
// readings, is no limit.
func TestMeLimitFromMachineWindows(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	u := &store.User{Email: "ann@example.com", Name: "Ann"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	org := &store.Org{Slug: "acme", Name: "Acme", CreatedBy: u.ID}
	if err := st.CreateOrg(ctx, org); err != nil {
		t.Fatal(err)
	}
	sp := &store.Space{Slug: "acme-team", Name: "Acme", CreatedBy: u.ID}
	if err := st.CreateSpace(ctx, sp); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSpaceOrg(ctx, sp.ID, org.ID); err != nil {
		t.Fatal(err)
	}
	m := &store.Machine{SpaceID: sp.ID, Profile: "dir", Via: "command", KeyPub: "k", EncPub: "e", CreatedBy: u.ID}
	if err := st.CreateMachine(ctx, m); err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1_700_000_000_000)
	if got := meLimit(ctx, st, org.ID, u.ID, now); got.WeekUsedPct != nil || got.ResetsAt != nil {
		t.Fatalf("no reading: %+v", got)
	}
	put := func(used float64, resets, at int64) {
		t.Helper()
		if err := st.PutProviderWindow(ctx, store.ProviderWindow{MachineID: m.ID, Provider: "claude", Window: "seven_day", UsedPct: used, ResetsAt: resets, SampledAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	put(42.5, 1_700_500_000, now.Add(-time.Minute).UnixMilli())
	got := meLimit(ctx, st, org.ID, u.ID, now)
	if got.WeekUsedPct == nil || *got.WeekUsedPct != 42.5 || got.ResetsAt == nil || *got.ResetsAt != "2023-11-20T17:06:40.000Z" {
		t.Fatalf("limit %+v", got)
	}
	put(42.5, 1_700_500_000, now.Add(-3*time.Hour).UnixMilli())
	if got := meLimit(ctx, st, org.ID, u.ID, now); got.WeekUsedPct != nil {
		t.Fatalf("stale reading: %+v", got)
	}
}
