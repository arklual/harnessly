package orgs_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/analytics"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

func (v veraCase) signals(t *testing.T, p *hubtest.Person, query string) []analytics.Signal {
	t.Helper()
	r := p.Do("GET", v.url("/signals"+query), nil)
	if r.Code != http.StatusOK {
		t.Fatalf("signals %s: %s", query, r)
	}
	var out analytics.SignalsResponse
	r.JSON(&out)
	return out.Signals
}

func find(sigs []analytics.Signal, rule, subject string) *analytics.Signal {
	for i, s := range sigs {
		if s.Rule == rule && s.Subject.ID == subject {
			return &sigs[i]
		}
	}
	return nil
}

func (v veraCase) snooze(p *hubtest.Person, id, until string) hubtest.Resp {
	return p.Do("POST", v.url("/signals/"+id+"/snooze"), map[string]any{"until": until})
}

func (v veraCase) teamQuery() string { return "?scope=team:" + v.team.ID }

func TestSignalsByRole(t *testing.T) {
	v := newVera(t)
	v.paced(t) // the team at $1260 of $1500 and the organization at $1260 of $1261
	v.put(t, v.anna, map[string]any{"scope": scope("org", v.org), "amount": 1261})
	ctx := context.Background()

	// The organization's admin sees everything the limits find: vera at 84%, the team at 84%, the organization.
	all := v.signals(t, v.anna, "")
	vs := find(all, "over_80", v.vera.ID)
	if vs == nil || vs.Severity != "warn" || vs.Action == nil || vs.Action.Kind != "raise_limit" || vs.Action.UserID == nil || *vs.Action.UserID != v.vera.ID ||
		vs.Scope.Kind != "team" || vs.Scope.ID != v.team.ID || vs.FirstAt != "2026-10-31T16:00:00Z" || vs.Escalated || vs.SnoozedUntil != nil {
		t.Fatalf("vera's signal = %+v in %+v", vs, all)
	}
	if find(all, "over_80", v.team.ID) == nil || find(all, "over_80", v.org) == nil {
		t.Errorf("team and organization signals missing: %+v", all)
	}
	// The same rows are in the store, one for a rule and a key.
	open, err := v.s.DB.S().OpenSignals(ctx, v.org)
	if err != nil || len(open) != len(all) {
		t.Fatalf("stored %d (%v), shown %d", len(open), err, len(all))
	}

	// A lead sees his team's, a member only her own, even where she cannot manage.
	lead := v.signals(t, v.boris, v.teamQuery())
	if find(lead, "over_80", v.vera.ID) == nil || find(lead, "over_80", v.team.ID) == nil || find(lead, "over_80", v.org) != nil {
		t.Errorf("lead's signals = %+v", lead)
	}
	own := v.signals(t, v.vera, v.teamQuery())
	if len(own) != 1 || own[0].Subject.ID != v.vera.ID {
		t.Errorf("member's signals = %+v", own)
	}
	if mine := v.signals(t, v.vera, ""); len(mine) != 1 || mine[0].Subject.ID != v.vera.ID {
		t.Errorf("member's signals of the organization = %+v", mine)
	}
	// Somebody outside the scope sees nothing of it.
	if r := v.gleb.Do("GET", v.url("/signals"+v.teamQuery()), nil); r.Code != http.StatusForbidden {
		t.Errorf("gleb: %s", r)
	}
	if r := v.anna.Do("GET", v.url("/signals?scope=team:nope"), nil); r.Code/100 != 4 {
		t.Errorf("unknown scope: %s", r)
	}
}

