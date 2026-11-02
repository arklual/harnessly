package hubproto

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

const budgetGolden = "testdata/budget.json"

func budgetFrames() map[string]any {
	set := BudgetSet{Version: 17, Mode: EnforceWarn, IssuedAt: 1_793_455_200_000, Entries: []BudgetEntry{
		{User: "vera@example.com", Account: "claude:7d0c6f0e-0000-4000-8000-00000000000b", Status: BudgetStop, Pct: 1.02,
			ResetsAt: 1_793_466_000_000, RuleID: "l2windowmax2aaaaaaa", Reason: "Окно Claude Max №2 — 92%, сброс в 20:00"},
		{User: "vera@example.com", Status: BudgetWarn, Pct: 0.84, ResetsAt: 1_793_480_400_000, RuleID: "l2veraaaaaaaaaaaaaa",
			Reason: "Личный лимит — 84% за октябрь"},
		{User: "vera@example.com", ModelPattern: "opus-4-1", Status: BudgetStop, Forbid: true, RuleID: "l2forbidopusaaaaaaa",
			Reason: "Модель opus-4-1 в организации запрещена"},
	}}
	return map[string]any{
		"request budget.set":       frame(OpBudgetSet, 21, set),
		"result budget.set":        map[string]any{"id": 21, "ok": true, "result": BudgetSetResult{Version: 17}},
		"request budget.set empty": frame(OpBudgetSet, 22, BudgetSet{Version: 18, Mode: EnforceStop, IssuedAt: 1_793_455_260_000, Entries: []BudgetEntry{}}),
		"hello with budget":        Hello{Protocol: Version, AgentVersion: "0.17.0", Features: []string{FeatureUsage, FeatureBudget}, MachineID: "h2runneraaaaaaaaaaaa", BudgetVersion: 17},
	}
}

// TestBudgetGolden: the Go types make exactly testdata/budget.json; HUBPROTO_UPDATE=1 rewrites it.
func TestBudgetGolden(t *testing.T) {
	frames := budgetFrames()
	got, err := json.MarshalIndent(frames, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if os.Getenv("HUBPROTO_UPDATE") == "1" {
		if err := os.WriteFile(budgetGolden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(budgetGolden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the budget envelopes changed; HUBPROTO_UPDATE=1 go test ./internal/hubproto rewrites %s\n%s", budgetGolden, got)
	}
	var back map[string]json.RawMessage
	if err := json.Unmarshal(want, &back); err != nil {
		t.Fatal(err)
	}
	var set BudgetSet
	if err := json.Unmarshal(back["request budget.set"], &set); err != nil {
		t.Fatal(err)
	}
	orig := frames["request budget.set"].(map[string]any)
	if set.Version != 17 || set.Mode != EnforceWarn || len(set.Entries) != 3 || !reflect.DeepEqual(set.Entries[2], BudgetEntry{
		User: "vera@example.com", ModelPattern: "opus-4-1", Status: BudgetStop, Forbid: true, RuleID: "l2forbidopusaaaaaaa", Reason: "Модель opus-4-1 в организации запрещена"}) {
		t.Errorf("read back %+v (from %v)", set, orig["op"])
	}
	// An agent without the feature sends no budgetVersion at all: the old hello stays byte for byte.
	raw, _ := json.Marshal(Hello{Protocol: Version})
	if bytes.Contains(raw, []byte("budgetVersion")) {
		t.Errorf("%s", raw)
	}
}
