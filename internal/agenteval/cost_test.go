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

func TestReportShowsTimeScale(t *testing.T) {
	text := Summarize([]Result{{Scenario: "cron", Model: "haiku", State: Pass, TimeScale: 60}}, nil, 0).Markdown("cron")
	if !strings.Contains(text, "time ×60") {
		t.Fatalf("the report hides the time scale: %s", text)
	}
	if text := Summarize([]Result{{Scenario: "cron", Model: "haiku", State: Pass}}, nil, 0).Markdown("cron"); strings.Contains(text, "time ×") {
		t.Fatalf("a plain-clock report shows a scale: %s", text)
	}
}

func TestScenarioHoldAndCheckpoints(t *testing.T) {
	sc, err := ParseScenario("c.yaml", []byte("id: cc\nprompt: do\nhold_s: 18000\ncheckpoints:\n  - {name: first, after: 25m, by: 40m, kind: tool, match: Bash, min: 1}\nexpect: [{text: x}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if sc.HoldS != 18000 || len(sc.Checkpoints) != 1 || sc.Checkpoints[0]["by"] != "40m" {
		t.Fatalf("hold %d, checkpoints %v", sc.HoldS, sc.Checkpoints)
	}
	if _, err := ParseScenario("c.yaml", []byte("id: cc\nprompt: do\nhold_s: -1\nexpect: [{text: x}]\n")); err == nil {
		t.Fatal("a negative hold_s was taken")
	}
}
