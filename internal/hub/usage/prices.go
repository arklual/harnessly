// Package usage is the hub's side of spend (docs/hub/usage.md): the price list and what a call costs at list prices,
// and taking in the facts machines report.
package usage

import (
	"context"
	"math"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/pricing"
)

// Providers of the price list: who a call was paid to, not which CLI made it.
const (
	ProviderAnthropic  = "anthropic"
	ProviderOpenAI     = "openai"
	ProviderKimi       = "kimi"
	ProviderMoonshot   = "moonshot"
	ProviderZai        = "zai"
	ProviderOpenRouter = "openrouter"
	ProviderDeepSeek   = "deepseek"
	ProviderMiniMax    = "minimax"
)

// hostProviders maps the host a call went to onto its provider; a host not listed is its own provider (no price until
// someone adds rows for it).
var hostProviders = map[string]string{
	"api.anthropic.com": ProviderAnthropic,
	"api.openai.com":    ProviderOpenAI,
	"chatgpt.com":       ProviderOpenAI,
	"api.kimi.com":      ProviderKimi,
	"api.moonshot.ai":   ProviderMoonshot,
	"api.moonshot.cn":   ProviderMoonshot,
	"api.z.ai":          ProviderZai,
	"open.bigmodel.cn":  ProviderZai,
	"openrouter.ai":     ProviderOpenRouter,
	"api.deepseek.com":  ProviderDeepSeek,
	"api.minimax.io":    ProviderMiniMax,
	"api.minimaxi.com":  ProviderMiniMax,
}

// PriceProvider is the provider a call is priced by: the host it went to, else the CLI's own vendor.
func PriceProvider(cli, baseURLHost string) string {
	h := strings.ToLower(strings.TrimSpace(baseURLHost))
	if h != "" {
		if p, ok := hostProviders[h]; ok {
			return p
		}
		return h
	}
	if cli == "codex" {
		return ProviderOpenAI
	}
	return ProviderAnthropic
}

// PriceKey is the price list's key of a model at a provider; "" when the model names none. A Claude model whose family
// key is not in the list gets no price here, never a neighbour family's.
func PriceKey(provider, model string) string {
	switch provider {
	case ProviderAnthropic:
		return pricing.ClaudeKey(model)
	case ProviderOpenAI:
		k, _ := pricing.OpenAIKey(model)
		return k
	default:
		return strings.ToLower(strings.TrimSpace(model))
	}
}

// Tokens of one call. Input does not include the tokens read from the cache.
type Tokens struct {
	Input, Output, CacheRead, CacheWrite5m, CacheWrite1h int64
}

// Book is the price list in memory, ready to price calls.
type Book struct {
	rows map[string][]store.Price // provider + "\x00" + pattern → rows by effective_from
}

func NewBook(prices []store.Price) *Book {
	b := &Book{rows: map[string][]store.Price{}}
	for _, p := range prices {
		k := p.Provider + "\x00" + p.ModelPattern
		b.rows[k] = append(b.rows[k], p)
	}
	for _, rs := range b.rows {
		sort.Slice(rs, func(i, j int) bool { return rs[i].EffectiveFrom < rs[j].EffectiveFrom })
	}
	return b
}

// Price is the row in effect at ts (epoch ms) for a model at a provider; false when there is none.
func (b *Book) Price(provider, model string, ts int64) (store.Price, bool) {
	key := PriceKey(provider, model)
	if key == "" {
		return store.Price{}, false
	}
	rs := b.rows[provider+"\x00"+key]
	i := sort.Search(len(rs), func(i int) bool { return rs[i].EffectiveFrom > ts }) - 1
	if i < 0 {
		return store.Price{}, false
	}
	return rs[i], true
}

// Cost is a call's list price in micro-units of the row's unit (micro-dollars for usd), and the row's version (its
// effective_from); false when the model has no price: the caller keeps NULL, never 0.
func (b *Book) Cost(provider, model string, ts int64, t Tokens) (micros int64, ver int64, unit string, ok bool) {
	p, ok := b.Price(provider, model, ts)
	if !ok {
		return 0, 0, "", false
	}
	factor := 1.0
	if p.LongCtxThreshold != nil && p.LongCtxFactor != nil && t.Input+t.CacheRead+t.CacheWrite5m+t.CacheWrite1h > *p.LongCtxThreshold {
		factor = *p.LongCtxFactor
	}
	// Tokens × USD per million tokens is micro-dollars.
	v := float64(t.Input)*p.Input + float64(t.Output)*p.Output + float64(t.CacheRead)*p.CacheRead +
		float64(t.CacheWrite5m)*p.CacheWrite5m + float64(t.CacheWrite1h)*p.CacheWrite1h
	return int64(math.Round(v * factor)), p.EffectiveFrom, p.Unit, true
}

// SeedRows are the rows the hub puts in an empty price list: internal/pricing's tables (the runtime's own numbers, so
// the hub and /api/platform/stats agree), in effect from the beginning. Rows of other providers are added the same way
// with their own effective_from when their prices are confirmed; a model without a row has no price.
func SeedRows() []store.Price {
	var out []store.Price
	for _, r := range pricing.Rows() {
		out = append(out, store.Price{
			Provider: r.Provider, ModelPattern: r.Key, EffectiveFrom: 0,
			Input: r.Input, Output: r.Output, CacheWrite5m: r.CacheWrite5m, CacheWrite1h: r.CacheWrite1h, CacheRead: r.CacheRead,
			Unit: "usd", Source: "internal/pricing",
		})
	}
	return out
}

// Seed adds the seed rows that are missing; rows already there (an administrator's correction) are kept. It returns how
// many it added.
func Seed(ctx context.Context, s store.Store) (int, error) {
	n := 0
	for _, p := range SeedRows() {
		added, err := s.AddPrice(ctx, p)
		if err != nil {
			return n, err
		}
		if added {
			n++
		}
	}
	return n, nil
}

// PriceUnpriced prices the stored calls that had no price when they came (a model the list did not know yet) by the list
// as it is now, and sums their hours anew. It returns how many it priced; calls whose model the list still lacks stay.
func PriceUnpriced(ctx context.Context, s store.Store) (int, error) {
	prices, err := s.Prices(ctx)
	if err != nil {
		return 0, err
	}
	b := NewBook(prices)
	hours := map[string]map[int64]bool{}
	n := 0
	var after [3]string
	for {
		calls, err := s.UnpricedCalls(ctx, after, 500)
		if err != nil {
			return n, err
		}
		for _, c := range calls {
			after = [3]string{c.OrgKey, c.Provider, c.MsgKey}
			cost, ver, unit, ok := b.Cost(c.PriceProvider, c.Model, c.Ts,
				Tokens{Input: c.Input, Output: c.Output, CacheRead: c.CacheRead, CacheWrite5m: c.CacheWrite5m, CacheWrite1h: c.CacheWrite1h})
			if !ok || unit != "usd" {
				continue
			}
			done, err := s.PriceCall(ctx, c.OrgKey, c.Provider, c.MsgKey, cost, ver)
			if err != nil {
				return n, err
			}
			if done {
				n++
				if hours[c.OrgKey] == nil {
					hours[c.OrgKey] = map[int64]bool{}
				}
				hours[c.OrgKey][store.HourMs(c.Ts)] = true
			}
		}
		if len(calls) < 500 {
			break
		}
	}
	for org, hs := range hours {
		list := make([]int64, 0, len(hs))
		for h := range hs {
			list = append(list, h)
		}
		if err := s.Rollup(ctx, org, list); err != nil {
			return n, err
		}
	}
	return n, nil
}
