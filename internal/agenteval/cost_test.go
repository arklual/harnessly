package agenteval

import (
	"math"
	"strings"
	"testing"
)

func TestReportSeparatesEfforts(t *testing.T) {
	report := Summarize([]Result{
		{Scenario: "synthetic", Model: "codex:gpt-5.6-luna", Effort: "low", State: Pass},
		{Scenario: "synthetic", Model: "codex:gpt-5.6-luna", Effort: "medium", State: Fail},
	}, nil, 0)
	if len(report.Cells) != 2 || len(report.Models) != 2 {
		t.Fatalf("efforts merged: %+v", report.Cells)
	}
}

func TestReportMarksUnknownCost(t *testing.T) {
	report := Summarize([]Result{{Scenario: "synthetic", Model: "unpriced-synthetic", State: Pass, CostUnknown: true}}, nil, 0)
	if text := report.Markdown("synthetic"); !strings.Contains(text, "cost unknown") {
		t.Fatalf("missing unknown-price warning: %s", text)
	}
}

func TestUnknownModelCostIsNotReportedAsFree(t *testing.T) {
	if _, known := pricedCost(Facts{Model: "gpt-unpriced-synthetic", TokensIn: 100}); known {
		t.Fatal("unknown model must not be treated as a known zero cost")
	}
	if _, known := pricedCost(Facts{ByModel: map[string]Usage{"gpt-5.6-terra": {In: 100}, "unpriced-synthetic": {Out: 10}}}); known {
		t.Fatal("a known helper must not conceal an unpriced model")
	}
}

func TestCostIncludesOpenAIUsage(t *testing.T) {
	// Facts stores uncached input separately from cacheRead; the pricing API expects their sum.
	f := Facts{Model: "gpt-5.6-terra", TokensIn: 1000, CacheRead: 2000, TokensOut: 100}
	want := .0036 // 1000*2 + 2000*.2 + 100*12, per million tokens.
	if got := cost(f); math.Abs(got-want) > 1e-10 {
		t.Fatalf("got %g, want %g; GPT is not free", got, want)
	}
}
