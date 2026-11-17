package agenteval

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestRunReturnsInfrastructureErrorWithRecordedEvidence(t *testing.T) {
	old := sandboxLevel
	sandboxLevel = func() int { return 0 }
	t.Cleanup(func() { sandboxLevel = old })
	out := t.TempDir()
	o := Options{Scenarios: []*Scenario{{ID: "synthetic", Prompt: "synthetic"}}, Models: []Model{{Key: "claude:haiku", Agent: "claude", Model: "haiku"}}, Out: out, Stand: StandConfig{Root: filepath.Join(out, "stands")}}
	got, err := Run(context.Background(), o)
	if len(got) != 1 || got[0].State != Infra {
		t.Fatalf("expected one infrastructure failure, got %+v", got)
	}
	saved, readErr := ReadResults(filepath.Join(out, "results.jsonl"))
	if readErr != nil || len(saved) != 1 || saved[0].State != Infra {
		t.Fatalf("infrastructure evidence lost: results=%+v error=%v", saved, readErr)
	}
	if !errors.Is(err, ErrInfra) {
		t.Fatalf("infrastructure-only run must not report success: got %v", err)
	}
}
