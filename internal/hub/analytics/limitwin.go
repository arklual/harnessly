package analytics

import (
	"cmp"
	"math"
	"net/http"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The history of the rate-limit windows (docs/hub/analytics.md, «Limit windows»): for each subscription account
// the scope can see, each window with the cycles that ended in the period and how full each one got.

// fullPct is the use from which a cycle counts as run out.
const fullPct = 95

// WindowCycleView is one cycle of a window.
type WindowCycleView struct {
	EndsAt   string  `json:"endsAt"`
	PeakPct  float64 `json:"peakPct"`
	Measures int     `json:"measures"`
}

// WindowHistory is the cycles of one window of an account.
type WindowHistory struct {
	Name      string `json:"name"`
	Scope     string `json:"scope"`
	LengthSec int64  `json:"lengthSec"`
	// Cycles are oldest first.
	Cycles []WindowCycleView `json:"cycles"`
	// AvgPeakPct is the mean of the cycles' highest use; FullCycles the number that reached fullPct.
	AvgPeakPct float64 `json:"avgPeakPct"`
	FullCycles int     `json:"fullCycles"`
}

// AccountWindows is the history of the windows of one account.
type AccountWindows struct {
	AccountID string          `json:"accountId"`
	Label     string          `json:"label"`
	Provider  string          `json:"provider"`
	Plan      string          `json:"plan"`
	Windows   []WindowHistory `json:"windows"`
}

// LimitWindowsResponse is GET …/limit-windows.
type LimitWindowsResponse struct {
	Scope    orgs.ScopeView   `json:"scope"`
	Period   Period           `json:"period"`
	Accounts []AccountWindows `json:"accounts"`
}

func (s *Service) mountLimitWindows(mux *http.ServeMux) {
	mux.Handle("GET /api/orgs/{org}/limit-windows", s.gate(s.limitWindows))
}

func (s *Service) limitWindows(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	x, err := s.orgScope(ctx, st, p, r, authz.Money)
	if err != nil {
		return err
	}
	sp, err := parsePeriod(r.URL.Query(), x.loc, x.now)
	if err != nil {
		return err
	}
	all, err := st.AccountsByOrg(ctx, x.org.ID)
	if err != nil {
		return err
	}
	var accounts []store.Account
	var ids []string
	for _, a := range all {
		if a.Funding != store.FundingOrg || a.Kind != "subscription" {
			continue
		}
		ok, err := x.reachesAccount(ctx, st, a)
		if err != nil {
			return err
		}
		if ok {
			accounts = append(accounts, a)
			ids = append(ids, a.ID)
		}
	}
	from, to := sp.ms()
	cycles, err := st.WindowCycles(ctx, ids, from, to)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, LimitWindowsResponse{Scope: orgs.ScopeView{Kind: x.sc.Kind, ID: x.sc.ID},
		Period: sp.period(), Accounts: windowHistories(accounts, cycles)})
	return nil
}

// windowHistories groups the cycles by account and window. An account with no cycle in the period is left out.
func windowHistories(accounts []store.Account, cycles []store.WindowCycle) []AccountWindows {
	type key struct{ account, scope, name string }
	by := map[key]*WindowHistory{}
	var order []key
	for _, c := range cycles {
		k := key{c.AccountID, c.Scope, c.Name}
		h := by[k]
		if h == nil {
			h = &WindowHistory{Name: c.Name, Scope: c.Scope, Cycles: []WindowCycleView{}}
			by[k] = h
			order = append(order, k)
		}
		h.LengthSec = cmp.Or(c.LengthSec, h.LengthSec)
		h.Cycles = append(h.Cycles, WindowCycleView{EndsAt: time.UnixMilli(c.ResetsAt).UTC().Format(time.RFC3339),
			PeakPct: math.Round(c.PeakPct*10) / 10, Measures: c.Measures})
		h.AvgPeakPct += c.PeakPct
		if c.PeakPct >= fullPct {
			h.FullCycles++
		}
	}
	out := []AccountWindows{}
	for _, a := range accounts {
		var ws []WindowHistory
		for _, k := range order {
			if k.account != a.ID {
				continue
			}
			h := *by[k]
			h.AvgPeakPct = math.Round(h.AvgPeakPct/float64(len(h.Cycles))*10) / 10
			ws = append(ws, h)
		}
		if len(ws) == 0 {
			continue
		}
		// The longest window first: the week is what the screen tells about.
		slices.SortStableFunc(ws, func(l, r WindowHistory) int { return cmp.Compare(r.LengthSec, l.LengthSec) })
		out = append(out, AccountWindows{AccountID: a.ID, Label: accountLabel(a, true), Provider: a.Provider, Plan: a.Plan, Windows: ws})
	}
	slices.SortFunc(out, func(l, r AccountWindows) int {
		return cmp.Or(cmp.Compare(l.Label, r.Label), cmp.Compare(l.AccountID, r.AccountID))
	})
	return out
}
