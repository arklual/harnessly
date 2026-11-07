package apps

import (
	"encoding/json"
	"testing"
)

func contributionApp(t *testing.T, id string) Installed {
	t.Helper()
	m := &Manifest{
		ID: id, Targets: []string{TargetChat, TargetPane, "sinopia.acme.deck"},
		Views: []View{{ID: "main", Entry: "ui/index.html", Targets: []string{TargetPane}}, {ID: "card", Entry: "ui/card.html", Targets: []string{TargetChat}}},
		Chat:  &Chat{Commands: []string{"/pulse"}, Cards: []string{"card"}},
		Agent: &Agent{Instructions: "x"},
	}
	m.Sinopia, _ = json.Marshal(Sinopia{
		Panels: []SinopiaPanel{{ID: "inspect", View: "main"}},
		Checks: []SinopiaCheck{{ID: "lint", Kinds: []string{"acme.deck"}}},
	})
	return Installed{ID: id, Manifest: m}
}

// Removing an app takes away every record it owns, in one operation, and nothing of the other app.
func TestContributionsRemovedWithTheirApp(t *testing.T) {
	c := NewContributions([]Installed{contributionApp(t, "acme.a"), contributionApp(t, "acme.b"), {ID: "acme.muted", Muted: true, Manifest: contributionApp(t, "acme.muted").Manifest}})
	count := func(c *Contributions, app string) int {
		n := 0
		for _, r := range c.All() {
			if r.App == app {
				n++
			}
		}
		return n
	}
	if count(c, "acme.a") == 0 || count(c, "acme.muted") != 0 {
		t.Fatalf("records: %+v", c.All())
	}
	left := c.Without("acme.a")
	if count(left, "acme.a") != 0 || count(left, "acme.b") != count(c, "acme.b") {
		t.Fatalf("after removal: %+v", left.All())
	}
	if got := len(c.Of("chat.command")); got != 2 {
		t.Errorf("commands: %d", got)
	}
	in := left.In("sinopia.acme.deck")
	kinds := map[string]bool{}
	for _, r := range in {
		kinds[r.Kind] = true
	}
	if !kinds["sinopia.panel"] || !kinds["sinopia.check"] || !kinds["agent"] {
		t.Errorf("sinopia records: %+v", in)
	}
	if len(left.In(TargetPane)) != 2 { // the view and the agent profile
		t.Errorf("pane: %+v", left.In(TargetPane))
	}
}

// Every kind of the table reads from its manifest field and is listed in the catalog: one place adds a kind.
func TestContributionKindsAreInTheCatalog(t *testing.T) {
	cat := SurfaceCatalog()
	if len(cat.Contributions) != len(contributionTable) {
		t.Fatal("catalog and table differ")
	}
	seen := map[string]bool{}
	for _, k := range contributionTable {
		if seen[k.Name] || k.Field == "" || len(k.Targets) == 0 || k.read == nil {
			t.Errorf("kind %+v", k.ContributionKind)
		}
		seen[k.Name] = true
	}
}
