package efficiency

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/features"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/switches"
	"github.com/ly-solutions/harnessly/internal/hub/tenant"
)

func open(t *testing.T) store.Store {
	t.Helper()
	db, err := store.Open(context.Background(), "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db.S()
}

type fakeHost struct{ list []MR }

func (f fakeHost) MergedMRs(context.Context, time.Time) ([]MR, error) { return f.list, nil }

func mr(iid int, title, merged string) MR {
	at, _ := time.Parse(time.RFC3339, merged)
	return MR{Project: "g/p", IID: iid, Title: title, Author: "bob", CreatedAt: at.Add(-time.Hour), MergedAt: at}
}

// Every merged request counts, whoever created it; reverts stay out of the denominator.
func TestComputeCountsAllMergedAndDividesCost(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	c := &Collector{Store: st, Code: fakeHost{[]MR{
		mr(1, "feat: a", "2026-11-08T10:00:00Z"),
		mr(2, "feat: b", "2026-11-08T11:00:00Z"),
		mr(3, "feat: c", "2026-11-08T12:00:00Z"),
		mr(4, `Revert "feat: c"`, "2026-11-08T13:00:00Z"),
		mr(5, "feat: d", "2026-11-09T09:00:00Z"),
	}}, Now: func() time.Time { return now }}
	if !c.Once(ctx) {
		t.Fatal("pass failed")
	}
	cost := func(_ context.Context, day string, _ *time.Location) (float64, bool) {
		return map[string]float64{"2026-11-08": 30, "2026-11-09": 5}[day], true
	}
	from := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	got, err := Compute(ctx, st, from, from.AddDate(0, 0, 2), time.UTC, cost)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Days) != 2 || got.Days[0].MRs != 3 || got.Days[0].Reverts != 1 || got.Days[1].MRs != 1 {
		t.Fatalf("days: %+v", got.Days)
	}
	if v := got.Days[0].USDPerMR; v == nil || *v != 10 {
		t.Fatalf("usd per mr: %v", v)
	}
	if got.Totals.MRs != 4 || got.Totals.USDPerMR == nil || *got.Totals.USDPerMR != 35.0/4 {
		t.Fatalf("totals: %+v", got.Totals)
	}
	if len(got.Sources) != 1 || got.Sources[0].Source != CollectorMRs || !got.Sources[0].OK || got.Sources[0].Facts != 5 {
		t.Fatalf("sources: %+v", got.Sources)
	}
}

// Without a cost the money stays null, never zero.
func TestComputeNoCostIsNull(t *testing.T) {
	st := open(t)
	from := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	got, err := Compute(context.Background(), st, from, from.AddDate(0, 0, 1), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Totals.CostUSD != nil || got.Totals.USDPerMR != nil || got.Days[0].USDPerMR != nil {
		t.Fatalf("%+v", got)
	}
}

func TestUnconfiguredSourceIsRecorded(t *testing.T) {
	st := open(t)
	c := &Collector{Store: st}
	c.Once(context.Background())
	s, _ := st.EffSourceStates(context.Background())
	if len(s) != 1 || s[0].OK || s[0].Error != "not configured" {
		t.Fatalf("%+v", s)
	}
}

func TestGitLabPagesAndHidesToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "sekret" {
			w.WriteHeader(401)
			return
		}
		item := func(iid int) string {
			return fmt.Sprintf(`{"iid":%d,"title":"T","source_branch":"b","created_at":"2026-11-01T00:00:00Z","merged_at":"2026-11-02T00:00:00Z","user_notes_count":1,"author":{"username":"bot"},"merged_by":{"username":"bob"},"references":{"full":"g/p!%d"}}`, iid, iid)
		}
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("X-Next-Page", "2")
			_, _ = w.Write([]byte("[" + item(1) + "]"))
			return
		}
		_, _ = w.Write([]byte("[" + item(2) + "]"))
	}))
	defer srv.Close()
	gl := NewGitLab(srv.URL, []string{"g"}, "sekret")
	list, err := gl.MergedMRs(context.Background(), time.Unix(0, 0))
	if err != nil || len(list) != 2 || list[1].Key() != "g/p!2" || list[1].MergedBy != "bob" || !list[1].ForeignNotes {
		t.Fatalf("%v %+v", err, list)
	}
	bad := NewGitLab(srv.URL, []string{"g"}, "wrong")
	if _, err := bad.MergedMRs(context.Background(), time.Unix(0, 0)); err == nil || strings.Contains(err.Error(), "wrong") {
		t.Fatalf("err %v", err)
	}
	if NewGitLab("", nil, "") != nil {
		t.Fatal("unconfigured must be nil")
	}
}

