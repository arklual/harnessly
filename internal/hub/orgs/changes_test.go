package orgs_test

import (
	"net/http"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/analytics"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

func TestChanges(t *testing.T) {
	v := newVera(t)
	v.paced(t)
	create := func(p *hubtest.Person, body map[string]any) analytics.Change {
		t.Helper()
		r := p.Do("POST", v.url("/changes"), body)
		if r.Code != http.StatusCreated {
			t.Fatalf("create %v: %s", body, r)
		}
		var out struct{ Change analytics.Change }
		r.JSON(&out)
		return out.Change
	}
	org := create(v.anna, map[string]any{"scope": scope("org", v.org), "title": "  Opus по умолчанию ", "at": "2026-10-20"})
	if org.Title != "Opus по умолчанию" || org.Kind != "manual" || org.Source != "manual" || org.At != "2026-10-19T21:00:00Z" ||
		org.Author == nil || org.Author.UserID != v.anna.ID || org.Scope.Kind != "org" {
		t.Errorf("org change = %+v", org)
	}
	team := create(v.boris, map[string]any{"scope": scope("team", v.team.ID), "title": "Новые скиллы", "at": "2026-10-22T10:00:00Z"})

	// The lead marks his team only; vera marks nothing; a title and a day are needed, and not in the future.
	for _, c := range []struct {
		p    *hubtest.Person
		body map[string]any
		code int
	}{
		{v.boris, map[string]any{"scope": scope("org", v.org), "title": "x", "at": "2026-10-20"}, 403},
		{v.vera, map[string]any{"scope": scope("team", v.team.ID), "title": "x", "at": "2026-10-20"}, 403},
		{v.anna, map[string]any{"scope": scope("org", v.org), "title": " ", "at": "2026-10-20"}, 400},
		{v.anna, map[string]any{"scope": scope("org", v.org), "title": "x", "at": "вчера"}, 400},
		{v.anna, map[string]any{"scope": scope("org", v.org), "title": "x", "at": "2027-01-01"}, 400},
	} {
		if r := c.p.Do("POST", v.url("/changes"), c.body); r.Code != c.code {
			t.Errorf("%s %v: %s", c.p.Email, c.body, r)
		}
	}

	list := func(p *hubtest.Person, q string) []string {
		t.Helper()
		r := p.Do("GET", v.url("/changes"+q), nil)
		if r.Code != http.StatusOK {
			t.Fatalf("changes %s: %s", q, r)
		}
		var out analytics.ChangesResponse
		r.JSON(&out)
		var ids []string
		for _, c := range out.Changes {
			ids = append(ids, c.ID)
		}
		return ids
	}
	// A team sees the organization's changes too; the organization not its teams'.
	if ids := list(v.boris, "?scope=team:"+v.team.ID); len(ids) != 2 || ids[0] != org.ID || ids[1] != team.ID {
		t.Errorf("team changes = %v", ids)
	}
	if ids := list(v.anna, ""); len(ids) != 1 || ids[0] != org.ID {
		t.Errorf("org changes = %v", ids)
	}
	if ids := list(v.anna, "?from=2026-10-21&to=2026-11-01&scope=team:"+v.team.ID); len(ids) != 1 || ids[0] != team.ID {
		t.Errorf("changes from the 21st = %v", ids)
	}
	if r := v.vera.Do("GET", v.url("/changes"), nil); r.Code != 403 {
		t.Errorf("vera's changes: %s", r)
	}

	effect := func(p *hubtest.Person, id, q string) analytics.EffectResponse {
		t.Helper()
		r := p.Do("GET", v.url("/changes/"+id+"/effect"+q), nil)
		if r.Code != http.StatusOK {
			t.Fatalf("effect %s%s: %s", id, q, r)
		}
		var out analytics.EffectResponse
		r.JSON(&out)
		return out
	}
	e := effect(v.anna, org.ID, "")
	if e.Change.ID != org.ID || e.Before.From != "2026-10-13" || e.Before.To != "2026-10-20" || e.After.To != "2026-10-27" ||
		len(e.Overlaps) != 0 || len(e.Effects) != 15 || e.Effects[0].Scope.Kind != "org" {
		t.Errorf("effect = %+v", e)
	}
	// On the team the team's own change falls in the windows: the effect is mixed unless too few.
	e = effect(v.boris, org.ID, "?scope=team:"+v.team.ID)
	if len(e.Overlaps) != 1 || e.Overlaps[0] != team.ID {
		t.Errorf("team effect overlaps = %v", e.Overlaps)
	}
	for _, x := range e.Effects {
		if x.Verdict != "mixed" && x.Verdict != "too_few" {
			t.Errorf("effect %s = %s", x.Kpi, x.Verdict)
		}
	}
	// The organization is not touched by a team's change.
	if r := v.anna.Do("GET", v.url("/changes/"+team.ID+"/effect?scope=org:"+v.org), nil); r.Code != 404 {
		t.Errorf("team change on the org: %s", r)
	}
	if r := v.vera.Do("GET", v.url("/changes/"+org.ID+"/effect"), nil); r.Code != 403 {
		t.Errorf("vera's effect: %s", r)
	}

	evs, _ := v.audit(t, v.anna, "")
	found := false
	for _, ev := range evs {
		if ev.Action == "change.create" && ev.Target != nil && ev.Target.ID == org.ID {
			found = ev.Target.Label == "Opus по умолчанию"
		}
	}
	if !found {
		t.Error("no change.create with its title in the journal")
	}
}
