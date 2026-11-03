package orgs_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/analytics"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// atDay puts a session of the team with one call of the given dollars on the organization's account.
func (v veraCase) atDay(t *testing.T, key, user string, started, ended, updated time.Time, metrics string, dollars float64) {
	t.Helper()
	ctx := context.Background()
	st := v.s.DB.S()
	var end *int64
	if !ended.IsZero() {
		e := ended.UnixMilli()
		end = &e
	}
	if _, err := st.PutSession(ctx, store.AgentSession{OrgKey: v.org, Provider: "claude", SessionKey: key, UserID: user, SpaceID: v.tenant,
		TeamID: v.team.ID, AccountID: v.key1, Funding: store.FundingOrg, Origin: "live", Attribution: "exact", StartedAt: started.UnixMilli(),
		EndedAt: end, Metrics: metrics, Seq: 1, UpdatedAt: updated.UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	if dollars == 0 {
		return
	}
	list := int64(dollars * 1e6)
	if _, err := st.AddCall(ctx, store.UsageCall{OrgKey: v.org, Provider: "claude", MsgKey: key + "m", SessionKey: key, Ts: started.UnixMilli(),
		Model: "claude-sonnet-4-6", Input: 100, ListCostMicros: &list, ReceivedAt: started.UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	if err := st.Rollup(ctx, v.org, []int64{store.HourMs(started.UnixMilli())}); err != nil {
		t.Fatal(err)
	}
}

// today puts a closed task of boris and a running chat of vera on 31 October (the clock stands at 16:00 UTC, 19:00 in
// Moscow, where the day began at 21:00 UTC the day before).
func (v veraCase) today(t *testing.T) {
	t.Helper()
	d := func(h, m int) time.Time { return time.Date(2026, 10, 31, h, m, 0, 0, time.UTC) }
	v.atDay(t, "closed", v.boris.ID, d(8, 0), d(9, 0), d(9, 0), `{"turns":4,"mrs":1,"mrsMerged":1}`, 12)
	v.atDay(t, "running", v.vera.ID, d(15, 50), time.Time{}, d(15, 58), `{"turns":2}`, 3)
}

func TestOverviewToday(t *testing.T) {
	v := newVera(t)
	v.paced(t)
	v.today(t)
	var o analytics.OverviewResponse
	if r := v.anna.Do("GET", v.url("/overview"), nil); r.Code != http.StatusOK {
		t.Fatalf("overview: %s", r)
	} else {
		r.JSON(&o)
	}
	td := o.Today
	if td.Day != "2026-10-31" || td.Spend.Paid.USD != 15 || td.Spend.List.USD != 15 || td.Spend.Tokens != 200 {
		t.Errorf("today = %+v", td)
	}
	// Two of the four people of the organization had a chat today; one task is closed, one chat goes on.
	if td.Active.People != 2 || td.Active.Of != 4 || td.Tasks.Closed != 1 || td.Tasks.InWork != 1 {
		t.Errorf("active %+v tasks %+v", td.Active, td.Tasks)
	}
	if len(td.Sparks.Spend) != 14 || td.Sparks.Spend[13].Day != "2026-10-31" || td.Sparks.Spend[0].Day != "2026-10-18" {
		t.Fatalf("sparks = %+v", td.Sparks.Spend)
	}
	// 30 October: boris's $70 and his chat; the session without «mrs» is no closed task.
	if td.Sparks.Spend[13].Value != 15 || td.Sparks.Spend[12].Value != 70 || td.Sparks.Active[12].Value != 1 || td.Sparks.Active[13].Value != 2 ||
		td.Sparks.Tasks[12].Value != 0 || td.Sparks.Tasks[13].Value != 1 {
		t.Errorf("sparks = %+v", td.Sparks)
	}
	// The totals of the breakdown are the base of its shares, in every unit.
	if tot := o.SpendBy.Totals; tot.Paid.USD != 1275 || tot.List.USD != 1275 || tot.Tokens != o.SpendBy.Rows[0].Tokens.Total || o.SpendBy.Rows[0].Tokens.Total != 500 {
		t.Errorf("totals = %+v rows %+v", o.SpendBy.Totals, o.SpendBy.Rows)
	}

	// Efficiency: one task closed in October by the two people of the team. Of the five chats only one is long enough to
	// count, and it made a merge request.
	if len(o.Efficiency.Teams) != 1 {
		t.Fatalf("efficiency = %+v", o.Efficiency)
	}
	e := o.Efficiency.Teams[0]
	if e.Label != "Платформа" || e.People != 2 || e.Closed != 1 || e.PerPersonMonth == nil || *e.PerPersonMonth != 0.5 {
		t.Errorf("team = %+v", e)
	}
	if e.Reached.N != 5 || e.Reached.Value == nil || *e.Reached.Value != 1 || o.Efficiency.Reached.N != 5 || o.Efficiency.Reached.Value == nil || *o.Efficiency.Reached.Value != 1 {
		t.Errorf("reached = %+v %+v", e.Reached, o.Efficiency.Reached)
	}

	// A lead sees the figures of his team only.
	var l analytics.OverviewResponse
	v.boris.Do("GET", v.url("/overview?scope=team:"+v.team.ID), nil).JSON(&l)
	if l.Today.Active.Of != 2 || l.Today.Active.People != 2 || l.Today.Tasks.Closed != 1 {
		t.Errorf("lead's today = %+v", l.Today)
	}
}

func TestPeopleOfATeam(t *testing.T) {
	v := newVera(t)
	v.paced(t)
	v.atDay(t, "week", v.boris.ID, time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC), time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC), `{"mrs":2,"mrsMerged":1}`, 0)
	v.atDay(t, "now", v.boris.ID, time.Date(2026, 10, 31, 15, 50, 0, 0, time.UTC), time.Time{}, time.Date(2026, 10, 31, 15, 58, 0, 0, time.UTC), `{}`, 0)

	var p analytics.PeopleResponse
	r := v.boris.Do("GET", v.url("/people?scope=team:"+v.team.ID), nil)
	if r.Code != http.StatusOK {
		t.Fatalf("people: %s", r)
	}
	r.JSON(&p)
	if p.Scope.Kind != "team" || p.Period.From != "2026-10-01" || p.Week.From != "2026-10-25" || p.Week.To != "2026-11-01" {
		t.Errorf("scope %+v period %+v week %+v", p.Scope, p.Period, p.Week)
	}
	// $1260 of the team's $1500: 84%, a warning.
	if p.Total.Paid.USD != 1260 || p.Total.Limit == nil || p.Total.Limit.Pct != 0.84 || p.Total.Tone == nil || *p.Total.Tone != "warn" {
		t.Errorf("total = %+v", p.Total)
	}
	if len(p.People) != 2 {
		t.Fatalf("people = %+v", p.People)
	}
	b, vr := p.People[0], p.People[1]
	if b.UserID != v.boris.ID || b.Paid.USD != 1176 || b.Share == nil || *b.Share != 0.784 || b.Limit != nil || b.Hot {
		t.Errorf("boris = %+v", b)
	}
	if b.Chats.N != 3 || b.Chats.Running != 1 || b.MRs.N != 2 || b.MRs.Merged != 3 {
		t.Errorf("boris's week = %+v %+v", b.Chats, b.MRs)
	}
	// vera: $84 of her own $100: hot.
	if vr.UserID != v.vera.ID || vr.Paid.USD != 84 || vr.Limit == nil || vr.Limit.RuleID != v.veraRule || vr.Limit.Pct != 0.84 || !vr.Hot ||
		vr.Share == nil || *vr.Share != 0.056 || vr.Chats.N != 0 || vr.Name == "" {
		t.Errorf("vera = %+v", vr)
	}

	// The organization's administrator reads the same for the team; a member does not read it.
	if r := v.anna.Do("GET", v.url("/people?scope=team:"+v.team.ID), nil); r.Code != http.StatusOK {
		t.Errorf("anna: %s", r)
	}
	for _, c := range []struct {
		get func(string) int
		url string
	}{
		{func(u string) int { return v.vera.Do("GET", u, nil).Code }, "/people?scope=team:" + v.team.ID},
		{func(u string) int { return v.gleb.Do("GET", u, nil).Code }, "/people"},
		{func(u string) int { return v.boris.Do("GET", u, nil).Code }, "/people"},
	} {
		if code := c.get(v.url(c.url)); code/100 != 4 {
			t.Errorf("%s: %d", c.url, code)
		}
	}
}

func TestMyWeek(t *testing.T) {
	v := newVera(t)
	v.paced(t)
	v.atDay(t, "now", v.boris.ID, time.Date(2026, 10, 31, 15, 50, 0, 0, time.UTC), time.Time{}, time.Date(2026, 10, 31, 15, 58, 0, 0, time.UTC), `{"mrs":1}`, 0)
	v.atDay(t, "old", v.boris.ID, time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC), time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC), `{"mrs":5,"mrsMerged":5}`, 0)

	var w analytics.MeWeek
	r := v.boris.Do("GET", "/api/me/week", nil)
	if r.Code != http.StatusOK {
		t.Fatalf("week: %s", r)
	}
	r.JSON(&w)
	// The paced chat and the running one; the one of the 20th is out of the week.
	if w.Week.From != "2026-10-25" || w.Week.To != "2026-11-01" || w.Chats.N != 2 || w.Chats.Running != 1 || w.MRs.N != 1 || w.MRs.Merged != 2 {
		t.Errorf("week = %+v", w)
	}
	var none analytics.MeWeek
	v.gleb.Do("GET", "/api/me/week", nil).JSON(&none)
	if none.Chats.N != 0 || none.MRs.N != 0 {
		t.Errorf("gleb's week = %+v", none)
	}
}

