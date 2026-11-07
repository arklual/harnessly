package apps

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestBudgetOfCapsAndDefaults(t *testing.T) {
	if b := BudgetOf(&Manifest{}); b.USDPerDay != DefaultUSDPerDay || b.RunsPerDay != DefaultRunsPerDay {
		t.Fatalf("defaults: %+v", b)
	}
	if b := BudgetOf(&Manifest{Budget: &Budget{USDPerDay: 50, RunsPerDay: 99}}); b.USDPerDay != MaxUSDPerDay || b.RunsPerDay != MaxRunsPerDay {
		t.Fatalf("caps: %+v", b)
	}
	var problems []string
	(&Manifest{Budget: &Budget{USDPerDay: 9, RunsPerDay: -1}}).checkBudget(func(path, _ string, _ ...any) { problems = append(problems, path) })
	if len(problems) != 2 {
		t.Fatalf("checks: %v", problems)
	}
	for usd, want := range map[float64]string{0.5: "$0.50", 5: "$5", 0.00059899: "$0.0006", 0.01: "$0.01", 1.25: "$1.25", 0: "$0"} {
		if got := Money(usd); got != want {
			t.Errorf("Money(%v) = %s, want %s", usd, got, want)
		}
	}
}

func TestSpendStaysWithinTheBudget(t *testing.T) {
	r := Open(t.TempDir())
	b := Budget{USDPerDay: 1, RunsPerDay: 2}
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	// A call reserves its cap, then pays what it cost; the day never goes past the budget.
	got, err := r.ReserveUSD("acme.demo", b, 0.7, now)
	if err != nil || got != 0.7 {
		t.Fatalf("reserve: %v %v", got, err)
	}
	second, err := r.ReserveUSD("acme.demo", b, 0.7, now)
	if err != nil || math.Abs(second-0.3) > 1e-9 {
		t.Fatalf("second reserve takes what is left: %v %v", second, err)
	}
	if _, err := r.ReserveUSD("acme.demo", b, 0.1, now); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("nothing left: %v", err)
	}
	if err := r.SettleUSD("acme.demo", 0.7, 0.2, now); err != nil {
		t.Fatal(err)
	}
	if s := r.SpentToday("acme.demo", now); math.Abs(s.USD-0.5) > 1e-9 {
		t.Fatalf("settled: %+v", s)
	}
	// Runs count up to the day's number; a new day starts at zero.
	for i := range 2 {
		if err := r.TakeRun("acme.demo", b, now); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if err := r.TakeRun("acme.demo", b, now); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("third run: %v", err)
	}
	if s := r.SpentToday("acme.demo", now.Add(24*time.Hour)); s.USD != 0 || s.Runs != 0 {
		t.Fatalf("next day: %+v", s)
	}
}

func TestAccessShowsTheBudget(t *testing.T) {
	m := &Manifest{Permissions: []string{"ai", "agents.run"}, Budget: &Budget{USDPerDay: 2}}
	got := AccessOf(m, nil)
	if len(got) != 2 || got[0].Key != "perm:ai@$2" || got[0].Label.Ru != "Вызывать модель от вашего имени — до $2 в день" ||
		got[1].Key != "perm:agents.run@3" || got[1].Label.Ru != "Запускать агента в новом чате — до 3 раз в день" {
		t.Fatalf("access: %+v", got)
	}
}

func TestTimesRu(t *testing.T) {
	for n, want := range map[int]string{1: "1 раза", 3: "3 раз", 11: "11 раз", 20: "20 раз", 21: "21 раза"} {
		if got := timesRu(n); got != want {
			t.Errorf("%d: %s", n, got)
		}
	}
}
