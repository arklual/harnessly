package usage

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/pricing"
)

func TestUnknownModelHasNoPrice(t *testing.T) {
	b := NewBook(SeedRows())
	tok := Tokens{Input: 1000, Output: 1000}
	for _, c := range []struct{ provider, model string }{
		// The audit's three: a family the table lacks must not fall back to another family's price, nor cost 0.
		{ProviderAnthropic, "claude-sonnet-9-9"},
		{ProviderAnthropic, "claude-haiku-9-9"},
		{ProviderOpenAI, "gpt-9.9-sol"},
		// Another vendor's model nobody lists.
		{ProviderKimi, "k9-unknown"},
		{ProviderAnthropic, "k9-unknown"},
		{ProviderAnthropic, ""},
	} {
		if m, _, _, ok := b.Cost(c.provider, c.model, 1_761_900_000_000, tok); ok {
			t.Errorf("%s %s: priced %d, want no price", c.provider, c.model, m)
		}
	}
}

func TestCostMatchesTheRuntime(t *testing.T) {
	b := NewBook(SeedRows())
	tok := Tokens{Input: 1200, Output: 3400, CacheRead: 250_000, CacheWrite5m: 9000, CacheWrite1h: 2000}
	m, ver, unit, ok := b.Cost(ProviderAnthropic, "claude-opus-4-6", 1_761_900_000_000, tok)
	if !ok || ver != 0 || unit != "usd" {
		t.Fatalf("%d %d %q %v", m, ver, unit, ok)
	}
	want, _ := pricing.ClaudeCost("claude-opus-4-6", 1200, 3400, 9000, 2000, 250_000)
	if math.Abs(float64(m)-want*1e6) > 1 {
		t.Errorf("%d micro-dollars, the runtime says %.6f $", m, want)
	}
	m, _, _, ok = b.Cost(ProviderOpenAI, "gpt-5.4-2026-03-05", 0, Tokens{Input: 1000, CacheRead: 9000, Output: 500})
	wantOAI, _ := pricing.OpenAICost("gpt-5.4-2026-03-05", 10_000, 9000, 500)
	if !ok || math.Abs(float64(m)-wantOAI*1e6) > 1 {
		t.Errorf("openai %d, want %.6f $", m, wantOAI)
	}
}

// Another vendor's model that ran through Claude Code or Codex is priced by its maker's row (HRN-663).
func TestVendorModelThroughAnotherCLI(t *testing.T) {
	b := NewBook(SeedRows())
	for _, c := range []struct {
		provider, model string
		tok             Tokens
		want            int64
	}{
		{ProviderAnthropic, "k3.1-pro", Tokens{Input: 1_000_000, CacheRead: 1_000_000, Output: 1_000_000}, 12_200_000},
		{ProviderKimi, "k3.1-pro", Tokens{Input: 1_000_000}, 2_000_000},
		{ProviderAnthropic, "glm-5.3", Tokens{Input: 1_000_000, Output: 1_000_000}, 5_800_000},
		{ProviderOpenAI, "grok-build-0.1", Tokens{Input: 1_000_000, Output: 1_000_000}, 3_000_000},
		// Past 200K a Grok 4.7 prompt costs twice.
		{ProviderOpenAI, "grok-4.7", Tokens{Input: 300_000}, 1_200_000},
		{ProviderOpenAI, "gpt-6.1-sol", Tokens{Input: 1_000_000, Output: 1_000_000}, 12_000_000},
	} {
		m, _, _, ok := b.Cost(c.provider, c.model, 1_761_900_000_000, c.tok)
		if !ok || m != c.want {
			t.Errorf("%s %s: %d %v, want %d", c.provider, c.model, m, ok, c.want)
		}
	}
}

