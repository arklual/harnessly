package apps

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestChargeAINeverGoesPastTheBudget(t *testing.T) {
	r := Open(t.TempDir())
	b := Budget{USDPerDay: 0.05}
	now := func() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC) }
	var seen [][]string
	var input, outCap string
	run := func(_ context.Context, _ string, args, env []string, stdin string) ([]byte, error) {
		seen = append(seen, args)
		input = stdin
		for _, e := range env {
			if v, ok := strings.CutPrefix(e, "CLAUDE_CODE_MAX_OUTPUT_TOKENS="); ok {
				outCap = v
			}
		}
		return []byte(`{"type":"result","subtype":"success","is_error":false,"result":"Париж","total_cost_usd":0.004}`), nil
	}
	q := AIRequest{Prompt: "Столица Франции?", MaxTokens: 200}
	if err := q.Check(); err != nil {
		t.Fatal(err)
	}
	ans, spent, err := r.ChargeAI(context.Background(), "acme.demo", b, run, q, now)
	if err != nil || ans.Text != "Париж" || spent.USD != 0.004 {
		t.Fatalf("answer %+v spent %+v err %v", ans, spent, err)
	}
	// The prompt goes in on stdin, with no tools, a cap of money and of output.
	args := seen[0]
	if input != q.Prompt || slices.Contains(args, q.Prompt) || !slices.Contains(args, "--max-budget-usd") || outCap != "200" ||
		!slices.Contains(args, "--strict-mcp-config") ||
		args[slices.Index(args, "--tools")+1] != "" || args[slices.Index(args, "--model")+1] != "haiku" {
		t.Fatalf("args %q stdin %q cap %q", args, input, outCap)
	}
	// Calls go on while the worst of the next fits; then they are refused before the model is asked.
	for range 20 {
		if _, _, err = r.ChargeAI(context.Background(), "acme.demo", b, run, q, now); err != nil {
			break
		}
	}
	var be *BridgeError
	if !errors.As(err, &be) || be.Code != CodeQuotaExceeded {
		t.Fatalf("over budget: %v", err)
	}
	calls := len(seen)
	if _, _, err := r.ChargeAI(context.Background(), "acme.demo", b, run, q, now); err == nil || len(seen) != calls {
		t.Fatalf("a refused call asked the model: %v", err)
	}
	if s := r.SpentToday("acme.demo", now()); s.USD > b.USDPerDay {
		t.Fatalf("spent %v of %v", s.USD, b.USDPerDay)
	}
}

func TestAIRequestBounds(t *testing.T) {
	for _, q := range []AIRequest{{}, {Prompt: "x", Model: "opus"}, {Prompt: "x", MaxTokens: AIMaxTokens + 1}, {Prompt: strings.Repeat("я", AIPromptMax+1)}} {
		if err := q.Check(); err == nil {
			t.Errorf("%+v passed", q)
		}
	}
	q := AIRequest{Prompt: "x"}
	if err := q.Check(); err != nil || q.Model != "fast" || q.MaxTokens != AIDefaultTokens {
		t.Fatalf("defaults %+v %v", q, err)
	}
}

func TestAIKeepsTheReservationWhenThePriceIsUnknown(t *testing.T) {
	r := Open(t.TempDir())
	b := Budget{USDPerDay: 0.05}
	now := func() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC) }
	q := AIRequest{Prompt: "x", MaxTokens: 100}
	_ = q.Check()
	broken := func(context.Context, string, []string, []string, string) ([]byte, error) {
		return []byte("not json"), nil
	}
	if _, _, err := r.ChargeAI(context.Background(), "acme.demo", b, broken, q, now); err == nil {
		t.Fatal("an unreadable answer passed")
	}
	if s := r.SpentToday("acme.demo", now()); s.USD < q.Worst()-1e-9 {
		t.Fatalf("charged %v, worst %v", s.USD, q.Worst())
	}
}

func TestAIEnvKeepsNoSecrets(t *testing.T) {
	got := aiEnv([]string{"PATH=/bin", "HOME=/h", "GITLAB_TOKEN=x", "ANTHROPIC_API_KEY=k", "RUNTIME_CLI_TOKEN=t"})
	if strings.Join(got, " ") != "PATH=/bin HOME=/h ANTHROPIC_API_KEY=k" {
		t.Fatalf("env %q", got)
	}
}
