package analytics

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/limits"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// effectDays is the window on each side of a change its effect compares.
const effectDays = 7

// Change is Change of web/app/hub-api/analytics.ts.
type Change struct {
	ID       string          `json:"id"`
	Scope    orgs.ScopeView  `json:"scope"`
	Kind     string          `json:"kind"`
	Title    string          `json:"title"`
	At       string          `json:"at"`
	Author   *orgs.PersonRef `json:"author"`
	Source   string          `json:"source"`
	Machines int             `json:"machines"`
}

// ChangesResponse is the changes of a scope and its ancestors, oldest first.
type ChangesResponse struct {
	Changes []Change `json:"changes"`
}

// Effect is one KPI before and after a change.
type Effect struct {
	Kpi     string         `json:"kpi"`
	Scope   orgs.ScopeView `json:"scope"`
	Before  *float64       `json:"before"`
	After   *float64       `json:"after"`
	Delta   *float64       `json:"delta"`
	Noise   *float64       `json:"noise"`
	NBefore int            `json:"nBefore"`
	NAfter  int            `json:"nAfter"`
	Verdict string         `json:"verdict"`
}

// EffectResponse is the effect of a change on a scope.
type EffectResponse struct {
	Change   Change   `json:"change"`
	Before   Period   `json:"before"`
	After    Period   `json:"after"`
	Overlaps []string `json:"overlaps"`
	Effects  []Effect `json:"effects"`
}

func (s *Service) mountChanges(mux *http.ServeMux) {
	mux.Handle("GET /api/orgs/{org}/changes", s.gate(s.listChanges))
	mux.Handle("POST /api/orgs/{org}/changes", s.gate(s.createChange))
	mux.Handle("GET /api/orgs/{org}/changes/{id}/effect", s.gate(s.changeEffect))
}

func changeScopes(chain []authz.Scope) []store.ChangeScope {
	out := make([]store.ChangeScope, len(chain))
	for i, c := range chain {
		out[i] = store.ChangeScope{Kind: c.Kind, ID: c.ID}
	}
	return out
}

func changeView(ctx context.Context, st store.Store, c store.Change) (Change, error) {
	v := Change{ID: c.ID, Scope: orgs.ScopeView{Kind: c.ScopeKind, ID: c.ScopeID}, Kind: c.Kind, Title: c.Title,
		At: c.At.UTC().Format(time.RFC3339), Source: c.Source, Machines: c.Machines}
	if c.AuthorID != "" {
		u, err := st.UserByID(ctx, c.AuthorID)
		switch {
		case err == nil:
			v.Author = &orgs.PersonRef{UserID: u.ID, Name: u.Name}
			if v.Author.Name == "" {
				v.Author.Name = u.Email
			}
		case !store.IsNotFound(err):
			return v, err
		}
	}
	return v, nil
}