func TestMySeriesAndMarkers(t *testing.T) {
	v := newVera(t)
	d := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	for i, key := range []string{"a", "b", "c", "d", "e"} {
		mrs := 1
		if i == 4 {
			mrs = 0
		}
		v.atDay(t, key, v.boris.ID, d, d.Add(time.Hour), d.Add(time.Hour), `{"turns":3,"mrs":`+string(rune('0'+mrs))+`}`, 0)
	}
	// A change of the organization written by anna, one of the team by boris, and a new version of a tool.
	if r := v.anna.Do("POST", v.url("/changes"), map[string]any{"scope": scope("org", v.org), "title": "Sonnet", "at": "2026-10-16"}); r.Code != 201 {
		t.Fatalf("change: %s", r)
	}
	if r := v.boris.Do("POST", v.url("/changes"), map[string]any{"scope": scope("team", v.team.ID), "title": "Review rules", "at": "2026-10-17"}); r.Code != 201 {
		t.Fatalf("team change: %s", r)
	}
	if _, err := v.s.DB.S().AddChange(context.Background(), store.Change{OrgKey: v.org, ScopeKind: store.ScopeOrg, ScopeID: v.org, Kind: "claude_version",
		Title: "Claude 2.9", At: time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC), Source: store.ChangeDetected, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	get := func(url string) analytics.KpiSeriesResponse {
		t.Helper()
		r := v.boris.Do("GET", url, nil)
		if r.Code != http.StatusOK {
			t.Fatalf("%s: %s", url, r)
		}
		var out analytics.KpiSeriesResponse
		r.JSON(&out)
		return out
	}
	s := get("/api/me/kpis/completion/series?org=" + v.org + "&from=2026-10-13&to=2026-10-20")
	if s.Kpi != "completion" || s.Unit != "share" || s.By != "day" || len(s.Points) != 7 || s.Splits != nil {
		t.Fatalf("series = %+v", s)
	}
	if p := s.Points[2]; p.Day != "2026-10-15" || p.N != 5 || p.Value == nil || *p.Value != 0.8 {
		t.Errorf("point = %+v", p)
	}
	if len(s.Markers) != 3 {
		t.Fatalf("markers = %+v", s.Markers)
	}
	m0, m1, m2 := s.Markers[0], s.Markers[1], s.Markers[2]
	if m0.Title != "Sonnet" || m0.Layer != "org" || m0.Icon != "edit" || m0.Author == nil || m0.Author.UserID != v.anna.ID {
		t.Errorf("org marker = %+v", m0)
	}
	if m1.Title != "Review rules" || m1.Layer != "team" || m1.Icon != "edit" || m1.Author == nil || m1.Author.UserID != v.boris.ID {
		t.Errorf("team marker = %+v", m1)
	}
	if m2.Title != "Claude 2.9" || m2.Layer != "org" || m2.Icon != "rocket" || m2.Author != nil {
		t.Errorf("detected marker = %+v", m2)
	}
	// vera has no sessions of her own, so she sees only the organization's changes, and her series has no values.
	var vs analytics.KpiSeriesResponse
	v.vera.Do("GET", "/api/me/kpis/completion/series?org="+v.org+"&from=2026-10-13&to=2026-10-20&by=week", nil).JSON(&vs)
	if len(vs.Points) != 1 || vs.Points[0].Value != nil || vs.Points[0].N != 0 || len(vs.Markers) != 2 {
		t.Errorf("vera's series = %+v", vs)
	}
	// Without an organization there is no place for a change.
	if n := get("/api/me/kpis/completion/series?from=2026-10-13&to=2026-10-20"); len(n.Markers) != 0 || n.Markers == nil {
		t.Errorf("markers without an org = %+v", n.Markers)
	}
	for _, c := range []struct {
		url  string
		code int
	}{{"/api/me/kpis/nope/series", 404}, {"/api/me/kpis/completion/series?by=month", 400}} {
		if r := v.boris.Do("GET", c.url, nil); r.Code != c.code {
			t.Errorf("%s: %s", c.url, r)
		}
	}
}

// analyticsFixture reads a web fixture of the analytics contracts (web/app/hub-api/fixtures/analytics).
func analyticsFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "app", "hub-api", "fixtures", "analytics", name))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// keysMatch fails the test where the answer's objects have other keys than the fixture's, at any depth under at. Of
// a list the first elements are compared; a null on either side is not (a nullable field has no shape to compare).
func keysMatch(t *testing.T, at string, fx, got any) {
	t.Helper()
	switch f := fx.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			if got != nil {
				t.Errorf("%s: fixture is an object, answer %T", at, got)
			}
			return
		}
		if keys(f) != keys(g) {
			t.Errorf("%s keys: fixture %s, answer %s", at, keys(f), keys(g))
			return
		}
		for k := range f {
			keysMatch(t, at+"."+k, f[k], g[k])
		}
	case []any:
		g, ok := got.([]any)
		if ok && len(f) > 0 && len(g) > 0 {
			keysMatch(t, at+"[0]", f[0], g[0])
		}
	}
}

