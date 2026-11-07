package apps

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The budget of an app: what it may spend of the person's model a day through `ai` and how many chats it may
// start through `agents.run`. The manifest says it (`budget`), the host caps it, the install screen shows it in the
// lines of the two rights, and the runtime keeps the day's spending per app on this machine: a call that would go past
// it is refused before it is made.

// Budget is what an app may spend a day.
type Budget struct {
	// USDPerDay is the most the model calls of `ai` cost in a day, in US dollars of the model's price.
	USDPerDay float64 `json:"usdPerDay,omitempty"`
	// RunsPerDay is how many chats `agents.run` starts in a day.
	RunsPerDay int `json:"runsPerDay,omitempty"`
}

// The host's caps and the budget of an app that says none.
const (
	MaxUSDPerDay      = 5.0
	MaxRunsPerDay     = 20
	DefaultUSDPerDay  = 0.5
	DefaultRunsPerDay = 3
)

// ErrOverBudget refuses a call that would go past the app's budget of the day.
var ErrOverBudget = errors.New("apps: over the app's budget of the day")

// BudgetOf is the app's budget as the host keeps it: the manifest's, or the defaults, within the caps.
func BudgetOf(m *Manifest) Budget {
	b := Budget{USDPerDay: DefaultUSDPerDay, RunsPerDay: DefaultRunsPerDay}
	if m != nil && m.Budget != nil {
		if m.Budget.USDPerDay > 0 {
			b.USDPerDay = math.Min(m.Budget.USDPerDay, MaxUSDPerDay)
		}
		if m.Budget.RunsPerDay > 0 {
			b.RunsPerDay = min(m.Budget.RunsPerDay, MaxRunsPerDay)
		}
	}
	return b
}

// checkBudget is the manifest's check of its `budget`.
func (m *Manifest) checkBudget(add func(path, format string, a ...any)) {
	if m.Budget == nil {
		return
	}
	if m.Budget.USDPerDay < 0 || m.Budget.USDPerDay > MaxUSDPerDay {
		add("budget.usdPerDay", "is from 0 to %v", MaxUSDPerDay)
	}
	if m.Budget.RunsPerDay < 0 || m.Budget.RunsPerDay > MaxRunsPerDay {
		add("budget.runsPerDay", "is from 0 to %d", MaxRunsPerDay)
	}
}

// Money writes an amount the way the install screen and the journal show it: $5, $0.50, $0.0006.
func Money(usd float64) string {
	s := strings.TrimRight(fmt.Sprintf("%.4f", usd), "0")
	whole, cents, _ := strings.Cut(s, ".")
	switch {
	case cents == "":
		return "$" + whole
	case len(cents) == 1:
		return "$" + whole + "." + cents + "0"
	}
	return "$" + s
}

// Spent is an app's spending of one day.
type Spent struct {
	Day  string  `json:"day"`
	USD  float64 `json:"usd"`
	Runs int     `json:"runs"`
}

var budgetMu sync.Mutex

func (r *Registry) spentFile(id string) string {
	return filepath.Join(r.Root, "spent", PluginName(id)+".json")
}

func today(now time.Time) string { return now.UTC().Format(time.DateOnly) }

// SpentToday is what the app spent today (UTC).
func (r *Registry) SpentToday(id string, now time.Time) Spent {
	budgetMu.Lock()
	defer budgetMu.Unlock()
	return r.readSpent(id, now)
}

func (r *Registry) readSpent(id string, now time.Time) Spent {
	s := Spent{Day: today(now)}
	raw, err := os.ReadFile(r.spentFile(id))
	if err != nil {
		return s
	}
	var got Spent
	if json.Unmarshal(raw, &got) != nil || got.Day != s.Day {
		return s
	}
	return got
}

func (r *Registry) writeSpent(id string, s Spent) error {
	raw, _ := json.Marshal(s)
	file := r.spentFile(id)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(file, raw)
}

// ReserveUSD sets aside up to `most` of the app's money for one call and answers how much: the call is made with that
// cap, and SettleUSD puts back what it did not spend. ErrOverBudget when nothing is left.
func (r *Registry) ReserveUSD(id string, b Budget, most float64, now time.Time) (float64, error) {
	budgetMu.Lock()
	defer budgetMu.Unlock()
	s := r.readSpent(id, now)
	left := b.USDPerDay - s.USD
	if left <= 0.0001 {
		return 0, ErrOverBudget
	}
	take := math.Min(left, most)
	s.USD += take
	return take, r.writeSpent(id, s)
}

// SettleUSD replaces a reservation with what the call cost.
func (r *Registry) SettleUSD(id string, reserved, cost float64, now time.Time) error {
	budgetMu.Lock()
	defer budgetMu.Unlock()
	s := r.readSpent(id, now)
	s.USD = math.Max(0, s.USD-reserved+math.Max(cost, 0))
	return r.writeSpent(id, s)
}

// TakeRun counts one chat started by `agents.run`; ErrOverBudget when the day's runs are spent.
func (r *Registry) TakeRun(id string, b Budget, now time.Time) error {
	budgetMu.Lock()
	defer budgetMu.Unlock()
	s := r.readSpent(id, now)
	if s.Runs >= b.RunsPerDay {
		return ErrOverBudget
	}
	s.Runs++
	return r.writeSpent(id, s)
}

// ReturnRun gives back a run that did not start a chat.
func (r *Registry) ReturnRun(id string, now time.Time) error {
	budgetMu.Lock()
	defer budgetMu.Unlock()
	s := r.readSpent(id, now)
	s.Runs = max(0, s.Runs-1)
	return r.writeSpent(id, s)
}