func (s *Service) listChanges(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	x, err := s.orgScope(ctx, st, p, r, authz.View)
	if err != nil {
		return err
	}
	chain, err := orgs.Chain(ctx, st, x.org, x.sc)
	if err != nil {
		return err
	}
	var from, to int64
	if sp, ok, err := parseRange(r.URL.Query(), "from", "to", x.loc); err != nil {
		return err
	} else if ok {
		from, to = sp.ms()
	}
	cs, err := st.ChangesOn(ctx, x.org.ID, changeScopes(chain), from, to)
	if err != nil {
		return err
	}
	out := ChangesResponse{Changes: []Change{}}
	for _, c := range cs {
		v, err := changeView(ctx, st, c)
		if err != nil {
			return err
		}
		out.Changes = append(out.Changes, v)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// changeAt reads an instant (RFC 3339) or a day (its midnight in loc).
func changeAt(v string, loc *time.Location) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, true
	}
	if t, err := time.ParseInLocation(day, v, loc); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func (s *Service) createChange(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	var req struct {
		Scope orgs.ScopeView
		Title string
		At    string
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	st := s.db.S()
	ref := ""
	if req.Scope.Kind != "" || req.Scope.ID != "" {
		ref = req.Scope.Kind + ":" + req.Scope.ID
	}
	o, sc, err := orgs.ScopeIn(ctx, st, p, r.PathValue("org"), ref, authz.Manage)
	if err != nil {
		return err
	}
	title := strings.TrimSpace(req.Title)
	if title == "" || utf8.RuneCountInString(title) > 200 {
		return httpx.Invalid("title", "Название изменения — от 1 до 200 символов")
	}
	loc, err := limits.Location(ctx, st, o.ID)
	if err != nil {
		return err
	}
	at, ok := changeAt(req.At, loc)
	now := s.now()
	if !ok {
		return httpx.Invalid("at", "Дата изменения — YYYY-MM-DD или RFC 3339")
	}
	if at.After(now.Add(24 * time.Hour)) {
		return httpx.Invalid("at", "Изменение не может быть в будущем")
	}
	c := store.Change{OrgKey: o.ID, ScopeKind: sc.Kind, ScopeID: sc.ID, Kind: "manual", Title: title, At: at, AuthorID: p.User.ID,
		Source: store.ChangeManual, CreatedAt: now}
	err = s.db.Tx(ctx, func(st store.Store) error {
		chain, err := orgs.Chain(ctx, st, o, sc)
		if err != nil {
			return err
		}
		if c, err = st.AddChange(ctx, c); err != nil {
			return err
		}
		return st.AddAudit(ctx, orgs.Journal(chain, p.User.ID, "change.create", "change", c.ID, nil,
			map[string]any{"scope": sc.Kind + ":" + sc.ID, "title": c.Title, "at": c.At.UTC().Format(time.RFC3339)}, now))
	})
	if err != nil {
		return err
	}
	v, err := changeView(ctx, st, c)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"change": v})
	return nil
}

// changeEffect compares the KPI of the scope's live sessions in the 7 days before the change's day with the 7 days
// from it. The scope is the change's own by default; another must be the change's scope or under it.
func (s *Service) changeEffect(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	c, err := st.ChangeByID(ctx, r.PathValue("id"))
	if store.IsNotFound(err) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	q := r.URL.Query()
	if q.Get("scope") == "" {
		q.Set("scope", c.ScopeKind+":"+c.ScopeID)
		r.URL.RawQuery = q.Encode()
	}
	x, err := s.orgScope(ctx, st, p, r, authz.View)
	if err != nil {
		return err
	}
	if c.OrgKey != x.org.ID {
		return httpx.ErrNotFound
	}
	chain, err := orgs.Chain(ctx, st, x.org, x.sc)
	if err != nil {
		return err
	}
	touches := false
	for _, a := range chain {
		touches = touches || (a.Kind == c.ScopeKind && a.ID == c.ScopeID)
	}
	if !touches {
		return httpx.ErrNotFound
	}
	before, after := effectSpans(c.At, x.loc)
	from, to := kpiRange(after, before)
	ss, err := st.ScopeSessions(ctx, x.filter(from, to))
	if err != nil {
		return err
	}
	kv, err := kpisOf(ctx, st, x.loc, ss, after, before, true)
	if err != nil {
		return err
	}
	cv, err := changeView(ctx, st, c)
	if err != nil {
		return err
	}
	out := EffectResponse{Change: cv, Before: before.period(), After: after.period(), Overlaps: []string{}, Effects: []Effect{}}
	bf, _ := before.ms()
	_, af := after.ms()
	others, err := st.ChangesOn(ctx, x.org.ID, changeScopes(chain), bf, af)
	if err != nil {
		return err
	}
	for _, o := range others {
		if o.ID != c.ID {
			out.Overlaps = append(out.Overlaps, o.ID)
		}
	}
	scope := orgs.ScopeView{Kind: x.sc.Kind, ID: x.sc.ID}
	for _, k := range kv {
		e := Effect{Kpi: k.Kpi, Scope: scope, Before: k.CmpValue, After: k.Value, Delta: k.Delta, Noise: k.Noise, NBefore: k.CmpN,
			NAfter: k.N, Verdict: k.Verdict}
		if len(out.Overlaps) > 0 && e.Verdict != "too_few" {
			e.Verdict = "mixed"
		}
		out.Effects = append(out.Effects, e)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}