// sub is the value at a path of keys.
func sub(m map[string]any, path ...string) any {
	var v any = m
	for _, k := range path {
		o, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = o[k]
	}
	return v
}

func answer(t *testing.T, r interface {
	JSON(any)
}) map[string]any {
	t.Helper()
	var out map[string]any
	r.JSON(&out)
	return out
}

func TestTodayAnswersHaveTheFixturesShape(t *testing.T) {
	v := newVera(t)
	v.paced(t)
	v.today(t)
	v.atDay(t, "week", v.boris.ID, time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC), time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC), `{"mrs":2,"mrsMerged":1}`, 0)

	if r := v.anna.Do("POST", v.url("/changes"), map[string]any{"scope": scope("org", v.org), "title": "Sonnet", "at": "2026-10-16"}); r.Code != 201 {
		t.Fatalf("change: %s", r)
	}
	for _, c := range []struct {
		fixture string
		get     func() map[string]any
		paths   [][]string
	}{
		{"overview-admin.json", func() map[string]any { return answer(t, v.anna.Do("GET", v.url("/overview"), nil)) },
			[][]string{{"today"}, {"efficiency"}, {"spendBy", "totals"}, {"spendBy", "rows"}}},
		{"overview-lead.json", func() map[string]any {
			return answer(t, v.boris.Do("GET", v.url("/overview?scope=team:"+v.team.ID), nil))
		}, [][]string{{"today"}, {"efficiency"}, {"spendBy", "totals"}, {"spendBy", "rows"}}},
		{"people-lead.json", func() map[string]any {
			return answer(t, v.boris.Do("GET", v.url("/people?scope=team:"+v.team.ID), nil))
		}, [][]string{{}}},
		{"kpi-series.json", func() map[string]any { return answer(t, v.anna.Do("GET", v.url("/kpis/completion/series"), nil)) },
			[][]string{{"markers"}}},
		{"me-week-member.json", func() map[string]any { return answer(t, v.boris.Do("GET", "/api/me/week", nil)) }, [][]string{{}}},
		{"me-kpi-series-member.json", func() map[string]any {
			return answer(t, v.boris.Do("GET", "/api/me/kpis/completion/series?org="+v.org+"&by=week", nil))
		}, [][]string{{}}},
	} {
		fx, got := analyticsFixture(t, c.fixture), c.get()
		for _, p := range c.paths {
			keysMatch(t, c.fixture+"/"+strings.Join(p, "."), sub(fx, p...), sub(got, p...))
		}
	}
}
