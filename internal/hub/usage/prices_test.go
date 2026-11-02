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
		{ProviderAnthropic, "claude-sonnet-5-5"},
		{ProviderAnthropic, "claude-haiku-5-5"},
		{ProviderOpenAI, "gpt-5.6-sol"},
		// Another vendor's model through Claude Code, before its rows are added.
		{ProviderKimi, "k3.1-pro"},
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
