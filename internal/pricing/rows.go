package pricing

import (
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
)

// Row is one model's list price, USD per million tokens, for a price list kept elsewhere (the hub's prices table).
// Provider is "anthropic" or "openai"; Key is what ClaudeKey or OpenAIKey makes of a model id.
type Row struct {
	Provider                                             string
	Key                                                  string
	Input, Output, CacheWrite5m, CacheWrite1h, CacheRead float64
}

// Rows is the whole table, sorted by provider and key. An OpenAI model without its own cached price has its cached
// input at the input price, as OpenAICost counts it; OpenAI has no cache writes.
func Rows() []Row {
	var out []Row
	for k, p := range claudePrices {
		out = append(out, Row{Provider: "anthropic", Key: k, Input: p.input, Output: p.output, CacheWrite5m: p.cacheWrite5m, CacheWrite1h: p.cacheWrite1h, CacheRead: p.cacheRead})
	}
	for k, p := range openAIPrices {
		cached := p.input
		if p.cachedInput != nil {
			cached = *p.cachedInput
		}
		out = append(out, Row{Provider: "openai", Key: k, Input: p.input, Output: p.output, CacheRead: cached})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// OpenAIKey is the table key an OpenAI model id finds, the way OpenAICost finds its row: its own key after the prefix,
// date and snapshot are cut, else the longest key it starts with; false for none.
func OpenAIKey(model string) (string, bool) {
	if model == "" {
		return "", false
	}
	n := jscompat.Trim(strings.ToLower(model))
	n = openAIPrefix.ReplaceAllLiteralString(n, "")
	n = openAIDate.ReplaceAllLiteralString(n, "")
	n = openAISnapshot.ReplaceAllLiteralString(n, "")
	if _, ok := openAIPrices[n]; ok {
		return n, true
	}
	for _, k := range byLength {
		if n == k || strings.HasPrefix(n, k+"-") {
			return k, true
		}
	}
	return "", false
}
