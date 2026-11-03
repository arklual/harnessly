package analytics

import (
	"cmp"
	"context"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// SeriesPoint is one bucket of a KPI: its first day, its value (nil below minSessions) and its sessions.
type SeriesPoint struct {
	Day   string   `json:"day"`
	Value *float64 `json:"value"`
	N     int      `json:"n"`
}

// SeriesSplit is the points of one team.
type SeriesSplit struct {
	Key    string        `json:"key"`
	Label  string        `json:"label"`
	Points []SeriesPoint `json:"points"`
}

// ChangeMarker is a change on the chart, with the KPI's verdict over the change.
type ChangeMarker struct {
	ChangeID string `json:"changeId"`
	At       string `json:"at"`
	Title    string `json:"title"`
	// Layer is the level the change was made at: org, tenant or team.
	Layer string `json:"layer"`
	// Author is who made the change; null for a detected one or a person who has gone.
	Author *orgs.PersonRef `json:"author"`
	// Icon is the mark to draw: edit (written by a person), rocket (a new version of a tool) or announce (anything else).
	Icon    string  `json:"icon"`
	Verdict *string `json:"verdict"`
}

// Marker icons.
const (
	iconEdit     = "edit"
	iconAnnounce = "announce"
	iconRocket   = "rocket"
)

// iconOf is the mark of a change by its kind: manual ones are edits, a new version of a tool a rocket.
func iconOf(kind string) string {
	switch {
	case kind == store.ChangeManual:
		return iconEdit
	case strings.HasSuffix(kind, "_version"):
		return iconRocket
	}
	return iconAnnounce
}

// KpiSeriesResponse is KpiSeriesResponse of web/app/hub-api/analytics.ts.
type KpiSeriesResponse struct {
	Kpi     string         `json:"kpi"`
	Unit    string         `json:"unit"`
	By      string         `json:"by"`
	Points  []SeriesPoint  `json:"points"`
	Splits  []SeriesSplit  `json:"splits"`
	Markers []ChangeMarker `json:"markers"`
}

// effectSpans is the 7 days before the day of a change and the 7 days from it.
func effectSpans(at time.Time, loc *time.Location) (before, after span) {
	d := at.In(loc)
	d = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
	return newSpan(d.AddDate(0, 0, -effectDays), d, loc), newSpan(d, d.AddDate(0, 0, effectDays), loc)
}

// buckets cuts sp into days or weeks; the last week ends with sp.
func buckets(sp span, by string) []span {
	step := 1
	if by == "week" {
		step = 7
	}
	var out []span
	for a := sp.from; a.Before(sp.to); a = a.AddDate(0, 0, step) {
		b := a.AddDate(0, 0, step)
		if b.After(sp.to) {
			b = sp.to
		}
		out = append(out, newSpan(a, b, sp.loc))
	}
	return out
}

func seriesOf(d kpiDef, ks []kSession, bs []span) []SeriesPoint {
	out := make([]SeriesPoint, len(bs))
	for i, b := range bs {
		in := startedIn(ks, b.from, b.to)
		out[i] = SeriesPoint{Day: b.from.Format(day), Value: value(d, in, b.days()), N: len(in)}
	}
	return out
}

// kpiSeries is one KPI of a scope by day or week, per team on split=team, with the changes of the scope's chain in the
// period as markers. A marker's verdict is the change's effect on this KPI (docs/hub/analytics.md, «Изменения»).
func (s *Service) kpiSeries(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	st := s.db.S()
	x, err := s.orgScope(ctx, st, p, r, authz.View)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(kpis, func(d kpiDef) bool { return d.id == r.PathValue("kpi") })
	if i < 0 {
		return httpx.ErrNotFound
	}
	d := kpis[i]
	q := r.URL.Query()
	by := cmp.Or(q.Get("by"), "day")
	if by != "day" && by != "week" {
		return httpx.Invalid("by", "by — day или week")
	}
	if v := q.Get("split"); v != "" && v != "team" {
		return httpx.Invalid("split", "split — только team")
	}
	sp, err := parsePeriod(q, x.loc, x.now)
	if err != nil {
		return err
	}
	chain, err := orgs.Chain(ctx, st, x.org, x.sc)
	if err != nil {
		return err
	}
	pf, pt := sp.ms()
	cs, err := st.ChangesOn(ctx, x.org.ID, changeScopes(chain), pf, pt)
	if err != nil {
		return err
	}
	// The sessions cover the period and every marker's effect with its noise weeks.
	from, to := pf, pt
	for _, c := range cs {
		before, after := effectSpans(c.At, x.loc)
		a, b := kpiRange(after, before)
		from, to = min(from, a), max(to, b)
	}
	ss, err := st.ScopeSessions(ctx, x.filter(from, to))
	if err != nil {
		return err
	}
	ks, err := kSessionsOf(ctx, st, x.loc, ss, true)
	if err != nil {
		return err
	}
	bs := buckets(sp, by)
	out := KpiSeriesResponse{Kpi: d.id, Unit: d.unit, By: by, Points: seriesOf(d, ks, bs), Markers: []ChangeMarker{}}
	if q.Get("split") == "team" {
		out.Splits = []SeriesSplit{}
		teams := slices.SortedFunc(maps.Values(x.teams), func(a, b store.Team) int {
			return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
		})
		for _, t := range teams {
			var mine []store.AgentSession
			for _, v := range ss {
				if v.TeamID == t.ID {
					mine = append(mine, v)
				}
			}
			tk, err := kSessionsOf(ctx, st, x.loc, mine, true)
			if err != nil {
				return err
			}
			out.Splits = append(out.Splits, SeriesSplit{Key: t.ID, Label: t.Name, Points: seriesOf(d, tk, bs)})
		}
	}
	if out.Markers, err = markersOf(ctx, st, x.loc, x.org.ID, changeScopes(chain), cs, d, ks); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// markersOf are the changes cs on the chart of KPI d, each with the KPI's verdict over it. ks are the sessions the KPI is
// counted from, which must cover every change's effect spans; scopes are those cs were found on, to find the changes
// that overlap a change's weeks.
func markersOf(ctx context.Context, st store.Store, loc *time.Location, orgID string, scopes []store.ChangeScope, cs []store.Change, d kpiDef, ks []kSession) ([]ChangeMarker, error) {
	out := []ChangeMarker{}
	// Overlaps need the changes around the period too.
	var around []store.Change
	if len(cs) > 0 {
		b0, _ := effectSpans(cs[0].At, loc)
		_, a1 := effectSpans(cs[len(cs)-1].At, loc)
		bf, _ := b0.ms()
		_, at := a1.ms()
		var err error
		if around, err = st.ChangesOn(ctx, orgID, scopes, bf, at); err != nil {
			return nil, err
		}
	}
	for _, c := range cs {
		before, after := effectSpans(c.At, loc)
		var v *string
		for _, k := range kpisFrom(ks, after, before) {
			if k.Kpi == d.id {
				verdict := k.Verdict
				v = &verdict
			}
		}
		bf, _ := before.ms()
		_, af := after.ms()
		for _, o := range around {
			if o.ID != c.ID && o.At.UnixMilli() >= bf && o.At.UnixMilli() < af && v != nil && *v != "too_few" {
				mixed := "mixed"
				v = &mixed
			}
		}
		cv, err := changeView(ctx, st, c)
		if err != nil {
			return nil, err
		}
		out = append(out, ChangeMarker{ChangeID: c.ID, At: cv.At, Title: c.Title, Layer: c.ScopeKind, Author: cv.Author,
			Icon: iconOf(c.Kind), Verdict: v})
	}
	return out, nil
}
