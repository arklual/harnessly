package pricing

// openAILongContext are the prices of the models that charge more for a request past 200k tokens of context.
var openAILongContext = map[string]openAIPrice{
	"gpt-5.5": {10, 45, usd(1)},
	"gpt-5.4": {5, 22.5, usd(0.5)},
}

// openAIKeyOf is the price table row a model id finds, as openAIPriceOf finds it.
func openAIKeyOf(model string) (string, bool) {
	p, ok := openAIPriceOf(model)
	if !ok {
		return "", false
	}
	for k, row := range openAIPrices {
		if row.input == p.input && row.output == p.output && row.cachedInput == p.cachedInput {
			return k, true
		}
	}
	return "", false
}

// OpenAICostInWindow is OpenAICost for a session whose context window is known: a window past 200k tokens is billed at
// the model's long-context prices where it has them.
func OpenAICostInWindow(model string, input, cachedInput, output, contextWindow float64) (float64, bool) {
	if model == "" {
		return 0, false
	}
	p, ok := openAIPriceOf(model)
	if !ok {
		return 0, false
	}
	if contextWindow > 200_000 {
		if k, ok := openAIKeyOf(model); ok {
			if long, has := openAILongContext[k]; has {
				p = long
			}
		}
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
