package analytics

import (
	"cmp"
	"context"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/usage"
)

// The organization's accounts: every account its machines used, with who used it this month and what it
// cost at list prices, and registering one the hub only discovered — what the seed file does (docs/hub/orgs.md,
// «accounts»), from the screen, or registering one before any machine used it.

// AccountView is one account of GET …/accounts.
type AccountView struct {
	ID         string           `json:"id"`
	Label      string           `json:"label"`
	Provider   string           `json:"provider"`
	Kind       string           `json:"kind"`
	Plan       string           `json:"plan"`
	Funding    string           `json:"funding"`
	MonthlyFee Money            `json:"monthlyFee"`
	Discovered bool             `json:"discovered"`
	Scope      orgs.ScopeView   `json:"scope"`
	Owner      *orgs.PersonRef  `json:"owner"`
	MonthList  Money            `json:"monthList"`
	People     []orgs.PersonRef `json:"people"`
	CreatedAt  string           `json:"createdAt"`
}

// AccountsResponse is GET /api/orgs/{org}/accounts.
type AccountsResponse struct {
	Currency Currency      `json:"currency"`
	Accounts []AccountView `json:"accounts"`
}

func (s *Service) mountAccounts(mux *http.ServeMux) {
	mux.Handle("GET /api/orgs/{org}/accounts", s.gate(s.accounts))
	mux.Handle("POST /api/orgs/{org}/accounts", s.gate(s.createAccount))
	mux.Handle("PATCH /api/orgs/{org}/accounts/{id}", s.gate(s.registerAccount))
}

func (s *Service) accounts(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	x, err := s.orgScope(ctx, st, p, r, authz.Money)
	if err != nil {
		return err
	}
	start := monthStart(x.now, x.loc)
	sp := newSpan(start, start.AddDate(0, 1, 0), x.loc)
	cur := s.currency(ctx, x.org.Currency, sp)
	views, err := s.accountViews(ctx, st, x, cur)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, AccountsResponse{Currency: cur, Accounts: views})
	return nil
}

// accountViews are the accounts the scope reaches or used this month: discovered first, then by this month's money.
func (s *Service) accountViews(ctx context.Context, st store.Store, x *orgScope, cur Currency) ([]AccountView, error) {
	all, err := st.AccountsByOrg(ctx, x.org.ID)
	if err != nil {
		return nil, err
	}
	start := monthStart(x.now, x.loc)
	from, to := start.UnixMilli(), start.AddDate(0, 1, 0).UnixMilli()
	rows, err := st.RollupRows(ctx, x.org.ID, from, to)
	if err != nil {
		return nil, err
	}
	used := map[string]map[string]bool{}
	for _, r := range rows {
		if r.AccountID == "" || r.UserID == "" || !x.sees(r) {
			continue
		}
		if used[r.AccountID] == nil {
			used[r.AccountID] = map[string]bool{}
		}
		used[r.AccountID][r.UserID] = true
	}
	ids := make([]string, 0, len(all))
	for _, a := range all {
		ids = append(ids, a.ID)
	}
	totals, err := st.AccountListTotals(ctx, ids, from, to)
	if err != nil {
		return nil, err
	}
	names := map[string]orgs.PersonRef{}
	person := func(id string) (orgs.PersonRef, error) {
		if p, ok := names[id]; ok {
			return p, nil
		}
		p := orgs.PersonRef{UserID: id, Name: "Неизвестно"}
		u, err := st.UserByID(ctx, id)
		switch {
		case store.IsNotFound(err):
		case err != nil:
			return p, err
		default:
			p.Name = cmp.Or(u.Name, u.Email)
		}
		names[id] = p
		return p, nil
	}
	out := []AccountView{}
	for _, a := range all {
		if !x.reaches(a.ScopeKind, a.ScopeID) && len(used[a.ID]) == 0 {
			continue
		}
		v := AccountView{ID: a.ID, Label: accountLabel(a, true), Provider: a.Provider, Kind: a.Kind, Plan: a.Plan, Funding: a.Funding,
			MonthlyFee: cur.Micros(a.MonthlyFeeMicros), Discovered: a.Discovered, Scope: orgs.ScopeView{Kind: a.ScopeKind, ID: a.ScopeID},
			MonthList: cur.Micros(totals[a.ID]), People: []orgs.PersonRef{}, CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339)}
		if a.OwnerUserID != "" {
			o, err := person(a.OwnerUserID)
			if err != nil {
				return nil, err
			}
			v.Owner = &o
		}
		for id := range used[a.ID] {
			p, err := person(id)
			if err != nil {
				return nil, err
			}
			v.People = append(v.People, p)
		}
		slices.SortFunc(v.People, func(a, b orgs.PersonRef) int { return strings.Compare(a.Name, b.Name) })
		out = append(out, v)
	}
	slices.SortStableFunc(out, func(a, b AccountView) int {
		if a.Discovered != b.Discovered {
			if a.Discovered {
				return -1
			}
			return 1
		}
		return cmp.Compare(b.MonthList.USD, a.MonthList.USD)
	})
	return out, nil
}

