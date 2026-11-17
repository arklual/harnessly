package pricing

import (
	"math"
	"testing"
)

func TestLunaStandardPrices(t *testing.T) {
	// Official model pages verified 2026-11-16; standard pricing, per-request context <=272k.
	// https://developers.openai.com/api/docs/models/gpt-5.6-luna
	// https://developers.openai.com/api/docs/models/gpt-6-luna
	for model, want := range map[string]float64{"gpt-5.6-luna": .00144, "gpt-6-luna": .00062, "gpt-6.1-terra": .0061} {
		got, known := OpenAICost(model, 3000, 2000, 1000)
		if !known || math.Abs(got-want) > 1e-10 {
			t.Fatalf("%s: cost %g known=%v, want %g", model, got, known, want)
		}
	}
}