func TestSignalsKeepFirstMomentAndResolve(t *testing.T) {
	v := newVera(t)
	first := find(v.signals(t, v.anna, ""), "over_80", v.vera.ID)
	if first == nil {
		t.Fatal("no signal")
	}

	v.s.Clock.Advance(time.Hour)
	again := find(v.signals(t, v.anna, ""), "over_80", v.vera.ID)
	if again == nil || again.ID != first.ID || again.FirstAt != first.FirstAt || again.LastAt != "2026-10-31T17:00:00Z" {
		t.Errorf("first %+v, again %+v", first, again)
	}

	// Her limit is raised: the signal is resolved, not deleted.
	v.put(t, v.boris, map[string]any{"scope": scope("team", v.team.ID), "userId": v.vera.ID, "amount": 1000})
	if find(v.signals(t, v.anna, ""), "over_80", v.vera.ID) != nil {
		t.Error("signal survived the raise")
	}
	g, err := v.s.DB.S().SignalByID(context.Background(), first.ID)
	if err != nil || g.ResolvedAt == nil {
		t.Fatalf("stored %+v (%v)", g, err)
	}

	// Lowered again: a new signal, with a new first moment.
	v.s.Clock.Advance(time.Hour)
	v.put(t, v.boris, map[string]any{"scope": scope("team", v.team.ID), "userId": v.vera.ID, "amount": 100})
	back := find(v.signals(t, v.anna, ""), "over_80", v.vera.ID)
	if back == nil || back.ID != first.ID || back.FirstAt != "2026-10-31T18:00:00Z" {
		t.Errorf("back = %+v", back)
	}
}

func TestSignalsSnooze(t *testing.T) {
	v := newVera(t)
	v.paced(t)
	sig := find(v.signals(t, v.anna, ""), "over_80", v.vera.ID)
	if sig == nil {
		t.Fatal("no signal")
	}

	for name, until := range map[string]string{
		"past":      "2026-10-30",
		"now":       "2026-10-31T16:00:00Z",
		"too late":  "2026-12-15",
		"not a day": "tomorrow",
		"empty":     "",
	} {
		r := v.snooze(v.boris, sig.ID, until)
		if r.Code != http.StatusBadRequest || r.Detail("field") != "until" {
			t.Errorf("%s: %s", name, r)
		}
	}
	// Somebody who is neither the addressee nor a manager of the scope.
	if r := v.snooze(v.gleb, sig.ID, "2026-11-05"); r.Code/100 != 4 || r.Code == http.StatusBadRequest {
		t.Errorf("gleb: %s", r)
	}
	if r := v.snooze(v.boris, "nope", "2026-11-05"); r.Code != http.StatusNotFound {
		t.Errorf("unknown: %s", r)
	}

	r := v.snooze(v.boris, sig.ID, "2026-11-05T09:00:00Z")
	if r.Code != http.StatusOK {
		t.Fatalf("snooze: %s", r)
	}
	var out struct{ Signal analytics.Signal }
	r.JSON(&out)
	if out.Signal.ID != sig.ID || out.Signal.SnoozedUntil == nil || *out.Signal.SnoozedUntil != "2026-11-05T09:00:00Z" {
		t.Errorf("snoozed = %+v", out.Signal)
	}

	// Gone from the list and from the overview, back with all=1.
	if find(v.signals(t, v.boris, v.teamQuery()), "over_80", v.vera.ID) != nil {
		t.Error("snoozed signal is listed")
	}
	shown := find(v.signals(t, v.boris, v.teamQuery()+"&all=1"), "over_80", v.vera.ID)
	if shown == nil || shown.SnoozedUntil == nil || shown.FirstAt != sig.FirstAt {
		t.Errorf("all=1: %+v", shown)
	}
	var o analytics.OverviewResponse
	v.anna.Do("GET", v.url("/overview"), nil).JSON(&o)
	if find(o.Attention, "over_80", v.vera.ID) != nil {
		t.Errorf("overview shows the snoozed: %+v", o.Attention)
	}

	// Journaled.
	evs, _ := v.audit(t, v.anna, "?scope=team:"+v.team.ID)
	var n int
	for _, e := range evs {
		if e.Action == "signal.snooze" {
			n++
			if e.Actor.UserID != v.boris.ID || e.Target == nil || e.Target.Kind != "signal" || e.Target.ID != sig.ID || e.Target.Label != sig.Title {
				t.Errorf("snooze event = %+v %+v", e, e.Target)
			}
		}
	}
	if n != 1 {
		t.Errorf("%d snooze events in %+v", n, actions(evs))
	}

	// The addressee can snooze her own signal without managing anything.
	sig2 := find(v.signals(t, v.vera, v.teamQuery()), "over_80", v.vera.ID)
	if sig2 != nil {
		t.Errorf("snoozed signal came back too early: %+v", sig2)
	}
	if r := v.snooze(v.vera, sig.ID, "2026-11-06"); r.Code != http.StatusOK {
		t.Errorf("addressee: %s", r)
	}
	// The team's signal is not hers.
	team := find(v.signals(t, v.anna, v.teamQuery()), "over_80", v.team.ID)
	if team == nil {
		t.Fatal("no team signal")
	}
	if r := v.snooze(v.vera, team.ID, "2026-11-06"); r.Code != http.StatusForbidden {
		t.Errorf("member snoozes the team's: %s", r)
	}

	// The snooze runs out with the clock.
	v.s.Clock.Advance(7 * time.Hour * 24)
	if g, err := v.s.DB.S().SignalByID(context.Background(), sig.ID); err != nil || snoozedAt(g, v.s.Clock.Now()) {
		t.Errorf("stored %+v (%v)", g, err)
	}
}

