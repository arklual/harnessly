package agenteval

import (
	"context"
	"math"
	"testing"
)

func TestReuseRepricesLegacyGPTResult(t *testing.T) {
	for _, missingFacts := range []bool{false, true} {
		t.Run(map[bool]string{false: "with usage", true: "without usage"}[missingFacts], func(t *testing.T) {
			o := Options{Scenarios: []*Scenario{{ID: "synthetic", Prompt: "synthetic"}}, Models: []Model{{Key: "codex:gpt-5.6-luna", Agent: "codex", Model: "gpt-5.6-luna", Effort: "low"}}, Out: t.TempDir(), Setup: "synthetic", MaxUSD: 1}
			key := o.Cells()[0].Key(o.Setup)
			r := Result{Key: key, Scenario: "synthetic", Model: "codex:gpt-5.6-luna", Effort: "low", State: Pass}
			if !missingFacts {
				r.Facts = &Facts{Model: "gpt-5.6-luna", TokensIn: 1000, TokensOut: 1000}
			}
			o.Reuse = map[string]Result{key: r}
			got, err := Run(context.Background(), o)
			if err != nil || len(got) != 1 {
				t.Fatalf("results=%+v err=%v", got, err)
			}
			if missingFacts {
				if !got[0].CostUnknown {
					t.Fatal("legacy zero without usage must not imply free")
				}
			} else if got[0].CostUnknown || math.Abs(got[0].USD-.0014) > 1e-10 {
				t.Fatalf("legacy cost not repriced: %+v", got[0])
			}
		})
	}
}
