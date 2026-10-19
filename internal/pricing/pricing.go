// Package pricing is the API price of tokens, per model, for Claude and OpenAI models: what a conversation would have
// cost at list prices. The tables and the way a model id finds its row are the previous runtime's, so a cost computed
// here is the number it showed.
package pricing

import (
	"regexp"
	"sort"
	"strings"
	"sync"

	"harnessly/internal/jscompat"
)

// Claude prices, USD per million tokens; cache writes are priced by their lifetime.
type claudePrice struct{ input, output, cacheWrite5m, cacheWrite1h, cacheRead float64 }

var claudePrices = map[string]claudePrice{
	"fable-5-1":  {10, 50, 12.5, 20, 0.25},
	"fable-5":    {10, 50, 12.5, 20, 1},
	"opus-5-5":   {4, 20, 5, 8, 0.2},
	"opus-5":     {5, 25, 6.25, 10, 0.5},
	"sonnet-5":   {2, 10, 2.5, 4, 0.2},
	"opus-4-8":   {5, 25, 6.25, 10, 0.5},
	"opus-4-7":   {5, 25, 6.25, 10, 0.5},
	"opus-4-6":   {5, 25, 6.25, 10, 0.5},
	"opus-4-5":   {5, 25, 6.25, 10, 0.5},
	"opus-4-1":   {15, 75, 18.75, 30, 1.5},
	"opus-4":     {15, 75, 18.75, 30, 1.5},
	"sonnet-4-6": {3, 15, 3.75, 6, 0.3},
	"sonnet-4-5": {3, 15, 3.75, 6, 0.3},
	"sonnet-4":   {3, 15, 3.75, 6, 0.3},
	"sonnet-3-7": {3, 15, 3.75, 6, 0.3},
	"sonnet-3-5": {3, 15, 3.75, 6, 0.3},
	"haiku-4-5":  {1, 5, 1.25, 2, 0.1},
	"haiku-3-5":  {0.8, 4, 1, 1.6, 0.08},
	"haiku-3":    {0.25, 1.25, 0.3, 0.5, 0.03},
	"opus-3":     {15, 75, 18.75, 30, 1.5},
}

var (
	bracketSuffix = regexp.MustCompile(`\[.*?\]$`)
	// Version parts are one or two digits, so a snapshot date (claude-haiku-4-5-20251001) stays out of the key.
	claudeModelKey = regexp.MustCompile(`claude-(\w+-\d{1,2}(?:-\d{1,2})*)(?:-\d{8}|-v\d)?(?::\d)?$`)
)

var (
	keysMu sync.Mutex
	keys   = map[string]string{}
)

// ClaudeKey is the price table key of a Claude model id ("opus-4-6"), "" when the id names none. Keys are remembered:
// a transcript names a handful of models a thousand times.
func ClaudeKey(model string) string {
	keysMu.Lock()
	k, ok := keys[model]
	keysMu.Unlock()
	if ok {
		return k
	}
	if m := claudeModelKey.FindStringSubmatch(bracketSuffix.ReplaceAllLiteralString(model, "")); m != nil {
		k = m[1]
	}
	keysMu.Lock()
	if len(keys) < 1024 {
		keys[model] = k
	}
	keysMu.Unlock()
	return k
}

// ClaudeCost is the list price of a Claude model's tokens; false for a model the table does not know.
func ClaudeCost(model string, input, output, cacheWrite5m, cacheWrite1h, cacheRead float64) (float64, bool) {
	p, ok := claudePrices[ClaudeKey(model)]
	if !ok {
		return 0, false
	}
	return claudeCost(p, input, output, cacheWrite5m, cacheWrite1h, cacheRead), true
}

// ClaudeCostByFullID is ClaudeCost, with a model the table does not know priced by the family its id mentions (Sonnet
// when none).
func ClaudeCostByFullID(model string, input, output, cacheWrite5m, cacheWrite1h, cacheRead float64) float64 {
	if c, ok := ClaudeCost(model, input, output, cacheWrite5m, cacheWrite1h, cacheRead); ok {
		return c
	}
	lower := strings.ToLower(model)
	p := claudePrices["sonnet-4-6"]
	if strings.Contains(lower, "opus") {
		p = claudePrices["opus-4-6"]
	} else if strings.Contains(lower, "haiku") {
		p = claudePrices["haiku-4-5"]
	}
	return claudeCost(p, input, output, cacheWrite5m, cacheWrite1h, cacheRead)
}