// sees says whether a rollup row is the scope's: a team's or a tenant's own money, or any for the organization.
func (x *orgScope) sees(r store.RollupRow) bool {
	switch x.sc.Kind {
	case store.ScopeOrg:
		return true
	case store.ScopeTenant:
		return r.SpaceID == x.sc.ID
	}
	return r.TeamID == x.sc.ID
}

// accountReq is the body of POST and PATCH …/accounts: what the account is and who pays for it.
type accountReq struct {
	Provider    string // POST: the CLI the account is used with, claude or codex
	Ref         string // POST: the account as the machine reports it: claude:<uuid>, codex:<id>, key:<hash>
	Label       string
	Plan        string
	Kind        string
	Funding     string
	MonthlyFee  *float64
	OwnerUserID string
	TenantID    string
	History     bool
}

var accountRefRe = regexp.MustCompile(`^(claude|codex|key):[A-Za-z0-9._-]{1,100}$`)

// scopeArg is a scope as orgs.ScopeIn takes it: "" for the organization itself.
func scopeArg(kind, id string) string {
	if kind == store.ScopeOrg {
		return ""
	}
	return kind + ":" + id
}

// createAccount is POST /api/orgs/{org}/accounts: an account registered before any machine used it (a key or a
// subscription the organization bought). The machines' reports of that ref then land on it instead of a discovered one.
func (s *Service) createAccount(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	var req accountReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	st := s.db.S()
	o, err := orgs.Resolve(ctx, st, p, r.PathValue("org"))
	if err != nil {
		return err
	}
	if req.Provider != "claude" && req.Provider != "codex" {
		return httpx.Invalid("provider", "Где работает аккаунт — Claude или Codex")
	}
	ref := strings.TrimSpace(req.Ref)
	if !accountRefRe.MatchString(ref) {
		return httpx.Invalid("ref", "Ссылка на аккаунт — claude:<id>, codex:<id> или key:<hash>, как её сообщает машина")
	}
	next := store.Account{OrgKey: o.ID, Provider: usage.PriceProvider(req.Provider, ""), AccountRef: ref, Kind: "subscription",
		CreatedBy: p.User.ID, CreatedAt: s.now()}
	if strings.HasPrefix(ref, "key:") {
		next.Kind = "api_key"
	}
	if have, err := st.AccountByRef(ctx, o.ID, next.Provider, ref); err == nil {
		return httpx.Conflict("account_exists", "Этот аккаунт уже есть на хабе").With("id", have.ID)
	} else if !store.IsNotFound(err) {
		return err
	}
	sc, err := s.fillAccount(ctx, st, p, r.PathValue("org"), o, &next, req)
	if err != nil {
		return err
	}
	return s.saveAccount(w, r, p, o, sc, nil, next, req.History)
}

