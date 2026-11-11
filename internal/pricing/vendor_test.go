package pricing

import (
	"math"
	"testing"
)

// A model of another vendor behind Claude Code (Kimi, GLM) is not priced as a Claude: no price is no price.
func TestVendorModelsAreNotPricedAsSonnet(t *testing.T) {
	for _, m := range []string{"kimi-k3.1-pro", "kimi-for-coding", "k3.1-pro", "glm-5.3", "glm-5.3-flash", "grok-build-0.1"} {
		if Anthropic(m) || ClaudeCostByFullID(m, 1e6, 1e6, 0, 0, 1e6) != 0 {
			t.Errorf("%s", m)
		}
	}
	for _, m := range []string{"sonnet", "opus", "claude-haiku-4-5", "claude-fable-5-1", "<synthetic>", ""} {
		if !Anthropic(m) {
			t.Errorf("%s", m)
		}
	}
	if ClaudeCostByFullID("claude-future-9", 1e6, 0, 0, 0, 0) == 0 {
		t.Error("a Claude model the table does not know is priced by its family, as before")
	}
}

func TestNewModelsHavePrices(t *testing.T) {
	for _, m := range []string{"claude-sonnet-5-5", "claude-haiku-5-5"} {
		if c, ok := ClaudeCost(m, 1e6, 0, 0, 0, 0); !ok || c <= 0 {
			t.Errorf("%s: %v %v", m, c, ok)
		}
	}
	if c, ok := OpenAICost("gpt-5.6-sol", 1e6, 0, 1e6); !ok || c != 24 {
		t.Errorf("gpt-5.6-sol: %v %v", c, ok)
	}
	// list prices of November 2026.
	if c, ok := ClaudeCost("claude-fable-5-2", 1e6, 1e6, 1e6, 1e6, 1e6); !ok || math.Abs(c-74.2) > 1e-9 {
		t.Errorf("claude-fable-5-2: %v %v", c, ok)
	}
	if c, ok := ClaudeCost("claude-sonnet-5-5", 0, 0, 0, 0, 1e6); !ok || math.Abs(c-0.1) > 1e-12 {
		t.Errorf("claude-sonnet-5-5 cache read: %v %v", c, ok)
	}
	for m, want := range map[string]float64{"gpt-5.6-terra": 14, "gpt-6-astra": 60} {
		if k, ok := OpenAIKey(m); !ok || k != m {
			t.Errorf("%s key: %q %v", m, k, ok)
		}
		if c, ok := OpenAICost(m, 1e6, 0, 1e6); !ok || math.Abs(c-want) > 1e-9 {
			t.Errorf("%s: %v %v", m, c, ok)
		}
	}
}
