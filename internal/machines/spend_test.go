package machines

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func modelLine(req, model string, ts time.Time, out int) string {
	return fmt.Sprintf(`{"type":"assistant","requestId":"%s","timestamp":"%s","message":{"id":"m-%s","model":"%s","usage":{"input_tokens":10,"output_tokens":%d}}}`,
		req, ts.UTC().Format(time.RFC3339Nano), req, model, out)
}

// What Claude cost since a moment: the hours from it on, by the model family; earlier calls and Codex do not count.
func TestSpendSince(t *testing.T) {
	now := time.Date(2026, 11, 8, 14, 20, 0, 0, time.UTC)
	dayStart := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC) // 03:00 in Moscow
	base := t.TempDir()
	file := filepath.Join(base, "projects", "p", "s.jsonl")
	_ = os.MkdirAll(filepath.Dir(file), 0o755)
	lines := modelLine("y", "claude-opus-4-6", dayStart.Add(-30*time.Minute), 1000) + "\n" + // yesterday by this day
		modelLine("a", "claude-opus-4-6", dayStart.Add(10*time.Minute), 1000) + "\n" +
		modelLine("b", "claude-sonnet-4-5", now.Add(-2*time.Hour), 2000) + "\n" +
		modelLine("c", "claude-opus-4-6", now.Add(-5*time.Minute), 3000) + "\n" +
		modelLine("c", "claude-opus-4-6", now.Add(-5*time.Minute), 3000) + "\n" // a repeated chunk counts once
	_ = os.WriteFile(file, []byte(lines), 0o600)
	u := &Usage{ClaudeRoot: filepath.Join(base, "projects"), CodexRoot: filepath.Join(base, "none"), Now: func() time.Time { return now }}
	if u.Spend() != nil {
		t.Fatal("a spend before the first pass")
	}
	u.Build()
	s := u.Spend()
	if s == nil || s.AsOf != float64(now.Unix()) {
		t.Fatalf("spend %+v", s)
	}
	cost := func(model string, out int) float64 {
		return rowCost("claude", msgRow{model: model, input: 10, output: float64(out)})
	}
	got := s.Since(float64(dayStart.Unix()))
	wantOpus, wantSonnet := cost("claude-opus-4-6", 1000)+cost("claude-opus-4-6", 3000), cost("claude-sonnet-4-5", 2000)
	if wantOpus <= 0 || wantSonnet <= 0 {
		t.Fatal("no price for the models")
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
	if !near(got.ByModel["opus"], wantOpus) || !near(got.ByModel["sonnet"], wantSonnet) || len(got.ByModel) != 2 ||
		!near(got.USD, wantOpus+wantSonnet) {
		t.Fatalf("since the day start: %+v, want opus %v sonnet %v", got, wantOpus, wantSonnet)
	}
	all := s.Since(0)
	if !near(all.USD, got.USD+cost("claude-opus-4-6", 1000)) {
		t.Fatalf("the whole day back: %v", all.USD)
	}
	if late := s.Since(float64(now.Unix()) + 3600); late.USD != 0 || len(late.ByModel) != 0 {
		t.Fatalf("since the future: %+v", late)
	}
}

func TestClaudeBilling(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, ".claude.json")
	b := &ClaudeBilling{ConfigDir: dir, ConfigFile: config}
	if b.Mode() != nil {
		t.Fatal("told without files")
	}
	_ = os.WriteFile(config, []byte(`{"primaryApiKey":"sk-ant-x"}`), 0o600)
	if m := b.Mode(); m == nil || *m != BillingAPI {
		t.Fatalf("console key: %v", m)
	}
	_ = os.WriteFile(filepath.Join(dir, ".credentials.json"),
		[]byte(`{"claudeAiOauth":{"scopes":["user:inference","user:profile"],"subscriptionType":"max"}}`), 0o600)
	if m := b.Mode(); m == nil || *m != BillingSubscription {
		t.Fatalf("subscription: %v", m)
	}
}