// registerAccount is PATCH /api/orgs/{org}/accounts/{id}: someone with money rights on the account's scope says what
// the account is and who pays for it. The account stops being «discovered», its sessions take its funding, and with
// history the owner's sessions without an account are given it; the hours whose money moved are summed anew.
func (s *Service) registerAccount(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	var req accountReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	st := s.db.S()
	o, err := orgs.Resolve(ctx, st, p, r.PathValue("org"))
	if err != nil {
		return err
	}
	cur, err := st.AccountByID(ctx, r.PathValue("id"))
	if store.IsNotFound(err) || (err == nil && cur.OrgKey != o.ID) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, _, err := orgs.ScopeIn(ctx, st, p, r.PathValue("org"), scopeArg(cur.ScopeKind, cur.ScopeID), authz.Money); err != nil {
		return err
	}
	next := cur
	sc, err := s.fillAccount(ctx, st, p, r.PathValue("org"), o, &next, req)
	if err != nil {
		return err
	}
	return s.saveAccount(w, r, p, o, sc, &cur, next, req.History)
}

// fillAccount checks the request and writes it into next; the answer is the account's scope, on which p has money rights.
func (s *Service) fillAccount(ctx context.Context, st store.Store, p *auth.Principal, ref string, o store.Org, next *store.Account, req accountReq) (authz.Scope, error) {
	next.Discovered = false
	label := strings.TrimSpace(req.Label)
	if label == "" || utf8.RuneCountInString(label) > 80 {
		return authz.Scope{}, httpx.Invalid("label", "Название аккаунта — от 1 до 80 символов")
	}
	next.Label = label
	next.Plan = strings.TrimSpace(req.Plan)
	if utf8.RuneCountInString(next.Plan) > 40 {
		return authz.Scope{}, httpx.Invalid("plan", "Тариф — до 40 символов")
	}
	if req.Kind != "" {
		if !slices.Contains([]string{"subscription", "api_key", "prepaid"}, req.Kind) {
			return authz.Scope{}, httpx.Invalid("kind", "Вид аккаунта — подписка, ключ или предоплата")
		}
		next.Kind = req.Kind
	}
	switch req.Funding {
	case store.FundingOrg, store.FundingPersonal:
		next.Funding = req.Funding
	default:
		return authz.Scope{}, httpx.Invalid("funding", "Кто платит — организация или сам человек")
	}
	if req.MonthlyFee != nil {
		if math.IsNaN(*req.MonthlyFee) || *req.MonthlyFee < 0 || *req.MonthlyFee > 100000 {
			return authz.Scope{}, httpx.Invalid("monthlyFee", "Абонентская плата — от 0 до 100 000 $ в месяц")
		}
		next.MonthlyFeeMicros = int64(math.Round(*req.MonthlyFee * 1e6))
	}
	if next.Kind != "subscription" {
		next.MonthlyFeeMicros = 0
	}
	if req.OwnerUserID != "" {
		if _, err := st.UserByID(ctx, req.OwnerUserID); store.IsNotFound(err) {
			return authz.Scope{}, httpx.Invalid("ownerUserId", "Такого человека нет")
		} else if err != nil {
			return authz.Scope{}, err
		}
		next.OwnerUserID = req.OwnerUserID
	}
	if req.TenantID != "" {
		sp, err := st.SpaceByID(ctx, req.TenantID)
		if store.IsNotFound(err) || (err == nil && sp.OrgID != o.ID) {
			return authz.Scope{}, httpx.Invalid("tenantId", "Это пространство не в организации")
		}
		if err != nil {
			return authz.Scope{}, err
		}
		next.ScopeKind, next.ScopeID = store.ScopeTenant, sp.ID
	} else {
		next.ScopeKind, next.ScopeID = store.ScopeOrg, o.ID
	}
	_, sc, err := orgs.ScopeIn(ctx, st, p, ref, scopeArg(next.ScopeKind, next.ScopeID), authz.Money)
	return sc, err
}