// A call before a price change is priced by the old row, a call after by the new.
func TestPriceByEffectiveFrom(t *testing.T) {
	change := int64(1_760_000_000_000)
	b := NewBook([]store.Price{
		{Provider: ProviderKimi, ModelPattern: "kimi-k2", EffectiveFrom: 0, Input: 0.6, Output: 2.5, CacheRead: 0.15, Unit: "usd"},
		{Provider: ProviderKimi, ModelPattern: "kimi-k2", EffectiveFrom: change, Input: 1.2, Output: 5, CacheRead: 0.3, Unit: "usd"},
	})
	tok := Tokens{Input: 1_000_000}
	before, v1, _, _ := b.Cost(ProviderKimi, "Kimi-K2", change-1, tok)
	after, v2, _, _ := b.Cost(ProviderKimi, "kimi-k2", change, tok)
	if before != 600_000 || after != 1_200_000 || v1 != 0 || v2 != change {
		t.Errorf("before %d (v%d), after %d (v%d)", before, v1, after, v2)
	}
	if _, _, _, ok := NewBook([]store.Price{{Provider: ProviderKimi, ModelPattern: "kimi-k2", EffectiveFrom: change}}).Cost(ProviderKimi, "kimi-k2", change-1, tok); ok {
		t.Error("a call before the first row has no price")
	}
}

func TestPriceProvider(t *testing.T) {
	for _, c := range []struct{ cli, host, want string }{
		{"claude", "", ProviderAnthropic},
		{"claude", "api.anthropic.com", ProviderAnthropic},
		{"claude", "api.kimi.com", ProviderKimi},
		{"claude", "API.Z.AI", ProviderZai},
		{"codex", "", ProviderOpenAI},
		{"claude", "llm.example.net", "llm.example.net"},
	} {
		if got := PriceProvider(c.cli, c.host); got != c.want {
			t.Errorf("%s via %q: %s, want %s", c.cli, c.host, got, c.want)
		}
	}
}

func TestSeedKeepsCorrections(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.S().AddPrice(ctx, store.Price{Provider: ProviderAnthropic, ModelPattern: "opus-4-6", EffectiveFrom: 0, Input: 99, Unit: "usd"}); err != nil {
		t.Fatal(err)
	}
	n, err := Seed(ctx, db.S())
	if err != nil || n != len(SeedRows())-1 {
		t.Fatalf("seeded %d of %d, %v", n, len(SeedRows()), err)
	}
	if again, _ := Seed(ctx, db.S()); again != 0 {
		t.Errorf("second seed added %d", again)
	}
	ps, _ := db.S().Prices(ctx)
	b := NewBook(ps)
	if p, _ := b.Price(ProviderAnthropic, "claude-opus-4-6", 1); p.Input != 99 {
		t.Errorf("the correction was overwritten: %+v", p)
	}
}

// A call that came before its model had a price is priced once the list has one, and its hour is summed anew.
func TestPriceUnpricedCalls(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := db.S()
	ts := int64(1_761_900_000_000)
	for i, m := range []string{"vendor-x-1", "vendor-y-1"} {
		if _, err := s.AddCall(ctx, store.UsageCall{OrgKey: "o", Provider: "claude", MsgKey: m, SessionKey: "s", Ts: ts + int64(i),
			Model: m, PriceProvider: ProviderKimi, Input: 1_000_000, ReceivedAt: ts}); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := PriceUnpriced(ctx, s); err != nil || n != 0 {
		t.Fatalf("priced %d before the list had a row, %v", n, err)
	}
	if _, err := s.AddPrice(ctx, store.Price{Provider: ProviderKimi, ModelPattern: "vendor-x-1", Input: 2, Unit: "usd"}); err != nil {
		t.Fatal(err)
	}
	if n, err := PriceUnpriced(ctx, s); err != nil || n != 1 {
		t.Fatalf("priced %d, %v", n, err)
	}
	tot, err := s.SumCalls(ctx, "o", ts-1, ts+10, "")
	if err != nil || tot.Unpriced != 1 || tot.ListCostMicros != 2_000_000 {
		t.Fatalf("%+v %v", tot, err)
	}
	rows, err := s.RollupRows(ctx, "o", store.HourMs(ts), store.HourMs(ts)+3600000)
	var list int64
	for _, r := range rows {
		list += r.ListCostMicros
	}
	if err != nil || list != 2_000_000 {
		t.Errorf("rollup %d micro-dollars, %v", list, err)
	}
	if n, _ := PriceUnpriced(ctx, s); n != 0 {
		t.Errorf("second run priced %d", n)
	}
}
