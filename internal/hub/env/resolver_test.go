package env

import (
	"fmt"
	"slices"
	"testing"
)

func items() map[string]Item {
	return map[string]Item{
		"review":  {ID: "review", Kind: "skill", Name: "Review"},
		"tracker": {ID: "tracker", Kind: "mcp", Name: "Tracker"},
		"docs":    {ID: "docs", Kind: "doc", Name: "Handbook"},
		"old":     {ID: "old", Kind: "repo", Name: "Old", Archived: true},
	}
}

func entry(layer, scope, item string, opts ...func(*Entry)) Entry {
	e := Entry{ID: layer + ":" + scope + ":" + item, Layer: layer, ScopeID: scope, ItemID: item, Enabled: true, Audience: Audience{Mode: AudienceTenant}}
	for _, o := range opts {
		o(&e)
	}
	return e
}

func pinned(e *Entry)   { e.Pinned = true }
func disabled(e *Entry) { e.Enabled = false }
func teams(ids ...string) func(*Entry) {
	return func(e *Entry) { e.Audience = Audience{Mode: AudienceTeams, Teams: ids} }
}
func nobody(e *Entry) { e.Audience = Audience{Mode: AudienceOff} }

func TestResolve(t *testing.T) {
	anna := Person{OrgID: "o1", SpaceID: "s1", Teams: []string{"t1"}}
	cases := []struct {
		name    string
		entries []Entry
		person  Person
		want    []string // "item@layer" with "!" when pinned and "-" when switched off by the person
	}{
		{"nothing", nil, anna, nil},
		{"org entry reaches every tenant", []Entry{entry("org", "o1", "review")}, anna, []string{"review@org"}},
		{"another organization's entry", []Entry{entry("org", "o2", "review")}, anna, nil},
		{"no organization layer without an organization", []Entry{entry("org", "o1", "review")}, Person{SpaceID: "s1"}, nil},
		{"tenant entry", []Entry{entry("tenant", "s1", "tracker")}, anna, []string{"tracker@tenant"}},
		{"another tenant's entry", []Entry{entry("tenant", "s2", "tracker")}, anna, nil},
		{"team entry only for its members", []Entry{entry("team", "t1", "docs"), entry("team", "t2", "review")}, anna, []string{"docs@team"}},
		{"highest layer is the source", []Entry{entry("team", "t1", "review"), entry("tenant", "s1", "review"), entry("org", "o1", "review")}, anna,
			[]string{"review@org"}},
		{"audience teams reaches listed teams", []Entry{entry("tenant", "s1", "tracker", teams("t1", "t9"))}, anna, []string{"tracker@tenant"}},
		{"audience teams skips the others", []Entry{entry("tenant", "s1", "tracker", teams("t9"))}, anna, nil},
		{"audience teams skips a person without team", []Entry{entry("tenant", "s1", "tracker", teams("t1"))}, Person{OrgID: "o1", SpaceID: "s1"}, nil},
		{"audience off reaches nobody", []Entry{entry("tenant", "s1", "tracker", nobody)}, anna, nil},
		{"disabled entry puts nothing in", []Entry{entry("tenant", "s1", "tracker", disabled)}, anna, nil},
		{"archived item is never in", []Entry{entry("org", "o1", "old")}, anna, nil},
		{"unknown item is never in", []Entry{entry("org", "o1", "ghost")}, anna, nil},
		{"tenant switches off an org item", []Entry{entry("org", "o1", "review"), entry("tenant", "s1", "review", disabled)}, anna, nil},
		{"tenant switch-off for other teams only", []Entry{entry("org", "o1", "review"), entry("tenant", "s1", "review", disabled, teams("t9"))}, anna,
			[]string{"review@org"}},
		{"team puts back what the tenant switched off", []Entry{entry("org", "o1", "review"), entry("tenant", "s1", "review", disabled), entry("team", "t1", "review")}, anna,
			[]string{"review@org"}},
		{"pin of the org beats a tenant switch-off", []Entry{entry("org", "o1", "review", pinned), entry("tenant", "s1", "review", disabled)}, anna,
			[]string{"review@org!"}},
		{"pin of the tenant beats a team switch-off", []Entry{entry("tenant", "s1", "tracker", pinned), entry("team", "t1", "tracker", disabled)}, anna,
			[]string{"tracker@tenant!"}},
		{"pin of the tenant does not stop the org", []Entry{entry("org", "o1", "review"), entry("tenant", "s1", "review", pinned, disabled)}, anna, nil},
		{"pin on a disabled entry pins nothing", []Entry{entry("org", "o1", "review", pinned, disabled), entry("tenant", "s1", "review")}, anna,
			[]string{"review@tenant"}},
		{"pin for other teams does not reach", []Entry{entry("tenant", "s1", "tracker", pinned, teams("t9"))}, anna, nil},
		{"pinned by a lower layer is a pin all the same", []Entry{entry("org", "o1", "review"), entry("team", "t1", "review", pinned)}, anna,
			[]string{"review@org!"}},
		{"person switches off", []Entry{entry("tenant", "s1", "tracker")}, Person{OrgID: "o1", SpaceID: "s1", Off: map[string]bool{"tracker": true}},
			[]string{"tracker@tenant-"}},
		{"person cannot switch off a pinned item", []Entry{entry("org", "o1", "review", pinned)}, Person{OrgID: "o1", SpaceID: "s1", Off: map[string]bool{"review": true}},
			[]string{"review@org!"}},
		{"order is kind, name", []Entry{entry("org", "o1", "review"), entry("org", "o1", "tracker"), entry("org", "o1", "docs")}, anna,
			[]string{"docs@org", "tracker@org", "review@org"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []string
			for _, e := range Resolve(items(), c.entries, c.person) {
				s := fmt.Sprintf("%s@%s", e.ID, e.Layer)
				if e.Pinned {
					s += "!"
				}
				if e.Off {
					s += "-"
				}
				got = append(got, s)
			}
			if !slices.Equal(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestActive(t *testing.T) {
	p := Person{OrgID: "o1", SpaceID: "s1", Off: map[string]bool{"tracker": true}}
	all := Resolve(items(), []Entry{entry("org", "o1", "review"), entry("org", "o1", "tracker")}, p)
	if len(all) != 2 || len(Active(all)) != 1 || Active(all)[0].ID != "review" {
		t.Fatalf("%+v", all)
	}
}

func TestLockedAbove(t *testing.T) {
	es := []Entry{entry("org", "o1", "review", pinned), entry("tenant", "s1", "tracker", pinned), entry("tenant", "s1", "docs", pinned, teams("t1"))}
	cases := []struct{ layer, item, want string }{
		{"org", "review", ""},
		{"tenant", "review", "org"},
		{"team", "review", "org"},
		{"team", "tracker", "tenant"},
		{"tenant", "tracker", ""},
		{"team", "docs", ""},
	}
	for _, c := range cases {
		if got := LockedAbove(es, c.layer, "o1", "s1", c.item); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.layer, c.item, got, c.want)
		}
	}
	if got := LockedAbove(es, "tenant", "o2", "s1", "review"); got != "" {
		t.Errorf("another organization's pin: %q", got)
	}
}

func TestParseAudience(t *testing.T) {
	for raw, want := range map[string]Audience{
		``:                               {Mode: "tenant", Teams: []string{}},
		`{"mode":"teams","teams":["a"]}`: {Mode: "teams", Teams: []string{"a"}},
		`{"mode":"off"}`:                 {Mode: "off", Teams: []string{}},
		`{"mode":"everyone"}`:            {Mode: "tenant", Teams: []string{}},
		`not json`:                       {Mode: "tenant", Teams: []string{}},
	} {
		if got := ParseAudience(raw); got.Mode != want.Mode || !slices.Equal(got.Teams, want.Teams) {
			t.Errorf("%q: %+v, want %+v", raw, got, want)
		}
	}
}