// saveAccount writes the account (a new one when cur is nil), moves its sessions' money, journals account.create or
// account.update and answers the account as the list shows it.
func (s *Service) saveAccount(w http.ResponseWriter, r *http.Request, p *auth.Principal, o store.Org, sc authz.Scope, cur *store.Account, next store.Account, history bool) error {
	ctx := r.Context()
	now := s.now()
	err := s.db.Tx(ctx, func(st store.Store) error {
		var moved []string
		if cur == nil {
			if err := st.CreateAccount(ctx, &next); store.IsUnique(err) {
				return httpx.Conflict("account_exists", "Этот аккаунт уже есть на хабе")
			} else if err != nil {
				return err
			}
		} else {
			if err := st.UpdateAccount(ctx, next); err != nil {
				return err
			}
			m, err := st.SetAccountFunding(ctx, o.ID, next.ID, next.Funding)
			if err != nil {
				return err
			}
			moved = m
		}
		if history && next.OwnerUserID != "" {
			keys, err := st.SessionsWithoutAccount(ctx, o.ID, next.OwnerUserID, cliOf(next.Provider))
			if err != nil {
				return err
			}
			if len(keys) > 0 {
				if err := st.SetSessionsAccount(ctx, o.ID, cliOf(next.Provider), keys, next.ID, next.Funding); err != nil {
					return err
				}
				moved = append(moved, keys...)
			}
		}
		if len(moved) > 0 {
			hours, err := st.CallHours(ctx, o.ID, moved)
			if err != nil {
				return err
			}
			if err := st.Rollup(ctx, o.ID, hours); err != nil {
				return err
			}
		}
		if g, err := st.SignalByKey(ctx, o.ID, ruleUnknownAccount, next.ID); err == nil && g.ResolvedAt == nil {
			g.ResolvedAt, g.ActedAt = &now, &now
			if err := st.UpdateSignal(ctx, g); err != nil {
				return err
			}
		} else if err != nil && !store.IsNotFound(err) {
			return err
		}
		chain, err := orgs.Chain(ctx, st, o, sc)
		if err != nil {
			return err
		}
		action, before := "account.create", any(nil)
		if cur != nil {
			action, before = "account.update", accountAudit(*cur)
		}
		return st.AddAudit(ctx, orgs.Journal(chain, p.User.ID, action, "account", next.ID, before, accountAudit(next), now))
	})
	if err != nil {
		return err
	}
	st := s.db.S()
	x, err := s.loadScope(ctx, st, o, sc)
	if err != nil {
		return err
	}
	start := monthStart(x.now, x.loc)
	views, err := s.accountViews(ctx, st, x, s.currency(ctx, o.Currency, newSpan(start, start.AddDate(0, 1, 0), x.loc)))
	if err != nil {
		return err
	}
	code := http.StatusOK
	if cur == nil {
		code = http.StatusCreated
	}
	for _, v := range views {
		if v.ID == next.ID {
			httpx.WriteJSON(w, code, map[string]any{"account": v})
			return nil
		}
	}
	httpx.WriteJSON(w, code, map[string]any{"account": nil})
	return nil
}

// cliOf is the CLI whose sessions an account of the provider pays for (sessions are kept by CLI, accounts by provider).
func cliOf(provider string) string {
	switch provider {
	case "anthropic":
		return "claude"
	case "openai":
		return "codex"
	}
	return provider
}

func accountAudit(a store.Account) map[string]any {
	return map[string]any{"label": a.Label, "plan": a.Plan, "kind": a.Kind, "funding": a.Funding, "monthlyFeeMicros": a.MonthlyFeeMicros,
		"scopeKind": a.ScopeKind, "scopeId": a.ScopeID, "ownerUserId": a.OwnerUserID, "discovered": a.Discovered}
}