func snoozedAt(g store.Signal, now time.Time) bool {
	return g.SnoozedUntil != nil && g.SnoozedUntil.After(now)
}

func TestSignalsEscalate(t *testing.T) {
	v := newVera(t)
	sig := find(v.signals(t, v.anna, ""), "over_80", v.vera.ID)
	if sig == nil || sig.Escalated {
		t.Fatalf("fresh signal = %+v", sig)
	}
	ctx := context.Background()
	st := v.s.DB.S()
	g, err := st.SignalByID(ctx, sig.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Nobody did anything for two days.
	g.FirstAt = v.s.Clock.Now().Add(-49 * time.Hour)
	if err := st.UpdateSignal(ctx, g); err != nil {
		t.Fatal(err)
	}
	all := v.signals(t, v.anna, "")
	got := find(all, "over_80", v.vera.ID)
	if got == nil || !got.Escalated || all[0].ID != got.ID && all[0].Severity == got.Severity {
		t.Errorf("escalated = %+v first %+v", got, all[0])
	}
	var o analytics.OverviewResponse
	v.anna.Do("GET", v.url("/overview"), nil).JSON(&o)
	if s := find(o.Attention, "over_80", v.vera.ID); s == nil || !s.Escalated {
		t.Errorf("overview = %+v", o.Attention)
	}

	// Acting on it (a snooze) starts the wait again.
	if r := v.snooze(v.boris, sig.ID, "2026-10-31T18:00:00Z"); r.Code != http.StatusOK {
		t.Fatalf("snooze: %s", r)
	}
	v.s.Clock.Advance(3 * time.Hour) // the snooze is over, 48 hours are not
	if s := find(v.signals(t, v.anna, "?all=1"), "over_80", v.vera.ID); s == nil || s.Escalated {
		t.Errorf("after the snooze = %+v", s)
	}
}

func TestLimitRequest(t *testing.T) {
	v := newVera(t)
	ask := func(p *hubtest.Person, body map[string]any) hubtest.Resp {
		return p.Do("POST", "/api/me/limit-requests", body)
	}
	good := map[string]any{"amountUsd": 150, "reason": "Релиз на этой неделе", "scope": scope("team", v.team.ID)}

	for name, patch := range map[string]map[string]any{
		"zero amount": {"amountUsd": 0},
		"negative":    {"amountUsd": -5},
		"no reason":   {"reason": "  "},
		"long reason": {"reason": strings.Repeat("я", 501)},
		"no scope":    {"scope": map[string]string{}},
	} {
		body := map[string]any{}
		for k, x := range good {
			body[k] = x
		}
		for k, x := range patch {
			body[k] = x
		}
		if r := ask(v.vera, body); r.Code != http.StatusBadRequest {
			t.Errorf("%s: %s", name, r)
		}
	}
	// 500 characters is the most.
	long := map[string]any{"amountUsd": 150, "reason": strings.Repeat("я", 500), "scope": scope("team", v.team.ID)}
	if r := ask(v.vera, long); r.Code != http.StatusCreated {
		t.Errorf("500 runes: %s", r)
	}
	// Gleb is not in vera's team.
	if r := ask(v.gleb, good); r.Code != http.StatusForbidden {
		t.Errorf("outsider: %s", r)
	}

	r := ask(v.vera, good)
	if r.Code != http.StatusCreated {
		t.Fatalf("ask: %s", r)
	}
	var out analytics.LimitRequestResponse
	r.JSON(&out)
	if out.Request.ID == "" || out.Request.Status != "sent" || len(out.Request.To) != 1 || out.Request.To[0].UserID != v.boris.ID {
		t.Errorf("response = %+v", out)
	}

	// The lead sees it among his team's signals, with the sum and the reason.
	req := find(v.signals(t, v.boris, v.teamQuery()), "limit_request", v.vera.ID)
	if req == nil || req.ID != out.Request.ID || req.Severity != "warn" || !strings.Contains(req.Detail, "150") || !strings.Contains(req.Detail, "Релиз") ||
		req.Action == nil || req.Action.Kind != "raise_limit" || req.Action.UserID == nil || *req.Action.UserID != v.vera.ID {
		t.Errorf("request signal = %+v", req)
	}
	// Vera sees her own request; the organization's admin sees it too.
	if find(v.signals(t, v.vera, v.teamQuery()), "limit_request", v.vera.ID) == nil || find(v.signals(t, v.anna, ""), "limit_request", v.vera.ID) == nil {
		t.Error("request is not shown to vera or anna")
	}
	// Asking again is the same request.
	first := req.FirstAt
	v.s.Clock.Advance(time.Hour)
	if r := ask(v.vera, good); r.Code != http.StatusCreated {
		t.Fatalf("again: %s", r)
	}
	n := 0
	for _, s := range v.signals(t, v.boris, v.teamQuery()) {
		if s.Rule == "limit_request" {
			n++
			if s.FirstAt != first {
				t.Errorf("first moment moved: %+v", s)
			}
		}
	}
	if n != 1 {
		t.Errorf("%d requests", n)
	}

	// It stays until a limit is set after it, then it is resolved.
	v.s.Clock.Advance(time.Minute)
	if find(v.signals(t, v.boris, v.teamQuery()), "limit_request", v.vera.ID) == nil {
		t.Error("request resolved by itself")
	}
	v.s.Clock.Advance(time.Minute)
	v.put(t, v.boris, map[string]any{"scope": scope("team", v.team.ID), "userId": v.vera.ID, "amount": 250})
	v.s.Clock.Advance(time.Minute)
	if find(v.signals(t, v.boris, v.teamQuery()), "limit_request", v.vera.ID) != nil {
		t.Error("request survived the raise")
	}
}

func TestLimitRequestAddressee(t *testing.T) {
	v := newVera(t)
	// A tenant: its owner; a team lead who asks for himself: the tenant's owner, not himself.
	r := v.vera.Do("POST", "/api/me/limit-requests", map[string]any{"amountUsd": 10, "reason": "Нужно", "scope": scope("tenant", v.tenant)})
	if r.Code != http.StatusCreated {
		t.Fatalf("tenant: %s", r)
	}
	var out analytics.LimitRequestResponse
	r.JSON(&out)
	if len(out.Request.To) != 1 || out.Request.To[0].UserID != v.anna.ID {
		t.Errorf("to = %+v", out.Request.To)
	}
	r = v.boris.Do("POST", "/api/me/limit-requests", map[string]any{"amountUsd": 10, "reason": "Нужно", "scope": scope("team", v.team.ID)})
	if r.Code != http.StatusCreated {
		t.Fatalf("lead: %s", r)
	}
	r.JSON(&out)
	for _, p := range out.Request.To {
		if p.UserID == v.boris.ID {
			t.Errorf("lead asks himself: %+v", out.Request.To)
		}
	}
	// The organization: vera is in it through the tenant.
	r = v.vera.Do("POST", "/api/me/limit-requests", map[string]any{"amountUsd": 10, "reason": "Нужно", "scope": scope("org", v.org)})
	if r.Code != http.StatusCreated {
		t.Fatalf("org: %s", r)
	}
	r.JSON(&out)
	if len(out.Request.To) != 1 || out.Request.To[0].UserID != v.anna.ID {
		t.Errorf("org to = %+v", out.Request.To)
	}
}
