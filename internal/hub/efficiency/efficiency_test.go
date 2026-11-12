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

// An alert of the old evaluation is stored once: importing again changes nothing, and what the person did with it stays.
func TestImportRegressions(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	raw := `[{"id":"a1","metric":"smoke: 0 из 8","baseline":8,"observed":0,"band":0,"addressee":"Anna@Example.com","diff":["8 × не дождались"],"created_at":"2026-11-02T07:34:42Z"},
		{"id":"a2","metric":"$ на задачу","baseline":0.62,"observed":1,"band":1,"addressee":"anna@example.com","created_at":"2026-11-03T07:00:00Z","state":"resolved","seen_at":"2026-11-04T07:00:00Z"}]`
	if n, err := ImportRegressions(ctx, st, raw); err != nil || n != 2 {
		t.Fatalf("import: %d %v", n, err)
	}
	changed := strings.Replace(raw, `"band":0,`, `"band":0,"state":"resolved",`, 1)
	if n, err := ImportRegressions(ctx, st, changed); err != nil || n != 2 {
		t.Fatalf("again: %d %v", n, err)
	}
	all, err := st.EffRegressionAlertsFor(ctx, "anna@example.com", false)
	if err != nil || len(all) != 2 || all[0].ID != "a2" || all[0].SeenAt == nil || all[1].Diff != `["8 × не дождались"]` {
		t.Fatalf("all: %+v %v", all, err)
	}
	open, err := st.EffRegressionAlertsFor(ctx, "anna@example.com", true)
	if err != nil || len(open) != 1 || open[0].ID != "a1" {
		t.Fatalf("a1 stays as it was stored (open), a2 was stored resolved: %+v %v", open, err)
	}
	for _, bad := range []string{`[{"id":"","metric":"m","created_at":"2026-11-03T07:00:00Z"}]`, `[{"id":"x","metric":"m","created_at":"yesterday"}]`, `[{"id":"x","metric":"m","state":"gone","created_at":"2026-11-03T07:00:00Z"}]`} {
		if _, err := ImportRegressions(ctx, st, bad); err == nil {
			t.Fatalf("must be refused: %s", bad)
		}
	}
}