func claudeCost(p claudePrice, input, output, cacheWrite5m, cacheWrite1h, cacheRead float64) float64 {
	const mtok = 1_000_000
	// Each product is rounded on its own (no fused multiply-add), as JavaScript computes it.
	return float64(input/mtok*p.input) + float64(cacheWrite5m/mtok*p.cacheWrite5m) + float64(cacheWrite1h/mtok*p.cacheWrite1h) +
		float64(cacheRead/mtok*p.cacheRead) + float64(output/mtok*p.output)
}

// OpenAI prices, USD per million tokens; a cached input without its own price costs as input.
type openAIPrice struct {
	input, output float64
	cachedInput   *float64
}

func usd(v float64) *float64 { return &v }

var openAIPrices = map[string]openAIPrice{
	"gpt-5.5":             {5, 30, usd(0.5)},
	"gpt-5.4":             {2.5, 15, usd(0.25)},
	"gpt-5.4-mini":        {0.75, 4.5, usd(0.075)},
	"gpt-5.4-nano":        {0.2, 1.25, usd(0.02)},
	"gpt-5.2-codex":       {1.75, 14, usd(0.175)},
	"gpt-5.2-chat-latest": {1.75, 14, usd(0.175)},
	"gpt-5.2":             {1.75, 14, usd(0.175)},
	"gpt-5.1-codex-max":   {1.25, 10, usd(0.125)},
	"gpt-5.1-codex":       {1.25, 10, usd(0.125)},
	"gpt-5.1-chat-latest": {1.25, 10, usd(0.125)},
	"gpt-5.1":             {1.25, 10, usd(0.125)},
	"gpt-5-codex":         {1.25, 10, usd(0.125)},
	"gpt-5-chat-latest":   {1.25, 10, usd(0.125)},
	"gpt-5-mini":          {0.25, 2, usd(0.025)},
	"gpt-5-nano":          {0.05, 0.4, usd(0.005)},
	"gpt-5":               {1.25, 10, usd(0.125)},
	"gpt-4.1-mini":        {0.4, 1.6, usd(0.1)},
	"gpt-4.1-nano":        {0.1, 0.4, usd(0.025)},
	"gpt-4.1":             {2, 8, usd(0.5)},
	"gpt-4o-mini":         {0.15, 0.6, usd(0.075)},
	"gpt-4o":              {2.5, 10, usd(1.25)},
}

// openAIOrder is the table's own order (the order a model id tries the rows in breaks ties of length by it).
var openAIOrder = []string{"gpt-5.5", "gpt-5.4", "gpt-5.4-mini", "gpt-5.4-nano", "gpt-5.2-codex", "gpt-5.2-chat-latest", "gpt-5.2",
	"gpt-5.1-codex-max", "gpt-5.1-codex", "gpt-5.1-chat-latest", "gpt-5.1", "gpt-5-codex", "gpt-5-chat-latest", "gpt-5-mini",
	"gpt-5-nano", "gpt-5", "gpt-4.1-mini", "gpt-4.1-nano", "gpt-4.1", "gpt-4o-mini", "gpt-4o"}

var (
	openAIPrefix   = regexp.MustCompile(`^(openai/|models/)`)
	openAIDate     = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)
	openAISnapshot = regexp.MustCompile(`-\d{8}$`)
	byLength       = func() []string {
		keys := append([]string(nil), openAIOrder...)
		sort.SliceStable(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
		return keys
	}()
)

func openAIPriceOf(model string) (openAIPrice, bool) {
	n := jscompat.Trim(strings.ToLower(model))
	n = openAIPrefix.ReplaceAllLiteralString(n, "")
	n = openAIDate.ReplaceAllLiteralString(n, "")
	n = openAISnapshot.ReplaceAllLiteralString(n, "")
	if p, ok := openAIPrices[n]; ok {
		return p, true
	}
	for _, k := range byLength {
		if n == k || strings.HasPrefix(n, k+"-") {
			return openAIPrices[k], true
		}
	}
	return openAIPrice{}, false
}

// OpenAICost is the list price of an OpenAI model's tokens, input counting the cached part too; false for a model
// the table does not know (or none).
func OpenAICost(model string, input, cachedInput, output float64) (float64, bool) {
	if model == "" {
		return 0, false
	}
	p, ok := openAIPriceOf(model)
	if !ok {
		return 0, false
	}
	cached := max(0, cachedInput)
	nonCached := max(0, input-cached)
	cachedPrice := p.input
	if p.cachedInput != nil {
		cachedPrice = *p.cachedInput
	}
	const mtok = 1_000_000
	return float64(nonCached/mtok*p.input) + float64(cached/mtok*cachedPrice) + float64(max(0, output)/mtok*p.output), true
}
