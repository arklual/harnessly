package pricing

import "testing"

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
