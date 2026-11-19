package analytics_test

import (
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/analytics"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// A heavy screen's answer is served from the cache while fresh, then once more while a new one is computed behind it.
func TestAnalyticsCache(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_ORGS", "on"), hubtest.Env("HUB_ANALYTICS_CACHE", "20s"))
	s.SignIn("root@example.com")
	anna := s.SignIn("anna@example.com")
	f := &fixture{t: t, st: s.DB.S()}
	f.account("key", "api_key", "personal", "", 0)
	f.session(anna.ID, "a", "", "key", "personal", "live", "2026-10-02T09:00:00Z", `{"turns":5,"mrs":1}`)
	const path = "/api/me/kpis?from=2026-10-01&to=2026-11-01"
	sessions := func(r hubtest.Resp) int {
		t.Helper()
		if r.Code != 200 {
			t.Fatalf("kpis: %s", r)
		}
		var k analytics.KpisResponse
		r.JSON(&k)
		for _, v := range k.Kpis {
			if v.Kpi == "completion" {
				return v.N
			}
		}
		t.Fatalf("no completion in %+v", k)
		return 0
	}
	if n := sessions(anna.Do("GET", path, nil)); n != 1 {
		t.Fatalf("first: %d sessions", n)
	}
	f.session(anna.ID, "b", "", "key", "personal", "live", "2026-10-03T09:00:00Z", `{"turns":5,"mrs":1}`)
	r := anna.Do("GET", path, nil)
	if n := sessions(r); n != 1 || r.Header.Get("X-Hub-Cache-Age") == "" {
		t.Errorf("fresh answer not served from the cache: %d sessions, age %q", n, r.Header.Get("X-Hub-Cache-Age"))
	}
	if n := sessions(anna.Do("GET", path, nil, "Cache-Control", "no-cache")); n != 2 {
		t.Errorf("no-cache: %d sessions", n)
	}
	// Past its freshness the old answer is served once more and a new one is computed behind it.
	s.Clock.Advance(30 * time.Second)
	f.session(anna.ID, "c", "", "key", "personal", "live", "2026-10-04T09:00:00Z", `{"turns":5,"mrs":1}`)
	sessions(anna.Do("GET", path, nil))
	deadline := time.Now().Add(5 * time.Second)
	for sessions(anna.Do("GET", path, nil)) != 3 {
		if time.Now().After(deadline) {
			t.Fatal("the answer was never computed again")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Another person never gets anna's answer.
	boris := s.SignIn("boris@example.com")
	if n := sessions(boris.Do("GET", path, nil)); n != 0 {
		t.Errorf("boris sees %d of anna's sessions", n)
	}
}