func TestCollectorKeepsOverlapAndUpserts(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	host := fakeHost{[]MR{mr(1, "a", "2026-11-08T10:00:00Z")}}
	c := &Collector{Store: st, Code: host, Now: func() time.Time { return now }}
	c.Once(ctx)
	host.list[0].Title = "renamed"
	c.Once(ctx)
	f, _ := st.EffFactsBetween(ctx, KindMR, 0, now.UnixMilli())
	if len(f) != 1 || !strings.Contains(f[0].Data, "renamed") {
		t.Fatalf("%+v", f)
	}
}

// The money of a day is the list price of the organization's calls in usage_rollup_hour; a day with no calls stays null.
func TestRollupCostFeedsSummary(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := db.S()
	h := func(s string) int64 { x, _ := time.Parse(time.RFC3339, s); return x.UnixMilli() }
	for _, r := range []struct {
		org, hour string
		calls     int
		micros    int64
	}{
		{"o1", "2026-11-08T10:00:00Z", 3, 20_000_000},
		{"o1", "2026-11-08T15:00:00Z", 1, 10_000_000},
		{"o2", "2026-11-08T10:00:00Z", 9, 99_000_000},
	} {
		if _, err := db.Exec(ctx, `INSERT INTO usage_rollup_hour (org_key, hour, calls, list_cost_micros) VALUES (?, ?, ?, ?)`,
			r.org, h(r.hour), r.calls, r.micros); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	c := &Collector{Store: st, Code: fakeHost{[]MR{
		mr(1, "feat: a", "2026-11-08T10:00:00Z"),
		mr(2, "feat: b", "2026-11-08T11:00:00Z"),
		mr(3, "feat: c", "2026-11-09T09:00:00Z"),
	}}, Now: func() time.Time { return now }}
	if !c.Once(ctx) {
		t.Fatal("pass failed")
	}
	from := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 2)
	cost, err := RollupCost(ctx, st, "o1", from, to, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Compute(ctx, st, from, to, time.UTC, cost)
	if err != nil {
		t.Fatal(err)
	}
	if got.Days[0].CostUSD == nil || *got.Days[0].CostUSD != 30 || got.Days[0].USDPerMR == nil || *got.Days[0].USDPerMR != 15 {
		t.Fatalf("day 1: %+v", got.Days[0])
	}
	if got.Days[1].CostUSD != nil || got.Days[1].USDPerMR != nil {
		t.Fatalf("a day without calls must stay null: %+v", got.Days[1])
	}
	if got.Totals.CostUSD == nil || *got.Totals.CostUSD != 30 || got.Totals.USDPerMR == nil || *got.Totals.USDPerMR != 10 {
		t.Fatalf("totals: %+v", got.Totals)
	}
}

// The overview takes the spend and calls from the rollup, the shares from the sessions' metrics, and leaves what no scanner
// measures null; «me» sees only the caller's own sessions.
func TestOverviewAndMe(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	day := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	put := func(key, user, origin, metrics string) {
		if _, err := st.PutSession(ctx, store.AgentSession{OrgKey: "o1", Provider: "claude", SessionKey: key, UserID: user, Origin: origin,
			StartedAt: day.UnixMilli(), UpdatedAt: day.UnixMilli() + 1000, Metrics: metrics}); err != nil {
			t.Fatal(err)
		}
	}
	put("a", "u1", "live", `{"mrs":2,"firstCtx":20000,"carryTokens":50,"inputTokens":10,"cacheReadTokens":80,"cacheWriteTokens":10,"toolCalls":10,"toolErrors":1,"waits":2,"waitP90Ms":120000,"waitMs":240000}`)
	put("b", "u2", "eval", `{"mrs":1,"firstCtx":40000}`)
	from := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 3)

	team, err := ComputeOverview(ctx, st, "o1", "", from, to, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(team.Days) != 3 || team.Days[1].Day != "2026-11-09" || team.Days[1].Sessions != 2 {
		t.Fatalf("days: %+v", team.Days)
	}
	d := team.Days[1]
	if d.FirstCtxP50 == nil || *d.FirstCtxP50 != 30000 || d.CarryShare == nil || *d.CarryShare != 0.5 ||
		d.ToolErrorRate == nil || *d.ToolErrorRate != 0.1 || d.HumanReplyP90Min == nil || *d.HumanReplyP90Min != 2 {
		t.Fatalf("shares: %+v", d)
	}
	if d.AvgCtx != nil || team.Days[0].FirstCtxP50 != nil || team.Days[0].ToolErrorRate != nil {
		t.Fatalf("a figure nobody measured must be null: %+v", team.Days[0])
	}
	if len(team.Channels) != 2 || team.Channels[0].Channel != "tab" || team.Channels[1].Channel != "eval" || team.Totals.Sessions != 2 {
		t.Fatalf("channels: %+v", team.Channels)
	}
	if team.ScannedAt == nil {
		t.Fatal("scannedAt")
	}

	mine, err := ComputeOverview(ctx, st, "o1", "u1", from, to, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if mine.Days[1].Sessions != 1 || mine.Days[1].MRs != 2 || mine.Totals.MRs != 2 {
		t.Fatalf("mine: %+v", mine.Days[1])
	}

	me, err := ComputeMe(ctx, st, "o1", "u1", "Ann", from, to, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if me.Person != "Ann" || len(me.Sessions) != 1 || me.Sessions[0].FileID != "a" || me.Sessions[0].MRs != 2 {
		t.Fatalf("me: %+v", me)
	}
	if me.Attention.ReplyP50Min == nil || *me.Attention.ReplyP50Min != 2 || me.Attention.AskWaitP90Min != nil {
		t.Fatalf("attention: %+v", me.Attention)
	}
}

// A marker is a fact of its own source: the overview draws those dated in the period, and importing the same list again
// changes nothing.
func TestOverviewMarkers(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)
	raw := `[{"key":"k1","at":"2026-11-09T09:00:00Z","kind":"model","title":"Модель A → B","detail":"основная"},
		{"at":"2026-11-09T10:00:00Z","kind":"rule","title":"Правило"}]`
	for range 2 {
		if n, err := ImportMarkers(ctx, st, raw, now); err != nil || n != 2 {
			t.Fatalf("import: %d %v", n, err)
		}
	}
	if _, err := ImportMarkers(ctx, st, `[{"at":"2026-11-09T09:00:00Z","kind":"nonsense","title":"x"}]`, now); err == nil {
		t.Fatal("an unknown kind must be refused")
	}
	from := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	got, err := ComputeOverview(ctx, st, "o1", "", from, from.AddDate(0, 0, 3), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Markers) != 2 || got.Markers[0].Kind != "model" || got.Markers[0].Detail == nil || got.Markers[0].At != "2026-11-09T09:00:00Z" || got.Markers[1].Detail != nil {
		t.Fatalf("markers: %+v", got.Markers)
	}
}

// The overview and the personal tab answer only to a person whose rollout switch is on, and only in an organization with the flag.
func TestOverviewNeedsTheSwitch(t *testing.T) {
	o := &store.Org{ID: "o1"}
	who := &tenant.Ctx{Org: o, Person: &auth.Principal{User: store.User{Email: "R@Example.com"}}}
	r := httptest.NewRequest("GET", "/x", nil)
	for _, tc := range []struct {
		name string
		s    *Service
		ok   bool
	}{
		{"all on", &Service{Features: features.AllOn{}, Switches: switches.Static{SwitchOverview: true}}, true},
		{"switch off", &Service{Features: features.AllOn{}, Switches: switches.Static{}}, false},
		{"no switches", &Service{Features: features.AllOn{}}, false},
		{"flag off", &Service{Features: features.Static{}, Switches: switches.Static{SwitchOverview: true}}, false},
	} {
		if err := tc.s.gate(r, who); (err == nil) != tc.ok {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}