// Revert commits come from every project of the groups, skill commits from the files named SKILL.md; merges are left out.
func TestGitLabCommitSources(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch p := r.URL.EscapedPath(); {
		case strings.HasSuffix(p, "/groups/g/projects"):
			_, _ = w.Write([]byte(`[{"id":7,"path_with_namespace":"g/p"}]`))
		case strings.HasSuffix(p, "/projects/7/repository/commits"):
			_, _ = w.Write([]byte(`[{"id":"aaa","title":"Revert \"x\"","message":"Revert \"x\"","committed_date":"2026-11-02T10:00:00Z","parent_ids":["z"]},
				{"id":"bbb","title":"plain","message":"plain\n\nThis reverts commit 1234567","committed_date":"2026-11-03T10:00:00Z","parent_ids":["z"]},
				{"id":"ccc","title":"feat","message":"feat","committed_date":"2026-11-04T10:00:00Z","parent_ids":["z"]}]`))
		case strings.HasSuffix(p, "/projects/g%2Fplugins/repository/tree"):
			_, _ = w.Write([]byte(`[{"name":"SKILL.md","path":"a/SKILL.md"},{"name":"README.md","path":"README.md"}]`))
		case strings.HasSuffix(p, "/projects/g%2Fplugins/repository/commits"):
			if r.URL.Query().Get("path") != "a/SKILL.md" {
				t.Errorf("path %q", r.URL.Query().Get("path"))
			}
			_, _ = w.Write([]byte(`[{"id":"s1","title":"skill","message":"skill","committed_date":"2026-11-05T10:00:00Z","parent_ids":["z"]},
				{"id":"s2","title":"Merge","message":"Merge","committed_date":"2026-11-06T10:00:00Z","parent_ids":["z","y"]}]`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	gl := NewGitLab(srv.URL, []string{"g"}, "t")
	gl.SkillsProject = "g/plugins"
	reverts, err := gl.RevertCommits(context.Background(), time.Unix(0, 0))
	if err != nil || len(reverts) != 2 || reverts[0].Project != "g/p" || reverts[1].SHA != "bbb" {
		t.Fatalf("%v %+v", err, reverts)
	}
	skills, err := gl.SkillCommits(context.Background(), time.Unix(0, 0))
	if err != nil || len(skills) != 1 || skills[0].SHA != "s1" {
		t.Fatalf("%v %+v", err, skills)
	}
	st := open(t)
	c := &Collector{Store: st, Code: gl, Now: func() time.Time { return time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC) }}
	c.Once(context.Background())
	for kind, want := range map[string]int{KindRevert: 2, KindSkill: 1} {
		got, err := st.EffFactsBetween(context.Background(), kind, 0, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli())
		if err != nil || len(got) != want {
			t.Fatalf("%s: %v %d", kind, err, len(got))
		}
	}
}

// Comments of the configured project only; issues in progress replace the previous list and carry their last activity.
func TestYouTrackCommentsAndProgress(t *testing.T) {
	var inProgress = `[{"idReadable":"PROJ-1","created":1000},{"idReadable":"PROJ-2","created":2000}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sekret" {
			w.WriteHeader(401)
			return
		}
		switch p := r.URL.Path; {
		case p == "/api/activities":
			_, _ = w.Write([]byte(`[{"timestamp":1762000000000,"author":{"login":"bob"},"added":[{"text":"hello","issue":{"idReadable":"PROJ-9"}},{"text":"x","issue":{"idReadable":"OTHER-1"}}]}]`))
		case p == "/api/issues":
			_, _ = w.Write([]byte(inProgress))
		case strings.HasSuffix(p, "/activities"):
			_, _ = w.Write([]byte(`[{"timestamp":5000,"category":{"id":"CommentsCategory"}},{"timestamp":9000,"category":{"id":"CustomFieldCategory"},"field":{"name":"Priority"}}]`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	yt := NewYouTrack(srv.URL, "sekret", "PROJ")
	if NewYouTrack("", "", "") != nil {
		t.Fatal("unconfigured must be nil")
	}
	list, err := yt.Comments(context.Background(), time.Unix(0, 0), time.Now())
	if err != nil || len(list) != 1 || list[0].Issue != "PROJ-9" || list[0].Author != "bob" {
		t.Fatalf("%v %+v", err, list)
	}
	prog, err := yt.InProgress(context.Background())
	if err != nil || len(prog) != 2 || prog[0].LastActivityAt.UnixMilli() != 5000 {
		t.Fatalf("%v %+v", err, prog)
	}
	st := open(t)
	now := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	c := &Collector{Store: st, Tracker: yt, Now: func() time.Time { return now }}
	if !c.Once(context.Background()) {
		t.Fatal("pass failed")
	}
	inProgress = `[{"idReadable":"PROJ-2","created":2000}]`
	now = now.Add(time.Hour)
	c.Once(context.Background())
	got, err := st.EffFactsBetween(context.Background(), KindProgress, 0, now.UnixMilli())
	if err != nil || len(got) != 1 || got[0].Key != "PROJ-2" {
		t.Fatalf("%v %+v", err, got)
	}
	bad := NewYouTrack(srv.URL, "wrong", "PROJ")
	if _, err := bad.Comments(context.Background(), time.Unix(0, 0), time.Now()); err == nil || strings.Contains(err.Error(), "wrong") {
		t.Fatalf("err %v", err)
	}
}

// Only documents created in the window count, each at its first version; a pass replaces the previous window.
func TestArtifactsFirstPublication(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sekret" {
			w.WriteHeader(401)
			return
		}
		switch p := r.URL.Path; {
		case p == "/api/v1/artifacts":
			_, _ = w.Write([]byte(`{"items":[{"slug":"new","task":"PROJ-1","createdAt":"2026-11-05T10:00:00Z"},{"slug":"old","task":null,"createdAt":"2025-01-01T00:00:00Z"}],"nextOffset":null}`))
		case p == "/api/v1/artifacts/new/versions":
			_, _ = w.Write([]byte(`{"versions":[{"n":2,"createdAt":"2026-11-06T10:00:00Z","author":"later@example.com"},{"n":1,"createdAt":"2026-11-05T10:30:00Z","author":"bob@example.com"}]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	ar := NewArtifacts(srv.URL, "sekret")
	if NewArtifacts("", "") != nil {
		t.Fatal("unconfigured must be nil")
	}
	now := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	list, err := ar.Published(context.Background(), now.Add(-ArtifactsWindow))
	if err != nil || len(list) != 1 || list[0].Slug != "new" || list[0].AuthorEmail != "bob@example.com" || list[0].Task != "PROJ-1" || !list[0].At.Equal(time.Date(2026, 11, 5, 10, 30, 0, 0, time.UTC)) {
		t.Fatalf("%v %+v", err, list)
	}
	st := open(t)
	c := &Collector{Store: st, Documents: ar, Now: func() time.Time { return now }}
	if !c.Once(context.Background()) {
		t.Fatal("pass failed")
	}
	got, err := st.EffFactsBetween(context.Background(), KindArtifact, 0, now.UnixMilli())
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
}
