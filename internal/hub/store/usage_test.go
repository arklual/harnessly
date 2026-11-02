package store

import (
	"context"
	"testing"
)

func TestUsageSQLite(t *testing.T)   { exerciseUsage(t, openTest(t)) }
func TestUsagePostgres(t *testing.T) { exerciseUsage(t, openPostgres(t)) }

func f(v float64) *float64 { return &v }
func i64(v int64) *int64   { return &v }

func exerciseUsage(t *testing.T, db *DB) {
	ctx := context.Background()
	s := db.S()
	u := &User{Email: "u@example.com"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	sp := &Space{Slug: "sp", Name: "Sp", CreatedBy: u.ID}
	if err := s.CreateSpace(ctx, sp); err != nil {
		t.Fatal(err)
	}
	key := OrgKey(*sp)
	if key != "space:"+sp.ID {
		t.Fatalf("org key %s", key)
	}
	m := &Machine{SpaceID: sp.ID, Profile: "dir", Via: "command", KeyPub: "k", EncPub: "e"}
	if err := s.CreateMachine(ctx, m); err != nil {
		t.Fatal(err)
	}

	// A call is one call however often it is seen.
	call := UsageCall{OrgKey: key, Provider: "claude", MsgKey: "msg_1", SessionKey: "s1", MachineID: m.ID, Ts: 1000, Model: "claude-opus-4-6",
		Input: 10, Output: 20, CacheRead: 30, CacheWrite5m: 4, CacheWrite1h: 5, ListCostMicros: i64(1234), PriceVer: i64(0), ReceivedAt: 2000}
	for i, want := range []bool{true, false} {
		if added, err := s.AddCall(ctx, call); err != nil || added != want {
			t.Fatalf("add %d: %v %v", i, added, err)
		}
	}
	unpriced := call
	unpriced.MsgKey, unpriced.ListCostMicros, unpriced.PriceVer = "msg_2", nil, nil
	if _, err := s.AddCall(ctx, unpriced); err != nil {
		t.Fatal(err)
	}
	tot, err := s.SumCalls(ctx, key, 0, 5000, "")
	if err != nil || tot.Calls != 2 || tot.Unpriced != 1 || tot.ListCostMicros != 1234 || tot.Input != 20 || tot.CacheWrite != 18 {
		t.Fatalf("totals %+v %v", tot, err)
	}

	// Sessions: a later seq of the same machine replaces; an older one does not.
	sess := AgentSession{OrgKey: key, Provider: "claude", SessionKey: "s1", MachineID: m.ID, Funding: FundingPersonal, Origin: "live",
		Attribution: "guessed", StartedAt: 1000, Seq: 5, UpdatedAt: 2000}
	if ok, err := s.PutSession(ctx, sess); err != nil || !ok {
		t.Fatalf("put %v %v", ok, err)
	}
	older := sess
	older.Seq, older.Attribution = 3, "exact"
	if ok, err := s.PutSession(ctx, older); err != nil || ok {
		t.Fatalf("older seq replaced: %v %v", ok, err)
	}
	newer := sess
	newer.Seq, newer.EndedAt, newer.Metrics = 9, i64(3000), `{"turns":3}`
	if ok, err := s.PutSession(ctx, newer); err != nil || !ok {
		t.Fatalf("newer %v %v", ok, err)
	}
	got, err := s.SessionByKey(ctx, key, "claude", "s1")
	if err != nil || got.Seq != 9 || got.EndedAt == nil || *got.EndedAt != 3000 || got.Metrics != `{"turns":3}` || got.Attribution != "guessed" {
		t.Fatalf("session %+v %v", got, err)
	}

	acc := &Account{OrgKey: key, ScopeKind: ScopeTenant, ScopeID: sp.ID, Provider: "anthropic", Kind: "subscription", AccountRef: "claude:abc", Discovered: true}
	if err := s.CreateAccount(ctx, acc); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateAccount(ctx, &Account{OrgKey: key, ScopeKind: ScopeTenant, ScopeID: sp.ID, Provider: "anthropic", Kind: "subscription", AccountRef: "claude:abc"}); !IsUnique(err) {
		t.Fatalf("the same account twice: %v", err)
	}
	if a, err := s.AccountByRef(ctx, key, "anthropic", "claude:abc"); err != nil || a.ID != acc.ID || a.Funding != FundingPersonal || !a.Discovered {
		t.Fatalf("account %+v %v", a, err)
	}

	// Windows: the freshest measurement with a value wins; an empty one after a reset keeps the window.
	w := AccountWindow{AccountID: acc.ID, Scope: "all", Name: "five_hour", LengthSec: 18000, Unit: "pct", UsedPct: f(40), ResetsAt: i64(9000), MeasuredAt: 5000, Source: "oauth-usage", Confidence: "undocumented"}
	for _, step := range []struct {
		w    AccountWindow
		want float64
	}{
		{w, 40},
		{func() AccountWindow { x := w; x.UsedPct, x.MeasuredAt = f(55), 6000; return x }(), 55},
		{func() AccountWindow { x := w; x.UsedPct, x.MeasuredAt = f(10), 5500; return x }(), 55}, // older
		{func() AccountWindow { x := w; x.UsedPct, x.MeasuredAt = nil, 9500; return x }(), 55},   // empty after the reset
	} {
		if _, err := s.PutWindow(ctx, step.w); err != nil {
			t.Fatal(err)
		}
		ws, err := s.Windows(ctx, acc.ID)
		if err != nil || len(ws) != 1 || ws[0].UsedPct == nil || *ws[0].UsedPct != step.want {
			t.Fatalf("windows %+v %v; want %v", ws, err, step.want)
		}
	}

	// Balances: a failed poll keeps the last good value beside its error.
	if err := s.PutBalance(ctx, AccountBalance{AccountID: acc.ID, Currency: "USD", Available: f(41.5), MeasuredAt: 100, Source: "openrouter-key"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutBalance(ctx, AccountBalance{AccountID: acc.ID, Currency: "USD", Error: "timeout"}); err != nil {
		t.Fatal(err)
	}
	bs, err := s.Balances(ctx, acc.ID)
	if err != nil || len(bs) != 1 || bs[0].Available == nil || *bs[0].Available != 41.5 || bs[0].Error != "timeout" {
		t.Fatalf("balances %+v %v", bs, err)
	}

	if _, err := s.GrantAccount(ctx, AccountGrant{AccountID: acc.ID, ScopeKind: ScopeTenant, ScopeID: "x"}); err != nil {
		t.Fatal(err)
	}
	if gs, err := s.AccountGrants(ctx, acc.ID); err != nil || len(gs) != 1 {
		t.Fatalf("grants %+v %v", gs, err)
	}

	c, err := s.CursorOf(ctx, m.ID, "usage")
	if err != nil || c.Seq != 0 || c.Epoch != "" {
		t.Fatalf("fresh cursor %+v %v", c, err)
	}
	c.Epoch, c.Seq, c.AckedAt = "e1", 44, i64(7000)
	if err := s.SetCursor(ctx, c); err != nil {
		t.Fatal(err)
	}
	if c2, err := s.CursorOf(ctx, m.ID, "usage"); err != nil || c2.Seq != 44 || c2.Epoch != "e1" || c2.AckedAt == nil {
		t.Fatalf("cursor %+v %v", c2, err)
	}
}
