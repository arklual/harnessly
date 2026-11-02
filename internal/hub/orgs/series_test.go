package orgs_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/analytics"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

func TestKpiSeries(t *testing.T) {
	v := newVera(t)
	// Five sessions of the team on the 15th, four of them with an MR: completion 0.8 that day.
	ctx := context.Background()
	when := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	ended := when.Add(time.Hour).UnixMilli()
	for i := range 5 {
		mrs := 1
		if i == 4 {
			mrs = 0
		}
		if _, err := v.s.DB.S().PutSession(ctx, store.AgentSession{OrgKey: v.org, Provider: "claude", SessionKey: fmt.Sprintf("s%d", i),
			UserID: v.boris.ID, SpaceID: v.tenant, TeamID: v.team.ID, AccountID: v.key1, Funding: store.FundingOrg, Origin: "live",
			Attribution: "exact", StartedAt: when.UnixMilli(), EndedAt: &ended, Metrics: fmt.Sprintf(`{"turns":3,"mrs":%d}`, mrs), Seq: 1,
			UpdatedAt: ended}); err != nil {
			t.Fatal(err)
		}
	}
	if r := v.anna.Do("POST", v.url("/changes"), map[string]any{"scope": scope("org", v.org), "title": "Sonnet", "at": "2026-10-16"}); r.Code != 201 {
		t.Fatalf("change: %s", r)
	}
	get := func(p *hubtest.Person, q string) analytics.KpiSeriesResponse {
		t.Helper()
		r := p.Do("GET", v.url("/kpis/completion/series"+q), nil)
		if r.Code != http.StatusOK {
			t.Fatalf("series %s: %s", q, r)
		}
		var out analytics.KpiSeriesResponse
		r.JSON(&out)
		return out
	}

	s := get(v.anna, "?from=2026-10-13&to=2026-10-20&split=team")
	if s.Kpi != "completion" || s.Unit != "share" || s.By != "day" || len(s.Points) != 7 {
		t.Fatalf("series = %+v", s)
	}
	for _, pt := range s.Points {
		want := pt.Day == "2026-10-15"
		if got := pt.Value != nil && *pt.Value == 0.8 && pt.N == 5; got != want {
			t.Errorf("point %+v", pt)
		}
	}
	if len(s.Splits) != 1 || s.Splits[0].Label != "Платформа" || s.Splits[0].Points[2].N != 5 {
		t.Errorf("splits = %+v", s.Splits)
	}
	// Nothing after the change: too few to judge.
	if len(s.Markers) != 1 || s.Markers[0].Title != "Sonnet" || s.Markers[0].At != "2026-10-15T21:00:00Z" ||
		s.Markers[0].Verdict == nil || *s.Markers[0].Verdict != "too_few" {
		t.Errorf("markers = %+v", s.Markers)
	}

	w := get(v.boris, "?scope=team:"+v.team.ID+"&from=2026-10-13&to=2026-10-20&by=week")
	if len(w.Points) != 1 || w.Points[0].Day != "2026-10-13" || w.Points[0].N != 5 || w.Splits != nil || len(w.Markers) != 1 {
		t.Errorf("weekly = %+v", w)
	}
	for _, c := range []struct {
		p    *hubtest.Person
		url  string
		code int
	}{
		{v.vera, "/kpis/completion/series", 403},
		{v.anna, "/kpis/nope/series", 404},
		{v.anna, "/kpis/completion/series?by=month", 400},
		{v.anna, "/kpis/completion/series?split=person", 400},
	} {
		if r := c.p.Do("GET", v.url(c.url), nil); r.Code != c.code {
			t.Errorf("%s %s: %s", c.p.Email, c.url, r)
		}
	}
}
