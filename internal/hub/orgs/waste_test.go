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

func TestOrgWaste(t *testing.T) {
	v := newVera(t)
	ctx := context.Background()
	st := v.s.DB.S()
	// The base case has $1190 of the organization's list money in October (metrics {}), so nothing of it is wasted.
	n := 0
	put := func(user, funding string, ended bool, dollars float64, metrics string) {
		t.Helper()
		n++
		key := "w" + string(rune('a'+n))
		when := time.Date(2026, 10, 20, 9+n, 0, 0, 0, time.UTC)
		x := store.AgentSession{OrgKey: v.org, Provider: "claude", SessionKey: key, UserID: user, SpaceID: v.tenant, TeamID: v.team.ID,
			Funding: funding, Origin: "live", Attribution: "exact", StartedAt: when.UnixMilli(), Metrics: metrics, Seq: 1, UpdatedAt: when.UnixMilli()}
		if funding == store.FundingOrg {
			x.AccountID = v.key1
		}
		if ended {
			e := when.Add(time.Hour).UnixMilli()
			x.EndedAt, x.UpdatedAt = &e, e
		}
		if _, err := st.PutSession(ctx, x); err != nil {
			t.Fatal(err)
		}
		list := int64(dollars * 1e6)
		if _, err := st.AddCall(ctx, store.UsageCall{OrgKey: v.org, Provider: "claude", MsgKey: key + "m", SessionKey: key, Ts: when.UnixMilli(),
			Model: "claude-sonnet-4-6", Input: 100, ListCostMicros: &list, ReceivedAt: when.UnixMilli()}); err != nil {
			t.Fatal(err)
		}
	}
	// $100: 30% of the input is carried, 2 of 10 tool calls fail, ended after 5 turns with no MR.
	put(v.boris.ID, store.FundingOrg, true, 100, `{"turns":5,"mrs":0,"carryTokens":300,"inputTokens":100,"cacheReadTokens":600,"cacheWriteTokens":300,"toolCalls":10,"toolErrors":2,"firstCtx":20,"skillsListed":10,"skillsLoaded":5}`)
	// $50: more carried than read (capped at all of it), 1 of 4 tool calls fails, but it made an MR.
	put(v.vera.ID, store.FundingOrg, true, 50, `{"turns":4,"mrs":1,"carryTokens":1500,"inputTokens":1000,"toolCalls":4,"toolErrors":1}`)
	// $40 still running with no MR: not abandoned, and nothing else to count.
	put(v.boris.ID, store.FundingOrg, false, 40, `{"turns":10,"mrs":0}`)
	// A long call of the running session: $10 of the $40 are spent at a context of 300 000 tokens, which makes it $50.
	big := int64(10e6)
	if _, err := st.AddCall(ctx, store.UsageCall{OrgKey: v.org, Provider: "claude", MsgKey: "wdbig", SessionKey: "wd", Ts: time.Date(2026, 10, 20, 12, 30, 0, 0, time.UTC).UnixMilli(),
		Model: "claude-sonnet-4-6", Input: 200000, CacheRead: 100000, ListCostMicros: &big, ReceivedAt: 1}); err != nil {
		t.Fatal(err)
	}
	// vera's own account: not the organization's money, in no total and no item.
	put(v.vera.ID, store.FundingPersonal, true, 500, `{"turns":9,"mrs":0,"carryTokens":900,"inputTokens":1000,"toolCalls":2,"toolErrors":2}`)

	r := v.anna.Do("GET", v.url("/waste"), nil)
	if r.Code != http.StatusOK {
		t.Fatalf("waste: %s", r)
	}
	var w analytics.WasteResponse
	r.JSON(&w)
	if w.Scope.Kind != "org" || w.Scope.ID != v.org || w.Period.From != "2026-10-01" || w.Period.To != "2026-11-01" || w.Currency.Source != "none" {
		t.Errorf("scope %+v period %+v currency %+v", w.Scope, w.Period, w.Currency)
	}
	// The organization's list spend is 1190 + 100 + 50 + 50 = 1390.
	want := []struct {
		kind     analytics.WasteKind
		usd      float64
		share    float64
		sessions int
	}{
		{analytics.WasteAbandonedSessions, 100, 0.072, 1},
		{analytics.WasteRetriesAfterError, 32.5, 0.023, 2},
		{analytics.WasteLongContext, 10, 0.007, 1},
		{analytics.WasteExtraSkills, 5, 0.004, 1}, // 20 × 0.5 × 5 turns ÷ 1000 input tokens = 5% of $100
	}
	if len(w.Items) != len(want) {
		t.Fatalf("items = %+v", w.Items)
	}
	for i, c := range want {
		it := w.Items[i]
		if it.Kind != c.kind || it.Amount.USD != c.usd || it.Amount.Shown != c.usd || it.Share != c.share || it.Sessions != c.sessions ||
			it.Title == "" || it.Hint == "" || it.Explain == "" || it.Money != it.Amount {
			t.Errorf("item %d = %+v, want %+v", i, it, c)
		}
	}
	if w.Items[0].Title != "Брошенные сессии" || w.Items[1].Title != "Повторы после ошибок" {
		t.Errorf("titles = %q %q", w.Items[0].Title, w.Items[1].Title)
	}

	// The answer has the keys of the web fixture, at the top and in every item.
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "app", "hub-api", "fixtures", "analytics", "waste-admin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fx, got map[string]any
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	r.JSON(&got)
	if keys(fx) != keys(got) {
		t.Errorf("waste keys: fixture %s, answer %s", keys(fx), keys(got))
	}
	fi, gi := fx["items"].([]any), got["items"].([]any)
	if len(fi) != 4 || len(gi) != 4 {
		t.Fatalf("items: fixture %d, answer %d", len(fi), len(gi))
	}
	for i := range fi {
		if keys(fi[i].(map[string]any)) != keys(gi[i].(map[string]any)) {
			t.Errorf("item %d keys: fixture %s, answer %s", i, keys(fi[i].(map[string]any)), keys(gi[i].(map[string]any)))
		}
	}

	// A period without sessions: an empty list, not null; so is a tenant without any.
	for _, q := range []string{"?from=2026-08-01&to=2026-09-01", "?scope=tenant:" + v.otherTenant} {
		r := v.anna.Do("GET", v.url("/waste"+q), nil)
		if r.Code != http.StatusOK || !strings.Contains(string(r.Body), `"items":[]`) {
			t.Errorf("empty %s: %s", q, r)
		}
	}

	// The lead sees his team's; vera manages nothing; a bad period is 400.
	var lead analytics.WasteResponse
	r = v.boris.Do("GET", v.url("/waste?scope=team:"+v.team.ID), nil)
	if r.Code != http.StatusOK {
		t.Fatalf("lead waste: %s", r)
	}
	r.JSON(&lead)
	if lead.Scope.Kind != "team" || lead.Scope.ID != v.team.ID || len(lead.Items) != 4 || lead.Items[0].Amount.USD != 100 {
		t.Errorf("lead waste = %+v", lead)
	}
	for _, c := range []struct {
		who  string
		url  string
		code int
	}{{"vera", "/waste", 403}, {"vera", "/waste?scope=team:" + v.team.ID, 403}, {"boris", "/waste", 403}, {"anna", "/waste?period=year", 400}} {
		p := v.anna
		switch c.who {
		case "vera":
			p = v.vera
		case "boris":
			p = v.boris
		}
		if r := p.Do("GET", v.url(c.url), nil); r.Code != c.code {
			t.Errorf("%s %s: %s", c.who, c.url, r)
		}
	}
}
